// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
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
	Fatalf(format string, args ...interface{})
}

// Pool manages Docker resources and operations.
// It provides methods for running containers, building images, creating networks,
// and managing the lifecycle of Docker resources in tests.
//
// A Pool instance owns a Docker client connection and manages resources through
// that connection. Use NewPool or NewPoolT to create a Pool, and Close to clean up.
type Pool struct {
	client      client.DockerClient
	ownedClient bool          // true if Pool created the client and should close it
	MaxWait     time.Duration // maximum wait time for operations
	reuseScope  string
	resources   sync.Map
}

// NewPool creates a new Pool with the given endpoint and options.
//
// The endpoint parameter must be empty. The Docker client is created from
// environment variables (DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH) or
// provided via WithMobyClient option.
//
// To specify a custom Docker endpoint, set the DOCKER_HOST environment variable
// before calling NewPool, or provide a custom client with WithMobyClient.
//
// The default MaxWait is 60 seconds. This can be customized with WithMaxWait.
//
// The Pool creates and owns a Docker client by default. Call Close when done
// to release resources. If you need to provide your own client, use WithMobyClient.
//
// Example:
//
//	ctx := context.Background()
//	pool, err := dockertest.NewPool(ctx, "")
//	if err != nil {
//		panic(err)
//	}
//	defer pool.Close()
func NewPool(ctx context.Context, endpoint string, opts ...PoolOption) (*Pool, error) {
	p := &Pool{
		MaxWait:     60 * time.Second,
		ownedClient: true,
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	// Validate endpoint only if we need to create a client
	if p.client == nil && endpoint != "" {
		return nil, fmt.Errorf("endpoint parameter is not supported; use DOCKER_HOST environment variable or WithMobyClient option")
	}

	// Create client if not provided via options
	if p.client == nil {
		c, err := client.NewMobyClient(ctx)
		if err != nil {
			return nil, err
		}
		p.client = c
		p.ownedClient = true
	}

	p.reuseScope = clientScope(p.client)

	return p, nil
}

func clientScope(c client.DockerClient) string {
	if c == nil {
		return "nil-client"
	}

	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Ptr, reflect.UnsafePointer, reflect.Map, reflect.Chan, reflect.Func, reflect.Slice:
		return fmt.Sprintf("%T:%x", c, v.Pointer())
	default:
		return fmt.Sprintf("%T:%v", c, c)
	}
}

func (p *Pool) trackResource(resource *Resource) {
	if resource == nil || resource.Container.ID == "" {
		return
	}
	p.resources.Store(resource.Container.ID, resource)
}

func (p *Pool) untrackResource(containerID string) {
	if containerID == "" {
		return
	}
	p.resources.Delete(containerID)
}

func (p *Pool) trackedResources() []*Resource {
	var resources []*Resource
	p.resources.Range(func(_, value any) bool {
		resource, ok := value.(*Resource)
		if ok {
			resources = append(resources, resource)
		}
		return true
	})
	return resources
}

// NewPoolT creates a new Pool using t.Context() and registers cleanup with t.Cleanup().
func NewPoolT(t *testing.T, endpoint string, opts ...PoolOption) *Pool {
	t.Helper()

	pool, err := NewPool(t.Context(), endpoint, opts...)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("pool.Close() error = %v", err)
		}
	})

	return pool
}

// Close closes the Pool's Docker client if it was created by the Pool.
// If a custom client was provided via WithMobyClient, it is not closed
// (the caller remains responsible for closing it).
//
// It is safe to call Close multiple times.
func (p *Pool) Close() error {
	if p.ownedClient && p.client != nil {
		return p.client.Close()
	}
	return nil
}

// Cleanup removes all containers tracked by this pool.
// Errors during cleanup do not stop the cleanup process.
// The first error encountered is returned.
func (p *Pool) Cleanup(ctx context.Context) error {
	cleanupCtx := context.WithoutCancel(ctx)
	resources := p.trackedResources()

	var firstErr error
	for _, resource := range resources {
		if err := resource.Close(cleanupCtx); err != nil && firstErr == nil {
			firstErr = err
		}
		p.untrackResource(resource.Container.ID)
	}

	if p.reuseScope != "" {
		resetRegistryWithScope(p.reuseScope)
	}

	return firstErr
}

// Run starts a container with the given repository and options.
// Containers are reused by default based on repository:tag to speed up tests.
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
func (p *Pool) Run(ctx context.Context, repository string, opts ...RunOption) (*Resource, error) {
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

// checkForExisting looks up an existing container in the registry.
func checkForExisting(p *Pool, reuseID string) *Resource {
	if reuseID == "" {
		return nil
	}
	if existing, ok := getWithScope(p.reuseScope, reuseID); ok {
		cloned := *existing
		cloned.pool = p
		return &cloned
	}
	return nil
}

// pullImage pulls a Docker image unless noPull is set.
func (p *Pool) pullImage(ctx context.Context, ref string, noPull bool) error {
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

	if _, err := io.Copy(io.Discard, pullResp); err != nil {
		return fmt.Errorf("%w: drain response for %s: %w", ErrImagePullFailed, ref, err)
	}

	return nil
}

// createAndStartContainer creates and starts a container with the given configuration.
func (p *Pool) createAndStartContainer(ctx context.Context, ref string, cfg *runConfig) (string, error) {
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

	createOpts := mobyclient.ContainerCreateOptions{
		Config: containerConfig,
		HostConfig: &container.HostConfig{
			PublishAllPorts: true,
		},
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
func (p *Pool) inspectAndRegister(ctx context.Context, containerID, reuseID string) (*Resource, error) {
	inspectResp, err := p.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		_, _ = p.client.ContainerRemove(ctx, containerID, mobyclient.ContainerRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		return nil, fmt.Errorf("container inspect failed: %w", err)
	}

	resource := &Resource{
		pool:      p,
		Container: inspectResp.Container,
		reuseID:   reuseID,
	}

	if reuseID != "" {
		canonical, loaded := registerWithScope(p.reuseScope, reuseID, resource)
		if loaded {
			cleanupCtx := context.WithoutCancel(ctx)
			_, _ = p.client.ContainerRemove(cleanupCtx, containerID, mobyclient.ContainerRemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			}) //nolint:errcheck // Best effort cleanup of duplicate container

			cloned := *canonical
			cloned.pool = p
			resource = &cloned
		} else {
			resource = canonical
		}
	}

	p.trackResource(resource)

	return resource, nil
}

// RunT is a test helper that uses t.Context() and calls t.Fatalf on error.
func (p *Pool) RunT(t TestingTB, repository string, opts ...RunOption) *Resource {
	t.Helper()

	r, err := p.Run(t.Context(), repository, opts...)
	if err != nil {
		t.Fatalf("RunT failed: %v", err)
	}

	return r
}
