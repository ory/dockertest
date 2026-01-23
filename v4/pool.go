// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	internalclient "github.com/ory/dockertest/v4/internal/client"
)

const (
	// DefaultMaxWait is the default maximum time to wait for a container to be ready.
	DefaultMaxWait = 60 * time.Second
)

// Pool manages Docker connections and container lifecycle.
type Pool struct {
	client     internalclient.Client
	maxWait    time.Duration
	mobyClient *client.Client // Exposed for advanced users
}

// PoolOption configures a Pool using the functional options pattern.
type PoolOption func(*Pool) error

// WithMaxWait sets the maximum time to wait for container operations.
func WithMaxWait(d time.Duration) PoolOption {
	return func(p *Pool) error {
		p.maxWait = d
		return nil
	}
}

// WithMobyClient allows using a custom Moby client.
// This is useful for advanced users who need custom client configuration.
func WithMobyClient(c *client.Client) PoolOption {
	return func(p *Pool) error {
		p.mobyClient = c
		p.client = &internalclient.MobyClient{Client: c}
		return nil
	}
}

// NewPool creates a Pool with sensible defaults.
// The endpoint parameter specifies the Docker host. If empty, it uses the
// default from environment variables (DOCKER_HOST, etc.).
func NewPool(endpoint string, opts ...PoolOption) (*Pool, error) {
	return NewPoolWithContext(context.Background(), endpoint, opts...)
}

// NewPoolWithContext creates a Pool with a context.
func NewPoolWithContext(ctx context.Context, endpoint string, opts ...PoolOption) (*Pool, error) {
	pool := &Pool{
		maxWait: DefaultMaxWait,
	}

	// Apply options first (they might set a custom client)
	for _, opt := range opts {
		if err := opt(pool); err != nil {
			return nil, fmt.Errorf("failed to apply pool option: %w", err)
		}
	}

	// If no client was set by options, create default Moby client
	if pool.client == nil {
		var c *internalclient.MobyClient
		var err error

		if endpoint == "" {
			c, err = internalclient.NewMobyClient()
		} else {
			c, err = internalclient.NewMobyClientWithOptions(
				client.WithHost(endpoint),
				client.WithVersion("1.44"),
			)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create Docker client: %w", err)
		}

		pool.client = c
		pool.mobyClient = c.Client
	}

	// Ping to verify connection
	if _, err := pool.client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w: %w", ErrConnectionRefused, err)
	}

	return pool, nil
}

// Ping verifies the connection to the Docker daemon.
func (p *Pool) Ping(ctx context.Context) error {
	_, err := p.client.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping Docker daemon: %w: %w", ErrConnectionRefused, err)
	}
	return nil
}

// Client returns the underlying Moby client for advanced use cases.
func (p *Pool) Client() *client.Client {
	return p.mobyClient
}

// Network represents a Docker network.
type Network struct {
	pool    *Pool
	Network types.NetworkResource
}

