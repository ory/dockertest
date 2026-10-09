// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

// TestingTB is a subset of testing.TB for dependency injection.
// This interface allows methods to work with test helpers
// without directly depending on the testing package.
type TestingTB interface {
	Helper()
	Context() context.Context
	Cleanup(func())
	Logf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Pool is the interface for managing Docker resources in tests.
// Returned by NewPoolT; does not expose Close or CloseT.
type Pool interface {
	Run(ctx context.Context, repository string, opts ...RunOption) (ClosableResource, error)
	RunT(t TestingTB, repository string, opts ...RunOption) Resource
	BuildAndRun(ctx context.Context, name string, buildOpts *BuildOptions, runOpts ...RunOption) (ClosableResource, error)
	BuildAndRunT(t TestingTB, name string, buildOpts *BuildOptions, runOpts ...RunOption) Resource
	CreateNetwork(ctx context.Context, name string, opts *NetworkCreateOptions) (ClosableNetwork, error)
	CreateNetworkT(t TestingTB, name string, opts *NetworkCreateOptions) Network
	Retry(ctx context.Context, timeout time.Duration, fn func() error) error
	Client() client.DockerClient
}

// ClosablePool extends Pool with explicit lifecycle management.
// Returned by NewPool; the caller is responsible for calling Close.
type ClosablePool interface {
	Pool
	Close(ctx context.Context) error
	CloseT(t TestingTB)
}

// pool manages Docker resources and operations.
//
//nolint:govet // field alignment traded for readability
type pool struct {
	client         client.DockerClient
	ownedClient    bool   // true if pool created the client and should close it
	daemonHost     string // Docker daemon endpoint; scopes the reuse registry
	maxWait        time.Duration
	cleanupTimeout time.Duration

	daemonMu sync.Mutex
	daemonID string // resolved on the first resource-creating operation

	mu        sync.Mutex
	resources []*resource // each entry is one handle; reused containers appear once per acquisition
	networks  map[string]*dockerNetwork
}

// NewPool creates a new pool with the given endpoint and options.
//
// The endpoint parameter must be empty. The Docker client is created from
// environment variables (DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH) or
// provided via WithMobyClient option.
//
// The default maxWait is 60 seconds. This can be customized with WithMaxWait.
//
// Example:
//
//	ctx := context.Background()
//	pool, err := dockertest.NewPool(ctx, "")
//	if err != nil {
//		panic(err)
//	}
//	defer pool.Close(ctx)
func NewPool(ctx context.Context, endpoint string, opts ...PoolOption) (ClosablePool, error) {
	p := &pool{
		maxWait:        60 * time.Second,
		cleanupTimeout: 60 * time.Second,
		ownedClient:    true,
		networks:       map[string]*dockerNetwork{},
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.cleanupTimeout <= 0 {
		return nil, fmt.Errorf("%w: cleanup timeout must be positive, got %v", ErrInvalidOption, p.cleanupTimeout)
	}

	if p.client == nil && endpoint != "" {
		return nil, fmt.Errorf("endpoint parameter is not supported; use DOCKER_HOST environment variable or WithMobyClient option")
	}

	if p.client == nil {
		c, err := client.NewMobyClient(ctx)
		if err != nil {
			return nil, err
		}
		p.client = c
		p.ownedClient = true
	}

	p.daemonHost = p.client.DaemonHost()
	owner.addPool(p)

	return p, nil
}

// Client returns the underlying Docker client.
func (p *pool) Client() client.DockerClient {
	return p.client
}

// begin admits a resource-creating operation with the process owner and
// resolves the daemon identity. Under Main, the daemon is registered so that
// abandoned runs are recovered before the first resource is created on it.
func (p *pool) begin(ctx context.Context) (context.Context, func(), error) {
	if p.client == nil {
		return nil, nil, ErrClientClosed
	}
	ctx, done, err := owner.beginOperation(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := p.recordDaemon(ctx); err != nil {
		done()
		return nil, nil, err
	}
	return ctx, done, nil
}

func (p *pool) recordDaemon(ctx context.Context) error {
	p.daemonMu.Lock()
	if p.daemonID == "" {
		info, err := p.client.Info(ctx, mobyclient.InfoOptions{})
		if err != nil {
			p.daemonMu.Unlock()
			return fmt.Errorf("docker info failed: %w", err)
		}
		p.daemonID = info.Info.ID
	}
	daemonID := p.daemonID
	p.daemonMu.Unlock()
	return owner.registerDaemon(ctx, daemonID, p.client)
}

// cleanupContext bounds a cleanup by the pool's cleanup timeout. Deriving from
// ctx means an outer cleanup deadline is preserved and never extended.
func (p *pool) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, p.cleanupTimeout)
}

// rollbackContext is used to undo a partially created resource after a failed
// operation: it ignores the (possibly canceled) operation context but keeps
// its deadline bounded.
func (p *pool) rollbackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return p.cleanupContext(context.WithoutCancel(ctx))
}

