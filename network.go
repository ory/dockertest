// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
)

// CreateNetwork creates a new Docker network.
func (p *Pool) CreateNetwork(ctx context.Context, name string, opts ...NetworkOption) (*Network, error) {
	cfg := newNetworkConfig()
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply network option: %w", err)
		}
	}

	createOpts := types.NetworkCreate{
		Driver:     cfg.driver,
		Internal:   cfg.internal,
		Attachable: cfg.attachable,
		Labels:     cfg.labels,
	}

	resp, err := p.client.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	// Inspect to get full network info
	inspected, err := p.client.NetworkInspect(ctx, resp.ID, types.NetworkInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect network: %w", err)
	}

	// Convert network.Inspect to types.NetworkResource
	netResource := types.NetworkResource{
		Name:       inspected.Name,
		ID:         inspected.ID,
		Created:    inspected.Created,
		Scope:      inspected.Scope,
		Driver:     inspected.Driver,
		EnableIPv6: inspected.EnableIPv6,
		IPAM:       inspected.IPAM,
		Internal:   inspected.Internal,
		Attachable: inspected.Attachable,
		Ingress:    inspected.Ingress,
		Containers: inspected.Containers,
		Options:    inspected.Options,
		Labels:     inspected.Labels,
	}

	return &Network{
		pool:    p,
		Network: netResource,
	}, nil
}

// CreateNetworkT creates a network, failing the test on error.
func (p *Pool) CreateNetworkT(t testing.TB, name string, opts ...NetworkOption) *Network {
	t.Helper()

	ctx := context.Background()
	if ctxT, ok := any(t).(interface{ Context() context.Context }); ok {
		ctx = ctxT.Context()
	}

	network, err := p.CreateNetwork(ctx, name, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

// RemoveNetwork removes a Docker network.
func (p *Pool) RemoveNetwork(ctx context.Context, network *Network) error {
	if err := p.client.NetworkRemove(ctx, network.Network.ID); err != nil {
		return fmt.Errorf("failed to remove network: %w", err)
	}
	return nil
}

// Close removes the network.
func (n *Network) Close(ctx context.Context) error {
	return n.pool.RemoveNetwork(ctx, n)
}

// CloseT removes the network, failing the test on error.
func (n *Network) CloseT(t testing.TB) {
	t.Helper()

	ctx := context.Background()
	if ctxT, ok := any(t).(interface{ Context() context.Context }); ok {
		ctx = ctxT.Context()
	}

	if err := n.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// networkConfig holds configuration for network creation.
type networkConfig struct {
	driver     string
	internal   bool
	attachable bool
	labels     map[string]string
}

// NetworkOption configures network creation.
type NetworkOption func(*networkConfig) error

// WithNetworkDriver sets the network driver (default: "bridge").
func WithNetworkDriver(driver string) NetworkOption {
	return func(c *networkConfig) error {
		c.driver = driver
		return nil
	}
}

// WithNetworkInternal creates an internal network.
func WithNetworkInternal(internal bool) NetworkOption {
	return func(c *networkConfig) error {
		c.internal = internal
		return nil
	}
}

// WithNetworkAttachable makes the network attachable.
func WithNetworkAttachable(attachable bool) NetworkOption {
	return func(c *networkConfig) error {
		c.attachable = attachable
		return nil
	}
}

// WithNetworkLabels sets labels on the network.
func WithNetworkLabels(labels map[string]string) NetworkOption {
	return func(c *networkConfig) error {
		c.labels = labels
		return nil
	}
}

func newNetworkConfig() *networkConfig {
	return &networkConfig{
		driver:     "bridge",
		labels:     make(map[string]string),
		attachable: true,
	}
}