// Run starts a container with the given options.
// Containers are reused by default based on repository:tag.
func (p *Pool) Run(ctx context.Context, repository string, opts ...RunOption) (*Resource, error) {
	cfg := newRunConfig()
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply run option: %w", err)
		}
	}

	// Build image reference
	imageRef := repository
	if cfg.tag != "" {
		imageRef = repository + ":" + cfg.tag
	}

	// Build reuse ID
	reuseID := imageRef
	if cfg.reuseID != "" {
		reuseID = cfg.reuseID
	}

	// Check for existing container if reuse is enabled
	if !cfg.noReuse {
		if existing := registry.lookup(reuseID); existing != nil {
			// Inspect to verify it's still running
			inspected, err := p.client.ContainerInspect(ctx, existing.Container.ID)
			if err == nil && inspected.State.Running {
				// Update resource with fresh inspection
				existing.Container = inspected
				return existing, nil
			}
			// Container no longer exists or not running, remove from registry
			registry.unregister(existing)
		}
	}

	// Pull image if needed
	if err := p.pullImage(ctx, imageRef); err != nil {
		return nil, err
	}

	// Create container config
	containerConfig, hostConfig, networkingConfig, platform := p.buildContainerConfig(cfg, imageRef)

	// Create container
	createResp, err := p.client.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkingConfig,
		platform,
		cfg.name,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w: %w", ErrContainerCreateFailed, err)
	}

	// Start container
	if err := p.client.ContainerStart(ctx, createResp.ID, types.ContainerStartOptions{}); err != nil {
		// Clean up created container on start failure
		_ = p.client.ContainerRemove(ctx, createResp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container: %w: %w", ErrContainerStartFailed, err)
	}

	// Inspect to get full container info
	inspected, err := p.client.ContainerInspect(ctx, createResp.ID)
	if err != nil {
		// Clean up started container on inspect failure
		_ = p.client.ContainerRemove(ctx, createResp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	resource := &Resource{
		pool:      p,
		Container: inspected,
	}

	// Register for reuse if enabled
	if !cfg.noReuse {
		registry.register(reuseID, resource)
	}

	// Set expiry if configured
	if cfg.hasExpiry && !cfg.noExpiry {
		_ = resource.Expire(ctx, uint(cfg.expiry.Seconds()))
	} else if !cfg.noExpiry {
		// Default expiry
		_ = resource.Expire(ctx, uint(DefaultExpiry.Seconds()))
	}

	return resource, nil
}

// RunT is a test helper that starts a container, failing the test on error.
// It uses t.Context() for automatic cancellation when the test ends.
func (p *Pool) RunT(t testing.TB, repository string, opts ...RunOption) *Resource {
	t.Helper()

	ctx := context.Background()
	// Try to get context from testing.TB if available (Go 1.23+)
	if ctxT, ok := any(t).(interface{ Context() context.Context }); ok {
		ctx = ctxT.Context()
	}

	resource, err := p.Run(ctx, repository, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return resource
}

func (p *Pool) pullImage(ctx context.Context, imageRef string) error {
	// Check if image exists locally
	_, _, err := p.client.ImageInspectWithRaw(ctx, imageRef)
	if err == nil {
		// Image exists
		return nil
	}

	// Pull image
	reader, err := p.client.ImagePull(ctx, imageRef, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w: %w", imageRef, ErrImagePullFailed, err)
	}
	defer reader.Close()

	// Consume the output to ensure pull completes
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("failed to read pull output for %s: %w: %w", imageRef, ErrImagePullFailed, err)
	}

	return nil
}

func (p *Pool) buildContainerConfig(cfg *runConfig, imageRef string) (
	*container.Config,
	*container.HostConfig,
	*network.NetworkingConfig,
	*specs.Platform,
) {
	// Container config
	containerConfig := &container.Config{
		Image:  imageRef,
		Env:    cfg.env,
		Cmd:    cfg.cmd,
		Labels: cfg.labels,
		Tty:    cfg.tty,
	}

	// Exposed ports
	if len(cfg.exposedPorts) > 0 {
		exposedPorts := make(nat.PortSet)
		for _, port := range cfg.exposedPorts {
			// Add protocol if not specified
			if !strings.Contains(port, "/") {
				port = port + "/tcp"
			}
			exposedPorts[nat.Port(port)] = struct{}{}
		}
		containerConfig.ExposedPorts = exposedPorts
	}

	// Host config
	hostConfig := &container.HostConfig{
		Privileged:      cfg.privileged,
		PublishAllPorts: true, // Auto-bind to random host ports
		Mounts:          cfg.mounts,
	}

	// Port bindings
	if cfg.portBindings != nil {
		hostConfig.PortBindings = cfg.portBindings
	}

	// Networking config
	networkingConfig := &network.NetworkingConfig{}
	if len(cfg.networks) > 0 {
		endpoints := make(map[string]*network.EndpointSettings)
		for _, net := range cfg.networks {
			endpoints[net.Network.Name] = &network.EndpointSettings{}
		}
		networkingConfig.EndpointsConfig = endpoints
	}

	// Platform
	var platform *specs.Platform
	if cfg.platform != "" {
		platform = &specs.Platform{
			Architecture: cfg.platform,
		}
	}

	return containerConfig, hostConfig, networkingConfig, platform
}
