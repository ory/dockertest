// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
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
			return nil, wrapError(ErrTypeUnknown, "failed to apply pool option", err)
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
				client.WithAPIVersionNegotiation(),
			)
		}

		if err != nil {
			return nil, wrapError(ErrTypeUnknown, "failed to create Docker client", err)
		}

		pool.client = c
		pool.mobyClient = c.Client
	}

	// Ping to verify connection
	if _, err := pool.client.Ping(ctx); err != nil {
		return nil, wrapError(ErrTypeConnectionRefused, "failed to connect to Docker daemon", err)
	}

	return pool, nil
}

// Ping verifies the connection to the Docker daemon.
func (p *Pool) Ping(ctx context.Context) error {
	_, err := p.client.Ping(ctx)
	if err != nil {
		return wrapError(ErrTypeConnectionRefused, "failed to ping Docker daemon", err)
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
