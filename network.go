// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/netip"
	"strings"
	"sync/atomic"

	mobynetwork "github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

// Network provides access to a Docker network.
// Returned by CreateNetworkT; does not expose Close or CloseT.
type Network interface {
	ID() string
	Inspect() mobynetwork.Inspect
}

// ClosableNetwork extends Network with explicit lifecycle management.
// Returned by CreateNetwork; the caller is responsible for calling Close.
type ClosableNetwork interface {
	Network
	Close(ctx context.Context) error
	CloseT(t TestingTB)
}

// dockerNetwork represents a Docker network managed by dockertest.
type dockerNetwork struct {
	pool    *pool
	inspect mobynetwork.Inspect
	closed  atomic.Bool
}

// ID returns the network ID.
func (n *dockerNetwork) ID() string {
	return n.inspect.ID
}

// Inspect returns the network inspection response.
func (n *dockerNetwork) Inspect() mobynetwork.Inspect {
	return n.inspect
}

// NetworkCreateOptions holds options for creating a network.
// This is a subset of mobyclient.NetworkCreateOptions from github.com/moby/moby/client
// to provide a simpler API while allowing common customizations.
//
// All fields are optional. If not specified, Docker's defaults are used:
//   - Driver defaults to "bridge"
//   - Internal defaults to false (network has external access)
//   - Attachable defaults to false
//   - EnableIPv6 defaults to false
//
//nolint:govet // field alignment traded for readability
type NetworkCreateOptions struct {
	Driver     string            // Network driver (e.g., "bridge", "overlay")
	Labels     map[string]string // User-defined metadata
	Options    map[string]string // Driver-specific options
	Internal   bool              // Restrict external access to the network
	Attachable bool              // Enable manual container attachment
	Ingress    bool              // Create an ingress network (swarm mode)
	EnableIPv6 bool              // Enable IPv6 networking
}

// CreateNetwork creates a new Docker network with the given name and options.
// If opts is nil, default network options are used (bridge driver, external access allowed).
func (p *pool) CreateNetwork(ctx context.Context, name string, opts *NetworkCreateOptions) (ClosableNetwork, error) {
	ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	createOpts := mobyclient.NetworkCreateOptions{}

	if opts != nil {
		createOpts.Driver = opts.Driver
		createOpts.Internal = opts.Internal
		createOpts.Attachable = opts.Attachable
		createOpts.Ingress = opts.Ingress
		if opts.EnableIPv6 {
			enableIPv6 := true
			createOpts.EnableIPv6 = &enableIPv6
		}
		createOpts.Labels = opts.Labels
		createOpts.Options = opts.Options
	}
	createOpts.Labels = owner.withOwnershipLabels(createOpts.Labels, false)

	createResp, err := p.client.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		createResp, err = p.retryNetworkCreateWithCustomSubnet(ctx, name, createOpts, err)
		if err != nil {
			return nil, err
		}
	}
	owner.trackNetwork(createResp.ID, p)

	inspectResp, err := p.client.NetworkInspect(ctx, createResp.ID, mobyclient.NetworkInspectOptions{})
	if err != nil {
		rollbackCtx, cancel := p.rollbackContext(ctx)
		defer cancel()
		return nil, errors.Join(fmt.Errorf("network inspect failed: %w", err), owner.removeNetwork(rollbackCtx, p.client, createResp.ID))
	}

	net := &dockerNetwork{
		pool:    p,
		inspect: inspectResp.Network,
	}
	p.trackNetwork(net)

	return net, nil
}

func (p *pool) retryNetworkCreateWithCustomSubnet(
	ctx context.Context,
	name string,
	createOpts mobyclient.NetworkCreateOptions,
	createErr error,
) (mobyclient.NetworkCreateResult, error) {
	// NOTE: Error string matching is fragile and may break across Docker versions.
	// Tested against Docker Engine 27.x. There is no structured error type for this.
	if createOpts.IPAM != nil || !strings.Contains(createErr.Error(), "all predefined address pools have been fully subnetted") {
		return mobyclient.NetworkCreateResult{}, createErr
	}

	h := fnv.New64a()
	h.Write([]byte(name))
	seed := int64(h.Sum64())
	for i := 0; i < 128; i++ {
		thirdOctet := (int(seed>>8) + i) % 256
		secondOctet := 16 + ((int(seed>>16) + i) % 16) // 172.16.0.0/12 private range
		subnet := fmt.Sprintf("172.%d.%d.0/24", secondOctet, thirdOctet)
		prefix, err := netip.ParsePrefix(subnet)
		if err != nil {
			continue
		}

		retryOpts := createOpts
		retryOpts.IPAM = &mobynetwork.IPAM{
			Driver: "default",
			Config: []mobynetwork.IPAMConfig{
				{Subnet: prefix},
			},
		}

		createResp, err := p.client.NetworkCreate(ctx, name, retryOpts)
		if err == nil {
			return createResp, nil
		}

		if strings.Contains(err.Error(), "Pool overlaps with other one on this address space") ||
			strings.Contains(err.Error(), "all predefined address pools have been fully subnetted") {
			continue
		}

		return mobyclient.NetworkCreateResult{}, err
	}

	return mobyclient.NetworkCreateResult{}, createErr
}

// CreateNetworkT creates a network using t.Context() and calls t.Fatalf on error.
// The returned Network does not expose Close or CloseT; the network is
// cleaned up automatically when the pool is closed.
func (p *pool) CreateNetworkT(t TestingTB, name string, opts *NetworkCreateOptions) Network {
	t.Helper()

	net, err := p.CreateNetwork(t.Context(), name, opts)
	if err != nil {
		t.Fatalf("CreateNetworkT failed: %v", err)
	}

	return net
}

// Close removes the network. It releases the handle exactly once; repeated
// calls return nil. The removal honors ctx and is bounded by the pool's
// cleanup timeout. A failed removal is retried by the pool's Close and by Main.
// Any containers still connected to the network should be disconnected first,
// or the network removal will fail.
func (n *dockerNetwork) Close(ctx context.Context) error {
	if !n.closed.CompareAndSwap(false, true) {
		return nil
	}
	if n.pool == nil || n.pool.client == nil {
		return ErrClientClosed
	}
	defer n.pool.untrackNetwork(n.inspect.ID)

	ctx, cancel := n.pool.cleanupContext(ctx)
	defer cancel()
	return owner.removeNetwork(ctx, n.pool.client, n.inspect.ID)
}

// CloseT removes the network and calls t.Fatalf on error.
func (n *dockerNetwork) CloseT(t TestingTB) {
	t.Helper()
	if err := n.Close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("CloseT failed: %v", err)
	}
}
