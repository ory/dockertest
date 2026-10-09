// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
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

	shutdownCtx context.Context
	cancel      context.CancelFunc
	pending     sync.WaitGroup // in-flight create/build operations

	pools      []*pool
	containers map[string]*containerRecord
	networks   map[string]*networkRecord
	images     map[string]*imageRecord
	daemons    map[string]client.DockerClient // daemon ID -> a client for reconciliation
	onDaemon   func(ctx context.Context, daemonID string, c client.DockerClient) error
	created    int // total resources ever tracked; Main refuses to install after the fact
}

//nolint:govet // field alignment traded for readability
type containerRecord struct {
	pool     *pool
	imageID  string
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

func (r *removal) wait() error {
	<-r.done
	return r.err
}

// imageRecord is a build image owned by this process. users counts pending
// builds and live containers created from the image. The image is deleted
// when users drops to zero unless it is retained.
type imageRecord struct {
	client client.DockerClient // client of the pool that built the image
	users  int
	retain bool
}

func newProcessOwner(runID, hostID string, stderr io.Writer) *processOwner {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gocritic // shutdown signal is process-scoped, not request-scoped
	return &processOwner{
		runID:       runID,
		hostID:      hostID,
		stderr:      stderr,
		shutdownCtx: ctx,
		cancel:      cancel,
		containers:  map[string]*containerRecord{},
		networks:    map[string]*networkRecord{},
		images:      map[string]*imageRecord{},
		daemons:     map[string]client.DockerClient{},
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
	if o.shutdownCtx.Err() != nil {
		return nil, nil, ErrShuttingDown
	}
	o.pending.Add(1)
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(o.shutdownCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
		o.pending.Done()
	}, nil
}

// registerDaemon records the daemon a pool talks to. The first time a daemon is
// seen, Main's hook runs recovery for abandoned runs on that daemon.
func (o *processOwner) registerDaemon(ctx context.Context, daemonID string, c client.DockerClient) error {
	o.mu.Lock()
	if _, ok := o.daemons[daemonID]; ok {
		o.mu.Unlock()
		return nil
	}
	o.daemons[daemonID] = c
	hook := o.onDaemon
	o.mu.Unlock()
	if hook == nil {
		return nil
	}
	return hook(ctx, daemonID, c)
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
	if !ok || rec.imageID != "" {
		return
	}
	rec.imageID = imageID
	if img, ok := o.images[imageID]; ok {
		img.users++
	}
}

// removeContainer force-removes a container and its anonymous volumes through
// c, the client of whoever performs the removal (the creating pool may have
// closed its own client by now). On success (or if the container is already
// gone) the record is dropped and the container's image reference is
// released. On failure the record is kept so that a later cleanup can retry.
func (o *processOwner) removeContainer(ctx context.Context, c client.DockerClient, id string) error {
	o.mu.Lock()
	rec, ok := o.containers[id]
	if !ok {
		o.mu.Unlock()
		return nil
	}
	attempt, wait := start(&rec.inflight)
	o.mu.Unlock()
	if wait {
		return attempt.wait()
	}

	_, err := c.ContainerRemove(ctx, id, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return o.finish(&rec.inflight, attempt, fmt.Errorf("removing container %s: %w", id, err))
	}

	o.mu.Lock()
	delete(o.containers, id)
	o.mu.Unlock()
	if rec.imageID != "" {
		err = o.releaseImage(ctx, c, rec.imageID)
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
		return attempt.wait()
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

// addImageUser registers an owned build image, or adds a user to it.
func (o *processOwner) addImageUser(id string, c client.DockerClient, retain bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	img, ok := o.images[id]
	if !ok {
		o.created++
		img = &imageRecord{client: c, retain: retain}
		o.images[id] = img
	}
	img.users++
}

// releaseImage drops one user of an owned image and deletes the image once
// nobody uses it. Deletion is by full ID and never forced: an image that is
// still tagged elsewhere or referenced by a foreign container stays, the
// conflict is returned, and the record is kept for a later retry.
func (o *processOwner) releaseImage(ctx context.Context, c client.DockerClient, id string) error {
	o.mu.Lock()
	img, ok := o.images[id]
	if !ok {
		o.mu.Unlock()
		return nil
	}
	if img.users > 0 {
		img.users--
	}
	if img.users > 0 || img.retain {
		o.mu.Unlock()
		return nil
	}
	o.mu.Unlock()
	return o.deleteImage(ctx, c, id)
}

func (o *processOwner) deleteImage(ctx context.Context, c client.DockerClient, id string) error {
	_, err := c.ImageRemove(ctx, id, mobyclient.ImageRemoveOptions{Force: false, PruneChildren: false})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("removing image %s: %w", id, err)
	}
	o.mu.Lock()
	delete(o.images, id)
	o.mu.Unlock()
	return nil
}

// removeUnusedImages retries the deletion of owned images that nobody uses
// anymore but whose earlier deletion failed, for example because of a tag
// conflict that has since been resolved.
func (o *processOwner) removeUnusedImages(ctx context.Context) error {
	o.mu.Lock()
	unused := map[string]client.DockerClient{}
	for id, img := range o.images {
		if img.users == 0 && !img.retain {
			unused[id] = img.client
		}
	}
	o.mu.Unlock()
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(unused)) {
		errs = append(errs, o.deleteImage(ctx, unused[id], id))
	}
	return errors.Join(errs...)
}

// retry removes tracked containers and networks whose earlier removal failed,
// each through the client of the pool that created it. With only set, the
// retry is limited to that pool's resources and reused containers that other
// handles still reference are skipped; with only nil, every record whose pool
// still has a client is retried. Unused images are retried afterwards.
func (o *processOwner) retry(ctx context.Context, only *pool) error {
	o.mu.Lock()
	containers := map[string]client.DockerClient{}
	networks := map[string]client.DockerClient{}
	for id, rec := range o.containers {
		if (only != nil && rec.pool != only) || rec.pool.client == nil {
			continue
		}
		if _, live := get(rec.reuseID); only != nil && rec.reuseID != "" && live {
			continue
		}
		containers[id] = rec.pool.client
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
	o.cancel()
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
	for id, img := range o.images {
		if !img.retain {
			ids = append(ids, "image "+id)
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

	images, err := c.ImageList(ctx, mobyclient.ImageListOptions{Filters: filters})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("listing images: %w", err))...)
	}
	for i := range images.Items {
		img := &images.Items[i]
		if !ownershipMatches(img.Labels, want) || img.Labels[labelRetain] == labelTrue {
			continue
		}
		if _, removeErr := c.ImageRemove(ctx, img.ID, mobyclient.ImageRemoveOptions{Force: false, PruneChildren: false}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("removing image %s: %w", img.ID, removeErr))
		}
	}
	return errors.Join(errs...)
}