func (p *pool) trackResource(r *resource) {
	if r == nil || r.container.ID == "" {
		return
	}
	p.mu.Lock()
	p.resources = append(p.resources, r)
	p.mu.Unlock()
}

func (p *pool) untrackResource(r *resource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, tracked := range p.resources {
		if tracked == r {
			p.resources = append(p.resources[:i], p.resources[i+1:]...)
			return
		}
	}
}

func (p *pool) trackNetwork(net *dockerNetwork) {
	if net == nil || net.inspect.ID == "" {
		return
	}
	p.mu.Lock()
	p.networks[net.inspect.ID] = net
	p.mu.Unlock()
}

func (p *pool) untrackNetwork(networkID string) {
	p.mu.Lock()
	delete(p.networks, networkID)
	p.mu.Unlock()
}

func (p *pool) trackedNetworks() []*dockerNetwork {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Collect(maps.Values(p.networks))
}

func (p *pool) trackedResources() []*resource {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.resources)
}

// NewPoolT creates a new pool using t.Context() and registers cleanup with t.Cleanup().
// The returned Pool does not expose Close or CloseT; the pool is automatically
// cleaned up when the test finishes.
func NewPoolT(t TestingTB, endpoint string, opts ...PoolOption) Pool {
	t.Helper()

	p, err := NewPool(t.Context(), endpoint, opts...)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	t.Cleanup(func() {
		if err := p.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Logf("pool.Close() error: %v", err)
		}
	})

	return p
}

// Close cleans up all tracked containers and networks, bounded by the pool's
// cleanup timeout and the supplied context. If cleanup succeeds and the pool
// created its own Docker client, the client is closed unless Main is installed,
// in which case Main closes it at process shutdown. After a failed cleanup the
// client stays open so that Close can be called again to retry. A custom client
// provided via WithMobyClient is never closed.
//
// It is safe to call Close multiple times.
func (p *pool) Close(ctx context.Context) error {
	ctx, cancel := p.cleanupContext(ctx)
	defer cancel()
	if err := p.cleanup(ctx); err != nil {
		return err
	}
	if owner.scopeName() != "" {
		return nil
	}
	return p.closeOwnedClient()
}

func (p *pool) closeOwnedClient() error {
	if !p.ownedClient || p.client == nil {
		return nil
	}
	err := p.client.Close()
	p.client = nil
	return err
}

// CloseT cleans up all tracked containers and networks, then closes the pool's
// Docker client. It calls t.Fatalf on error.
func (p *pool) CloseT(t TestingTB) {
	t.Helper()
	if err := p.Close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("Pool.CloseT failed: %v", err)
	}
}

// cleanup closes all tracked resources and networks, then retries the removal
// of anything created by this pool whose earlier removal failed.
// Errors during cleanup do not stop the process; all errors are joined.
func (p *pool) cleanup(ctx context.Context) error {
	if p.client == nil {
		return nil
	}

	var errs []error
	for _, r := range p.trackedResources() {
		errs = append(errs, r.Close(ctx))
	}
	for _, net := range p.trackedNetworks() {
		errs = append(errs, net.Close(ctx))
	}
	errs = append(errs, owner.retry(ctx, p))
	return errors.Join(errs...)
}

