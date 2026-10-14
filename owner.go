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
	"time"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

// processOwner is the single process-wide inventory of Docker resources that
// dockertest created. It is separate from the reuse registry: the registry
// tracks handles to reusable containers, the owner tracks every physical
// container and network, including WithoutReuse containers and
// resources whose removal failed and is pending retry.
//
// The owner also coordinates shutdown: once shutdown starts, no new resources
// are admitted and the contexts of in-flight operations are canceled.
//
//nolint:govet // field alignment traded for readability
type processOwner struct {
	mu sync.Mutex

	runID   string
	main    bool   // set when Main or Setup is installed
	expires string // expiry label value; set by Main unless the test timeout is disabled
	stderr  io.Writer

	shutdown chan struct{}  // closed when process shutdown starts
	pending  sync.WaitGroup // in-flight create/build operations

	pools      []*pool
	containers map[string]*containerRecord
	networks   map[string]*networkRecord
	created    int // total resources ever tracked; Main and Setup refuse to install after the fact
}

//nolint:govet // field alignment traded for readability
type containerRecord struct {
	pool     *pool
	reuseID  string // registry key; empty for WithoutReuse containers
	inflight *removal
}

type networkRecord struct {
	pool     *pool
	inflight *removal
}

// removal is one removal attempt. Concurrent removers of the same resource (a
// handle's Close, the pool's Close, and process shutdown) share the attempt
// instead of issuing a second request; err is written before done is closed.
type removal struct {
	done chan struct{}
	err  error
}

// start returns the in-flight attempt to wait on, or a new one to perform. The
// caller must hold o.mu and must have looked the record up under the same
// lock: a record that was already removed and dropped must not start a second
// attempt, which would issue a duplicate removal.
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

func newProcessOwner(runID string, stderr io.Writer) *processOwner {
	return &processOwner{
		runID:      runID,
		stderr:     stderr,
		shutdown:   make(chan struct{}),
		containers: map[string]*containerRecord{},
		networks:   map[string]*networkRecord{},
	}
}

// owner is the process-wide owner. Tests swap it for an isolated instance.
var owner = newProcessOwner(rand.Text(), os.Stderr)

func (o *processOwner) installed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.main
}

// install attaches Main or Setup to the owner. It fails when either was
// already installed or when Docker resources were created before ownership
// was taken, as those would lack the expiry label and escape shutdown.
func (o *processOwner) install(expires string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.main {
		return fmt.Errorf("%w: Main or Setup is already installed", ErrInvalidOption)
	}
	if o.created > 0 {
		return fmt.Errorf("%w: Docker resources were created before Main or Setup was installed", ErrInvalidOption)
	}
	o.main = true
	o.expires = expires
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

func (o *processOwner) trackContainer(id string, p *pool, reuseID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.created++
	o.containers[id] = &containerRecord{pool: p, reuseID: reuseID}
}

// removeContainer force-removes a container and its anonymous volumes through
// the client of p, the pool that performs the removal (the creating pool may
// have closed its own client by now). p becomes responsible for retrying: a
// reused container is removed by whichever pool released the last reference.
// On success (or if the container is already gone) the record is dropped.
// On failure the record is kept so that a later cleanup of p can retry.
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

	o.mu.Lock()
	delete(o.containers, id)
	o.mu.Unlock()
	return o.finish(&rec.inflight, attempt, nil)
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

// retry removes tracked containers and networks whose earlier removal failed,
// each through the client of the pool responsible for it. With only set, the
// retry is limited to that pool's resources and reused containers that other
// handles still reference are skipped; with only nil, every record whose pool
// still has a client is retried.
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
	return errors.Join(errs...)
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
// reuse references are released in order), then any remaining containers and
// networks, and finally, on every daemon a pool talks to, anything that carries
// this run's label but whose creation response was lost, or that an expired
// run left behind.
func (o *processOwner) cleanup(ctx context.Context, now time.Time) error {
	o.mu.Lock()
	pools := slices.Clone(o.pools)
	o.mu.Unlock()

	var errs []error
	for _, p := range pools {
		errs = append(errs, p.cleanup(ctx))
	}

	// Pools keep their clients open under Main and Setup, so each record's own pool
	// can remove it; a pool without a client leaves its records as leftovers.
	errs = append(errs, o.retry(ctx, nil))

	// Clients are deduplicated by identity, not by DaemonHost: clients with a
	// custom dialer report the default host while talking to another daemon.
	// Sweeping one daemon through two clients is harmless.
	swept := map[client.DockerClient]bool{}
	for _, p := range pools {
		if p.client != nil && !swept[p.client] {
			swept[p.client] = true
			errs = append(errs, o.sweep(ctx, p.client, now))
		}
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

// sweep removes every container and network on the daemon behind c that is
// orphaned for this run at now. Failing to remove this run's own resources is
// an error; failing to remove an expired run's resources is only reported, as
// they are not this run's to clean up.
func (o *processOwner) sweep(ctx context.Context, c client.DockerClient, now time.Time) error {
	filters := mobyclient.Filters{}.Add("label", labelManaged+"="+labelTrue)

	var errs []error
	failed := func(labels map[string]string, err error) {
		if labels[labelRun] == o.runID {
			errs = append(errs, err)
		} else {
			report(o.stderr, "%v of expired run %s", err, labels[labelRun])
		}
	}

	containers, err := c.ContainerList(ctx, mobyclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	for i := range containers.Items {
		ct := &containers.Items[i]
		if !orphaned(ct.Labels, o.runID, now) {
			continue
		}
		// A conflict means another run's sweep is already removing it.
		if _, removeErr := c.ContainerRemove(ctx, ct.ID, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); removeErr != nil && !errdefs.IsNotFound(removeErr) && !errdefs.IsConflict(removeErr) {
			failed(ct.Labels, fmt.Errorf("removing container %s: %w", ct.ID, removeErr))
		}
	}

	networks, err := c.NetworkList(ctx, mobyclient.NetworkListOptions{Filters: filters})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("listing networks: %w", err))...)
	}
	for i := range networks.Items {
		n := &networks.Items[i]
		if !orphaned(n.Labels, o.runID, now) {
			continue
		}
		if _, removeErr := c.NetworkRemove(ctx, n.ID, mobyclient.NetworkRemoveOptions{}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			failed(n.Labels, fmt.Errorf("removing network %s: %w", n.ID, removeErr))
		}
	}

	return errors.Join(errs...)
}
