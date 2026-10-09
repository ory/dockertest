// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

// processOwner is the single process-wide inventory of Docker resources that
// dockertest created. It is separate from the reuse registry: the registry
// tracks handles to reusable containers, the owner tracks every physical
// container, network, and build image, including WithoutReuse containers and
// resources whose removal failed and is pending retry.
//
// The owner also coordinates shutdown: once shutdown starts, no new resources
// are admitted and the contexts of in-flight operations are canceled.
//
//nolint:govet // field alignment traded for readability
type processOwner struct {
	mu sync.Mutex

	runID  string
	hostID string
	scope  string // set by Main; empty otherwise
	stderr io.Writer

	shutdown chan struct{}  // closed when process shutdown starts
	pending  sync.WaitGroup // in-flight create/build operations

	pools       []*pool
	containers  map[string]*containerRecord
	networks    map[string]*networkRecord
	images      map[imageKey]*imageRecord
	acquiring   map[string]int                 // daemon ID -> pending image acquisitions
	daemons     map[string]client.DockerClient // daemon ID -> a client for reconciliation
	registering map[string]chan struct{}       // daemon ID -> closed when its pending hook returns
	onDaemon    func(ctx context.Context, daemonID string, c client.DockerClient) error
	created     int // total resources ever tracked; Main refuses to install after the fact
}

//nolint:govet // field alignment traded for readability
type containerRecord struct {
	pool     *pool
	image    imageKey
	reuseID  string // registry key; empty for WithoutReuse containers
	inflight *removal
}

type networkRecord struct {
	pool     *pool
	inflight *removal
}

// removal is one removal attempt. Concurrent removers of the same resource
// (a handle's Close, the pool's Close, and Main) share the attempt instead of
// issuing a second request; err is written before done is closed.
type removal struct {
	done chan struct{}
	err  error
}

// start returns the in-flight attempt to wait on, or a new one to perform. The
// caller must hold o.mu and must have looked the record up under the same
// lock: a record that was already removed and dropped must not start a second
// attempt, which would release its image reference twice.
func start(inflight **removal) (attempt *removal, wait bool) {
	if *inflight != nil {
		return *inflight, true
	}
	*inflight = &removal{done: make(chan struct{})}
	return *inflight, false
}

func (o *processOwner) finish(inflight **removal, attempt *removal, err error) error {
	o.mu.Lock()
	attempt.err = err
	*inflight = nil
	o.mu.Unlock()
	close(attempt.done)
	return err
}

// wait waits for the attempt or for ctx to expire, so that a caller's deadline
// is honored even when the attempt runs under a longer one.
func (r *removal) wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// imageKey identifies an image on a daemon. Image IDs are content-addressed,
// so identical builds on distinct daemons share an ID but are distinct images.
type imageKey struct {
	daemon string
	id     string
}

func (k imageKey) compare(other imageKey) int {
	return cmp.Or(strings.Compare(k.daemon, other.daemon), strings.Compare(k.id, other.id))
}

// imageRecord is a build image owned by this process. users counts pending
// builds and live containers created from the image. The image is deleted
// when users drops to zero unless it is retained.
//
// A build whose image could not be inspected leaves a record with the
// ownership labels it expected in verify: the daemon reported the ID, but
// whether the image is the one this process built is unknown. Such an image
// is only deleted after an inspection shows those labels, and is forgotten if
// it is gone or turns out to be foreign.
//
//nolint:govet // field alignment traded for readability
type imageRecord struct {
	client   client.DockerClient // client of the last pool that built or tried to delete the image
	users    int
	retain   bool
	deferred bool              // deletion was skipped because an image acquisition on the daemon was pending
	verify   map[string]string // ownership labels to verify before deletion; nil once verified
	inflight *removal
}

func newProcessOwner(runID, hostID string, stderr io.Writer) *processOwner {
	return &processOwner{
		runID:       runID,
		hostID:      hostID,
		stderr:      stderr,
		shutdown:    make(chan struct{}),
		containers:  map[string]*containerRecord{},
		networks:    map[string]*networkRecord{},
		images:      map[imageKey]*imageRecord{},
		acquiring:   map[string]int{},
		daemons:     map[string]client.DockerClient{},
		registering: map[string]chan struct{}{},
	}
}

// owner is the process-wide owner. Tests swap it for an isolated instance.
var owner = newProcessOwner(newRunID(), localHostID(), os.Stderr)

func newRunID() string {
	return rand.Text()
}

func localHostID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown-host"
	}
	return host
}

func (o *processOwner) warn(format string, args ...any) {
	_, _ = fmt.Fprintf(o.stderr, "dockertest: "+format+"\n", args...) //nolint:errcheck // warnings are best effort
}