// Run starts a container with the given repository and options.
//
// By default, containers are reused based on repository:tag to speed up tests.
// Reused containers are reference-counted: the Docker container is only removed
// when the last caller closes its reference. To ensure a fresh container, use
// WithoutReuse(). To control reuse with a custom key that accounts for your
// configuration, use WithReuseID().
//
// Example:
//
//	resource, err := pool.Run(ctx, "postgres",
//		dockertest.WithTag("14"),
//		dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
//	)
//	if err != nil {
//		panic(err)
//	}
//	defer resource.Close(ctx)
func (p *pool) Run(ctx context.Context, repository string, opts ...RunOption) (ClosableResource, error) {
	cfg, err := buildRunConfig(opts)
	if err != nil {
		return nil, err
	}

	ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	return p.run(ctx, repository, cfg)
}

// run creates or reuses a container for an admitted operation.
func (p *pool) run(ctx context.Context, repository string, cfg *runConfig) (*resource, error) {
	reuseID := computeReuseID(repository, cfg)

	if existing := checkForExisting(p, reuseID); existing != nil {
		p.trackResource(existing)
		return existing, nil
	}

	ref := cfg.image
	if ref == "" {
		// The tag may name an owned build image, which must not be deleted
		// between resolving the tag and attaching the new container to the
		// image (or rolling the container back).
		end, err := owner.beginImageAcquisition(ctx, p)
		if err != nil {
			return nil, err
		}
		defer end()
		ref = fmt.Sprintf("%s:%s", repository, cfg.tag)
		if err := p.pullImage(ctx, ref); err != nil {
			return nil, err
		}
	}

	containerID, err := p.createAndStartContainer(ctx, ref, cfg, reuseID)
	if err != nil {
		return nil, err
	}

	return p.inspectAndRegister(ctx, containerID, reuseID)
}

// buildRunConfig constructs a runConfig from options.
func buildRunConfig(opts []RunOption) (*runConfig, error) {
	cfg := &runConfig{
		tag: "latest",
	}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// computeReuseID determines the reuse ID for container caching. Containers
// created from a locally built image include the immutable image ID so that a
// changed build never reuses a container of an older build.
func computeReuseID(repository string, cfg *runConfig) string {
	if cfg.noReuse {
		return ""
	}
	id := cfg.reuseID
	if id == "" {
		id = fmt.Sprintf("%s:%s", repository, cfg.tag)
	}
	if cfg.image != "" {
		id += "@" + cfg.image
	}
	return id
}

// registryKey returns a registry key scoped to this pool's daemon host.
// This ensures that pools connected to different Docker daemons never
// share containers through the global registry.
func (p *pool) registryKey(reuseID string) string {
	return p.daemonHost + "\x00" + reuseID
}

// checkForExisting looks up an existing container in the registry and
// increments its reference count. The returned resource is a new handle for
// the canonical registry entry, bound to the caller's pool.
func checkForExisting(p *pool, reuseID string) *resource {
	if reuseID == "" {
		return nil
	}
	if existing, ok := acquire(p.registryKey(reuseID)); ok {
		return existing.handle(p)
	}
	return nil
}

// pullImage pulls a Docker image unless it is already present.
func (p *pool) pullImage(ctx context.Context, ref string) error {
	if _, err := p.client.ImageInspect(ctx, ref); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("image inspect failed for %s: %w", ref, err)
	}

	pullResp, err := p.client.ImagePull(ctx, ref, mobyclient.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrImagePullFailed, ref, err)
	}
	defer func() {
		_ = pullResp.Close() //nolint:errcheck // Best effort close in defer
	}()

	// Wait consumes the JSON stream and surfaces any errors embedded in it.
	if err := pullResp.Wait(ctx); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrImagePullFailed, ref, err)
	}

	return nil
}

