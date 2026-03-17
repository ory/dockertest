// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"fmt"
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
type pool struct {
	client      client.DockerClient
	ownedClient bool   // true if pool created the client and should close it
	daemonHost  string // Docker daemon endpoint; scopes the reuse registry
	maxWait     time.Duration

	mu        sync.Mutex
	resources []*resource // each entry is one reference; reused containers appear once per acquisition
	networks  sync.Map
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
		maxWait:     60 * time.Second,
		ownedClient: true,
	}

	for _, opt := range opts {
		opt(p)
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

	return p, nil
}

// Client returns the underlying Docker client.
func (p *pool) Client() client.DockerClient {
	return p.client
}

func (p *pool) trackResource(r *resource) {
	if r == nil || r.container.ID == "" {
		return
	}
	p.mu.Lock()
	p.resources = append(p.resources, r)
	p.mu.Unlock()
}

func (p *pool) untrackResource(containerID string) {
	if containerID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Remove the first matching entry (preserves other refs to the same container).
	for i, r := range p.resources {
		if r.container.ID == containerID {
			p.resources = append(p.resources[:i], p.resources[i+1:]...)
			return
		}
	}
}

func (p *pool) trackNetwork(net *dockerNetwork) {
	if net == nil || net.inspect.ID == "" {
		return
	}
	p.networks.Store(net.inspect.ID, net)
}

func (p *pool) untrackNetwork(networkID string) {
	if networkID == "" {
		return
	}
	p.networks.Delete(networkID)
}

func (p *pool) trackedNetworks() []*dockerNetwork {
	var networks []*dockerNetwork
	p.networks.Range(func(_, value any) bool {
		net, ok := value.(*dockerNetwork)
		if ok {
			networks = append(networks, net)
		}
		return true
	})
	return networks
}

func (p *pool) trackedResources() []*resource {
	p.mu.Lock()
	snapshot := make([]*resource, len(p.resources))
	copy(snapshot, p.resources)
	p.mu.Unlock()
	return snapshot
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

// Close cleans up all tracked containers and networks, then closes the pool's
// Docker client if it was created by the pool. If a custom client was provided
// via WithMobyClient, it is not closed (the caller remains responsible for
// closing it).
//
// It is safe to call Close multiple times.
func (p *pool) Close(ctx context.Context) error {
	cleanupErr := p.cleanup(ctx)
	if p.ownedClient && p.client != nil {
		err := p.client.Close()
		p.client = nil
		if err != nil {
			return err
		}
	}
	return cleanupErr
}

// CloseT cleans up all tracked containers and networks, then closes the pool's
// Docker client. It calls t.Fatalf on error.
func (p *pool) CloseT(t TestingTB) {
	t.Helper()
	if err := p.Close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("Pool.CloseT failed: %v", err)
	}
}

// cleanup closes all tracked resources and networks.
// Resources are closed first (respecting ref counts), then networks.
// Errors during cleanup do not stop the process. The first error is returned.
func (p *pool) cleanup(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()

	var firstErr error

	for _, r := range p.trackedResources() {
		if err := r.Close(cleanupCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	for _, net := range p.trackedNetworks() {
		if err := net.Close(cleanupCtx); err != nil && firstErr == nil {
			firstErr = err
		}
		p.untrackNetwork(net.inspect.ID)
	}

	return firstErr
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

	reuseID := computeReuseID(repository, cfg)

	if existing := checkForExisting(p, reuseID); existing != nil {
		p.trackResource(existing)
		return existing, nil
	}

	ref := fmt.Sprintf("%s:%s", repository, cfg.tag)

	if pullErr := p.pullImage(ctx, ref, cfg.noPull); pullErr != nil {
		return nil, pullErr
	}

	containerID, err := p.createAndStartContainer(ctx, ref, cfg)
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

// computeReuseID determines the reuse ID for container caching.
func computeReuseID(repository string, cfg *runConfig) string {
	if cfg.noReuse {
		return ""
	}
	if cfg.reuseID != "" {
		return cfg.reuseID
	}
	return fmt.Sprintf("%s:%s", repository, cfg.tag)
}

// registryKey returns a registry key scoped to this pool's daemon host.
// This ensures that pools connected to different Docker daemons never
// share containers through the global registry.
func (p *pool) registryKey(reuseID string) string {
	return p.daemonHost + "\x00" + reuseID
}

// checkForExisting looks up an existing container in the registry and
// increments its reference count. The returned resource is a shallow clone
// of the canonical registry entry with the pool pointer updated to the
// caller's pool.
func checkForExisting(p *pool, reuseID string) *resource {
	if reuseID == "" {
		return nil
	}
	if existing, ok := acquire(p.registryKey(reuseID)); ok {
		cloned := *existing
		cloned.pool = p
		return &cloned
	}
	return nil
}

// pullImage pulls a Docker image unless noPull is set.
func (p *pool) pullImage(ctx context.Context, ref string, noPull bool) error {
	if noPull {
		return nil
	}

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

// createAndStartContainer creates and starts a container with the given configuration.
func (p *pool) createAndStartContainer(ctx context.Context, ref string, cfg *runConfig) (string, error) {
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

	_, err = p.client.ContainerStart(ctx, createResp.ID, mobyclient.ContainerStartOptions{})
	if err != nil {
		_, _ = p.client.ContainerRemove(ctx, createResp.ID, mobyclient.ContainerRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		return "", fmt.Errorf("%w: %s: %w", ErrContainerStartFailed, createResp.ID, err)
	}

	return createResp.ID, nil
}

// inspectAndRegister inspects the container and registers it in the global registry.
func (p *pool) inspectAndRegister(ctx context.Context, containerID, reuseID string) (*resource, error) {
	inspectResp, err := p.inspectWithPortRetry(ctx, containerID)
	if err != nil {
		_, _ = p.client.ContainerRemove(ctx, containerID, mobyclient.ContainerRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		return nil, fmt.Errorf("container inspect failed: %w", err)
	}

	r := &resource{
		pool:      p,
		container: inspectResp.Container,
		reuseID:   reuseID,
	}

	if reuseID != "" {
		canonical, loaded := register(p.registryKey(reuseID), r)
		if loaded {
			cleanupCtx := context.WithoutCancel(ctx)
			_, _ = p.client.ContainerRemove(cleanupCtx, containerID, mobyclient.ContainerRemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			}) //nolint:errcheck // Best effort cleanup of duplicate container

			cloned := *canonical
			cloned.pool = p
			r = &cloned
		} else {
			r = canonical
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