func (o *processOwner) scopeName() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.scope
}

// install attaches Main to the owner. It fails when Main was already installed
// or when Docker resources were created before Main could take ownership.
func (o *processOwner) install(scope string, onDaemon func(context.Context, string, client.DockerClient) error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.scope != "" {
		return fmt.Errorf("%w: Main is already installed with scope %q", ErrInvalidOption, o.scope)
	}
	if o.created > 0 {
		return fmt.Errorf("%w: Docker resources were created before Main was installed", ErrInvalidOption)
	}
	o.scope = scope
	o.onDaemon = onDaemon
	return nil
}

func (o *processOwner) addPool(p *pool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pools = append(o.pools, p)
}

// beginOperation admits a resource-creating operation. The returned context is
// canceled when either the caller's context or process shutdown is canceled.
// The returned done function must be called when the operation finishes.
func (o *processOwner) beginOperation(ctx context.Context) (context.Context, func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	select {
	case <-o.shutdown:
		return nil, nil, ErrShuttingDown
	default:
	}
	o.pending.Add(1)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-o.shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		cancel()
		o.pending.Done()
	}, nil
}

// registerDaemon records, under Main, the daemon a pool talks to. The first
// time a daemon is seen, Main's hook runs recovery for abandoned runs on that
// daemon and adds it to the run's manifest. The daemon is published only after
// the hook succeeded: concurrent callers wait for the pending hook and run it
// again if it failed, so no resource is created on a daemon the manifest lacks.
func (o *processOwner) registerDaemon(ctx context.Context, daemonID string, c client.DockerClient) error {
	for {
		o.mu.Lock()
		hook := o.onDaemon
		if _, ok := o.daemons[daemonID]; ok || o.scope == "" {
			o.mu.Unlock()
			return nil
		}
		if pending, ok := o.registering[daemonID]; ok {
			o.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		done := make(chan struct{})
		o.registering[daemonID] = done
		o.mu.Unlock()

		var err error
		if hook != nil {
			err = hook(ctx, daemonID, c)
		}
		o.mu.Lock()
		delete(o.registering, daemonID)
		if err == nil {
			o.daemons[daemonID] = c
		}
		o.mu.Unlock()
		close(done)
		return err
	}
}

func (o *processOwner) trackContainer(id string, p *pool, reuseID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.created++
	o.containers[id] = &containerRecord{pool: p, reuseID: reuseID}
}

// attachImage records the image a tracked container was created from and
// counts the container as a user of that image if the image is owned.
func (o *processOwner) attachImage(containerID, imageID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.containers[containerID]
	if !ok || rec.image.id != "" {
		return
	}
	rec.image = imageKey{daemon: rec.pool.daemonID, id: imageID}
	if img, ok := o.images[rec.image]; ok {
		img.users++
	}
}

// removeContainer force-removes a container and its anonymous volumes through
// the client of p, the pool that performs the removal (the creating pool may
// have closed its own client by now). p becomes responsible for retrying: a
// reused container is removed by whichever pool released the last reference.
// On success (or if the container is already gone) the record is dropped and
// the container's image reference is released. On failure the record is kept
// so that a later cleanup of p can retry.
func (o *processOwner) removeContainer(ctx context.Context, p *pool, id string) error {
	o.mu.Lock()
	rec, ok := o.containers[id]
	if !ok {
		o.mu.Unlock()
		return nil
	}
	c := p.client
	if c == nil {
		o.mu.Unlock()
		return ErrClientClosed
	}
	attempt, wait := start(&rec.inflight)
	if !wait {
		rec.pool = p
	}
	o.mu.Unlock()
	if wait {
		return attempt.wait(ctx)
	}

	_, err := c.ContainerRemove(ctx, id, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return o.finish(&rec.inflight, attempt, fmt.Errorf("removing container %s: %w", id, err))
	}

	// The container stops counting as a user of its image in the same step in
	// which it stops being tracked, so that a concurrent first registration of
	// the image counts it exactly once.
	o.mu.Lock()
	delete(o.containers, id)
	if img, ok := o.images[rec.image]; ok && img.users > 0 {
		img.users--
	}
	o.mu.Unlock()
	err = nil
	if rec.image.id != "" {
		err = o.deleteImage(ctx, c, rec.image)
	}
	return o.finish(&rec.inflight, attempt, err)
}

func (o *processOwner) trackNetwork(id string, p *pool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.created++
	o.networks[id] = &networkRecord{pool: p}
}

func (o *processOwner) removeNetwork(ctx context.Context, c client.DockerClient, id string) error {
	o.mu.Lock()
	rec, ok := o.networks[id]
	if !ok {
		o.mu.Unlock()
		return nil
	}
	attempt, wait := start(&rec.inflight)
	o.mu.Unlock()
	if wait {
		return attempt.wait(ctx)
	}
	_, err := c.NetworkRemove(ctx, id, mobyclient.NetworkRemoveOptions{})
	if err != nil && !errdefs.IsNotFound(err) {
		return o.finish(&rec.inflight, attempt, fmt.Errorf("removing network %s: %w", id, err))
	}
	o.mu.Lock()
	delete(o.networks, id)
	o.mu.Unlock()
	return o.finish(&rec.inflight, attempt, nil)
}

// beginImageAcquisition admits an operation on p's daemon that resolves an
// image and makes it used: a build, which can produce, by content address, the
// very image that is being deleted before the build registers as its user, or
// the creation of a container from a tag that may name an owned build image
// until the container is attached to it. Such operations and image deletions
// on the same daemon exclude each other: an acquisition waits for in-flight
// deletions, and deletions are deferred while acquisitions are pending.
//
// The returned function ends the acquisition. When it was the last pending
// one on the daemon, it retries the deferred deletions through p's client,
// ignoring cancellation of ctx but bounded by p's cleanup timeout. They are
// unrelated to the operation, so failures are only reported as warnings; the
// images stay tracked for a later cleanup.
func (o *processOwner) beginImageAcquisition(ctx context.Context, p *pool) (end func(), err error) {
	daemonID := p.daemonID
	o.mu.Lock()
	for {
		var attempt *removal
		for key, img := range o.images {
			if key.daemon == daemonID && img.inflight != nil {
				attempt = img.inflight
				break
			}
		}
		if attempt == nil {
			break
		}
		o.mu.Unlock()
		select {
		case <-attempt.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		o.mu.Lock()
	}
	o.acquiring[daemonID]++
	o.mu.Unlock()
	return func() {
		o.mu.Lock()
		o.acquiring[daemonID]--
		if o.acquiring[daemonID] > 0 {
			o.mu.Unlock()
			return
		}
		delete(o.acquiring, daemonID)
		var deferred []imageKey
		for key, img := range o.images {
			if key.daemon == daemonID && img.deferred {
				deferred = append(deferred, key)
			}
		}
		o.mu.Unlock()
		slices.SortFunc(deferred, imageKey.compare)
		retryCtx, cancel := p.rollbackContext(ctx)
		defer cancel()
		for _, key := range deferred {
			if err := o.deleteImage(retryCtx, p.client, key); err != nil {
				o.warn("%v", err)
			}
		}
	}, nil
}

// attachedUsers counts the tracked containers that were attached to the image
// before it was registered: a container created from a tag can be attached
// while the build that produced the image is still verifying it. The caller
// must hold o.mu.
func (o *processOwner) attachedUsers(key imageKey) int {
	var n int
	for _, rec := range o.containers {
		if rec.image == key {
			n++
		}
	}
	return n
}

// addImageUser registers an owned build image, or adds a user to it. The
// caller must be inside beginImageAcquisition for the image's daemon and must
// have verified the image's ownership labels.
func (o *processOwner) addImageUser(key imageKey, c client.DockerClient, retain bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	img, ok := o.images[key]
	if !ok {
		o.created++
		img = &imageRecord{client: c, retain: retain, users: o.attachedUsers(key)}
		o.images[key] = img
	}
	img.verify = nil
	img.users++
}

// addUnverifiedImage records a non-retained image that a build reported but
// whose ownership labels, want, could not be verified. Its deletion is
// deferred until the caller, which must be inside beginImageAcquisition for
// the image's daemon, ends the acquisition; later cleanups retry it.
func (o *processOwner) addUnverifiedImage(key imageKey, c client.DockerClient, want map[string]string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.images[key]; ok {
		return // already owned and verified by another build
	}
	o.created++
	o.images[key] = &imageRecord{client: c, verify: want, deferred: true, users: o.attachedUsers(key)}
}

// releaseImage drops one user of an owned image and deletes the image once
// nobody uses it.
func (o *processOwner) releaseImage(ctx context.Context, c client.DockerClient, key imageKey) error {
	o.mu.Lock()
	if img, ok := o.images[key]; ok && img.users > 0 {
		img.users--
	}
	o.mu.Unlock()
	return o.deleteImage(ctx, c, key)
}

// deleteImage deletes an owned image through c unless it is in use or
// retained. While an image acquisition on its daemon is pending, the deletion
// is deferred until the last pending acquisition ends. An unverified image is
// inspected first and only deleted if it carries the expected ownership
// labels; if it is gone or foreign, it is forgotten without being deleted, and
// if the inspection fails, it is kept. Deletion is by full ID and never forced:
// an image that is still tagged elsewhere or referenced by a foreign container
// stays, the conflict is returned, and the record is kept for a later retry.
// Concurrent deleters share one attempt.
func (o *processOwner) deleteImage(ctx context.Context, c client.DockerClient, key imageKey) error {
	o.mu.Lock()
	img, ok := o.images[key]
	if !ok || img.users > 0 || img.retain {
		o.mu.Unlock()
		return nil
	}
	if o.acquiring[key.daemon] > 0 {
		img.deferred = true
		o.mu.Unlock()
		return nil
	}
	// c is open now, while the pool that built the image may have closed its
	// client: later retries use c.
	img.client = c
	img.deferred = false
	attempt, wait := start(&img.inflight)
	verify := img.verify
	o.mu.Unlock()
	if wait {
		return attempt.wait(ctx)
	}
	if verify != nil {
		_, owned, err := inspectOwnership(ctx, c, key.id, verify)
		if err != nil && !errdefs.IsNotFound(err) {
			return o.finish(&img.inflight, attempt, fmt.Errorf("verifying image %s: %w", key.id, err))
		}
		o.mu.Lock()
		if owned {
			img.verify = nil
		} else {
			delete(o.images, key)
		}
		o.mu.Unlock()
		if !owned {
			return o.finish(&img.inflight, attempt, nil)
		}
	}
	err := removeImage(ctx, c, key.id)
	if err == nil {
		o.mu.Lock()
		if img.users == 0 {
			delete(o.images, key)
		}
		o.mu.Unlock()
	}
	return o.finish(&img.inflight, attempt, err)
}

// inspectOwnership inspects an image and reports its ID and whether it
// carries the ownership labels want.
func inspectOwnership(ctx context.Context, c client.DockerClient, ref string, want map[string]string) (id string, owned bool, err error) {
	inspect, err := c.ImageInspect(ctx, ref)
	if err != nil {
		return "", false, err
	}
	var have map[string]string
	if inspect.Config != nil {
		have = inspect.Config.Labels
	}
	return inspect.ID, ownershipMatches(have, want), nil
}

// removeImage removes an owned image by its immutable ID and never forces.
// Tags are never removed individually: another process can move a tag to an
// unrelated image at any time, and untagging that image could delete it. The
// daemon therefore refuses the removal while the image is tagged in more than
// one repository or used by a foreign container; the conflict is returned so
// that the owner keeps the image for a later retry.
func removeImage(ctx context.Context, c client.DockerClient, id string) error {
	_, err := c.ImageRemove(ctx, id, mobyclient.ImageRemoveOptions{Force: false, PruneChildren: false})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("removing image %s: %w", id, err)
	}
	return nil
}

// removeUnusedImages retries the deletion of owned images that nobody uses
// anymore but whose earlier deletion failed, for example because of a tag
// conflict that has since been resolved, or was deferred.
func (o *processOwner) removeUnusedImages(ctx context.Context) error {
	o.mu.Lock()
	unused := map[imageKey]client.DockerClient{}
	for key, img := range o.images {
		if img.users == 0 && !img.retain {
			unused[key] = img.client
		}
	}
	o.mu.Unlock()
	// deleteImage re-checks each image: it may have gained a user since.
	var errs []error
	for _, key := range slices.SortedFunc(maps.Keys(unused), imageKey.compare) {
		errs = append(errs, o.deleteImage(ctx, unused[key], key))
	}
	return errors.Join(errs...)
}

// retry removes tracked containers and networks whose earlier removal failed,
// each through the client of the pool responsible for it. With only set, the
// retry is limited to that pool's resources and reused containers that other
// handles still reference are skipped; with only nil, every record whose pool
// still has a client is retried. Unused images are retried afterwards.
func (o *processOwner) retry(ctx context.Context, only *pool) error {
	o.mu.Lock()
	containers := map[string]*pool{}
	networks := map[string]client.DockerClient{}
	for id, rec := range o.containers {
		if (only != nil && rec.pool != only) || rec.pool.client == nil {
			continue
		}
		if only != nil && rec.reuseID != "" {
			if live, ok := get(rec.reuseID); ok && live.container.ID == id {
				continue
			}
		}
		containers[id] = rec.pool
	}
	for id, rec := range o.networks {
		if (only == nil || rec.pool == only) && rec.pool.client != nil {
			networks[id] = rec.pool.client
		}
	}
	o.mu.Unlock()

	var errs []error
	for _, id := range slices.Sorted(maps.Keys(containers)) {
		errs = append(errs, o.removeContainer(ctx, containers[id], id))
	}
	for _, id := range slices.Sorted(maps.Keys(networks)) {
		errs = append(errs, o.removeNetwork(ctx, networks[id], id))
	}
	return errors.Join(append(errs, o.removeUnusedImages(ctx))...)
}

// beginShutdown stops admitting new resources and cancels in-flight
// operations. It returns a function that waits for them to drain or for ctx
// to expire.
func (o *processOwner) beginShutdown() (drain func(context.Context) error) {
	o.mu.Lock()
	select {
	case <-o.shutdown:
	default:
		close(o.shutdown)
	}
	o.mu.Unlock()
	return func(ctx context.Context) error {
		done := make(chan struct{})
		go func() {
			o.pending.Wait()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("waiting for in-flight operations: %w", ctx.Err())
		}
	}
}

// cleanup removes everything the process still owns: pool handles first (so
// reuse references are released in order), then any remaining containers,
// networks, and unused images, and finally anything on the known daemons that
// carries this run's labels but whose creation response was lost.
func (o *processOwner) cleanup(ctx context.Context) error {
	o.mu.Lock()
	pools := slices.Clone(o.pools)
	o.mu.Unlock()

	var errs []error
	for _, p := range pools {
		errs = append(errs, p.cleanup(ctx))
	}

	// Pools keep their clients open under Main, so each record's own pool
	// can remove it; a pool without a client leaves its records as leftovers.
	errs = append(errs, o.retry(ctx, nil))

	o.mu.Lock()
	daemons := slices.Collect(maps.Values(o.daemons))
	o.mu.Unlock()
	want := runLabels(o.scopeName(), o.hostID, o.runID)
	for _, c := range daemons {
		errs = append(errs, sweepRun(ctx, c, want))
	}
	return errors.Join(errs...)
}

// leftovers lists the IDs of resources whose removal is still pending.
func (o *processOwner) leftovers() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var ids []string
	for id := range o.containers {
		ids = append(ids, "container "+id)
	}
	for id := range o.networks {
		ids = append(ids, "network "+id)
	}
	for key, img := range o.images {
		if !img.retain {
			ids = append(ids, "image "+key.id)
		}
	}
	slices.Sort(ids)
	return ids
}