// createAndStartContainer creates and starts a container with the given
// configuration. The container is tracked by the process owner as soon as it
// exists; if starting it fails, it is removed again and the removal error, if
// any, is joined with the start error.
func (p *pool) createAndStartContainer(ctx context.Context, ref string, cfg *runConfig, reuseID string) (string, error) {
	containerConfig := &container.Config{
		Image:      ref,
		Env:        cfg.env,
		Cmd:        cfg.cmd,
		User:       cfg.user,
		WorkingDir: cfg.workingDir,
		Labels:     cfg.labels,
		Hostname:   cfg.hostname,
	}

	if len(cfg.entrypoint) > 0 {
		containerConfig.Entrypoint = cfg.entrypoint
	}

	// Apply config modifier last to allow overriding anything
	if cfg.configModifier != nil {
		cfg.configModifier(containerConfig)
	}
	containerConfig.Labels = owner.withOwnershipLabels(containerConfig.Labels, false)

	hostConfig := &container.HostConfig{
		PublishAllPorts: true,
	}

	if len(cfg.portBindings) > 0 {
		hostConfig.PortBindings = cfg.portBindings
	}

	if len(cfg.binds) > 0 {
		hostConfig.Binds = cfg.binds
	}

	// Apply host config modifier last to allow overriding anything
	if cfg.hostConfigModifier != nil {
		cfg.hostConfigModifier(hostConfig)
	}

	createOpts := mobyclient.ContainerCreateOptions{
		Name:       cfg.name,
		Config:     containerConfig,
		HostConfig: hostConfig,
	}

	createResp, err := p.client.ContainerCreate(ctx, createOpts)
	if err != nil {
		return "", fmt.Errorf("%w from %s: %w", ErrContainerCreateFailed, ref, err)
	}
	var registryKey string
	if reuseID != "" {
		registryKey = p.registryKey(reuseID)
	}
	owner.trackContainer(createResp.ID, p, registryKey)

	_, err = p.client.ContainerStart(ctx, createResp.ID, mobyclient.ContainerStartOptions{})
	if err != nil {
		return "", errors.Join(fmt.Errorf("%w: %s: %w", ErrContainerStartFailed, createResp.ID, err), p.rollbackContainer(ctx, createResp.ID))
	}

	return createResp.ID, nil
}

// rollbackContainer removes a container that a failed operation left behind.
// The removal ignores cancellation of ctx but is bounded by the cleanup timeout.
func (p *pool) rollbackContainer(ctx context.Context, containerID string) error {
	ctx, cancel := p.rollbackContext(ctx)
	defer cancel()
	return owner.removeContainer(ctx, p, containerID)
}

// inspectAndRegister inspects the container and registers it in the global registry.
func (p *pool) inspectAndRegister(ctx context.Context, containerID, reuseID string) (*resource, error) {
	inspectResp, err := p.inspectWithPortRetry(ctx, containerID)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("container inspect failed: %w", err), p.rollbackContainer(ctx, containerID))
	}
	owner.attachImage(containerID, inspectResp.Container.Image)

	r := &resource{
		pool:      p,
		container: inspectResp.Container,
		reuseID:   reuseID,
	}

	if reuseID != "" {
		canonical, loaded := register(p.registryKey(reuseID), r)
		if loaded {
			// Another caller registered the same reuse ID first; drop ours.
			if err := p.rollbackContainer(ctx, containerID); err != nil {
				owner.warn("removing duplicate container: %v", err)
			}
			r = canonical.handle(p)
		}
	}

	p.trackResource(r)

	return r, nil
}

// inspectWithPortRetry inspects a container, retrying briefly if exposed ports
// have not yet been bound. Docker may report empty port bindings immediately
// after ContainerStart; this mirrors v3's inspectContainerWithRetries behavior.
func (p *pool) inspectWithPortRetry(ctx context.Context, containerID string) (mobyclient.ContainerInspectResult, error) {
	const maxRetries = 5
	const retryDelay = 100 * time.Millisecond

	for range maxRetries {
		resp, err := p.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
		if err != nil {
			return resp, err
		}

		// If the container has no exposed ports, no need to wait for bindings.
		if resp.Container.Config == nil || len(resp.Container.Config.ExposedPorts) == 0 {
			return resp, nil
		}

		// If port bindings are populated, we're good.
		if resp.Container.NetworkSettings != nil && len(resp.Container.NetworkSettings.Ports) > 0 {
			return resp, nil
		}

		// Wait and retry.
		select {
		case <-ctx.Done():
			return resp, ctx.Err()
		case <-time.After(retryDelay):
		}
	}

	// Return the last result even if ports aren't populated.
	return p.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
}

// RunT is a test helper that uses t.Context() and calls t.Fatalf on error.
// The returned Resource does not expose Close, CloseT, or Cleanup;
// the resource is automatically cleaned up when the test finishes.
func (p *pool) RunT(t TestingTB, repository string, opts ...RunOption) Resource {
	t.Helper()

	r, err := p.Run(t.Context(), repository, opts...)
	if err != nil {
		t.Fatalf("RunT failed: %v", err)
	}

	r.Cleanup(t)

	return r
}