// closeClients closes the Docker clients that pools created themselves.
// Caller-supplied clients remain caller-owned.
func (o *processOwner) closeClients() error {
	o.mu.Lock()
	pools := slices.Clone(o.pools)
	o.mu.Unlock()
	errs := make([]error, 0, len(pools))
	for _, p := range pools {
		errs = append(errs, p.closeOwnedClient())
	}
	return errors.Join(errs...)
}

// sweepRun removes every container, network, and non-retained image on the
// daemon behind c whose labels fully match want. It is used both to reconcile
// this run's own leftovers and to recover resources of abandoned runs.
func sweepRun(ctx context.Context, c client.DockerClient, want map[string]string) error {
	filters := mobyclient.Filters{}
	for k, v := range want {
		filters.Add("label", k+"="+v)
	}

	var errs []error
	containers, err := c.ContainerList(ctx, mobyclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	for i := range containers.Items {
		ct := &containers.Items[i]
		if !ownershipMatches(ct.Labels, want) {
			continue
		}
		if _, removeErr := c.ContainerRemove(ctx, ct.ID, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("removing container %s: %w", ct.ID, removeErr))
		}
	}

	networks, err := c.NetworkList(ctx, mobyclient.NetworkListOptions{Filters: filters})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("listing networks: %w", err))...)
	}
	for i := range networks.Items {
		n := &networks.Items[i]
		if !ownershipMatches(n.Labels, want) {
			continue
		}
		if _, removeErr := c.NetworkRemove(ctx, n.ID, mobyclient.NetworkRemoveOptions{}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("removing network %s: %w", n.ID, removeErr))
		}
	}

	// All includes intermediate images: an owned image that became the
	// untagged parent of another owned image is otherwise not listed and would
	// survive the removal of its child. If the parent is listed first, its
	// removal conflicts and the error keeps the run for a later sweep.
	images, err := c.ImageList(ctx, mobyclient.ImageListOptions{All: true, Filters: filters})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("listing images: %w", err))...)
	}
	for i := range images.Items {
		img := &images.Items[i]
		if !ownershipMatches(img.Labels, want) || img.Labels[labelRetain] == labelTrue {
			continue
		}
		errs = append(errs, removeImage(ctx, c, img.ID))
	}
	return errors.Join(errs...)
}
