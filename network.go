// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"

	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

// Network represents a Docker network managed by dockertest.
// Networks allow containers to communicate with each other using container names
// as hostnames, isolated from other networks.
//
// Create networks with Pool.CreateNetwork and connect containers with
// Resource.ConnectToNetwork.
type Network struct {
	pool    *Pool
	Network network.Inspect
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
func (p *Pool) CreateNetwork(ctx context.Context, name string, opts *NetworkCreateOptions) (*Network, error) {
	// Build network create options
	createOpts := mobyclient.NetworkCreateOptions{}

	// Apply custom options if provided
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

	// Create the network
	createResp, err := p.client.NetworkCreate(ctx, name, createOpts)
	if err != nil {
		return nil, err
	}

	// Inspect the network to get full details
	inspectResp, err := p.client.NetworkInspect(ctx, createResp.ID, mobyclient.NetworkInspectOptions{})
	if err != nil {
		// Clean up network on inspect failure
		_, _ = p.client.NetworkRemove(ctx, createResp.ID, mobyclient.NetworkRemoveOptions{}) //nolint:errcheck // Best effort cleanup
		return nil, err
	}

	return &Network{
		pool:    p,
		Network: inspectResp.Network,
	}, nil
}

// CreateNetworkT creates a network using t.Context() and calls t.Fatalf on error.
func (p *Pool) CreateNetworkT(t TestingTB, name string, opts *NetworkCreateOptions) *Network {
	t.Helper()

	net, err := p.CreateNetwork(t.Context(), name, opts)
	if err != nil {
		t.Fatalf("CreateNetworkT failed: %v", err)
	}

	return net
}

// Close removes the network.
// Any containers still connected to the network should be disconnected first,
// or the network removal will fail.
func (n *Network) Close(ctx context.Context) error {
	if n.pool == nil || n.pool.client == nil {
		return nil
	}

	_, err := n.pool.client.NetworkRemove(ctx, n.Network.ID, mobyclient.NetworkRemoveOptions{})
	return err
}

// CloseT removes the network and calls t.Fatalf on error.
func (n *Network) CloseT(t TestingTB) {
	t.Helper()
	if err := n.Close(t.Context()); err != nil {
		t.Fatalf("CloseT failed: %v", err)
	}
}

// ConnectToNetwork connects the container to the given network.
// The resource's Container field is automatically updated with the latest
// network settings after connection.
func (r *Resource) ConnectToNetwork(ctx context.Context, net *Network) error {
	if r.pool == nil || r.pool.client == nil {
		return nil
	}

	connectOpts := mobyclient.NetworkConnectOptions{
		Container: r.Container.ID,
	}

	_, err := r.pool.client.NetworkConnect(ctx, net.Network.ID, connectOpts)
	if err != nil {
		return err
	}

	// Refresh container inspection to get updated network settings
	inspectResp, err := r.pool.client.ContainerInspect(ctx, r.Container.ID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	r.Container = inspectResp.Container
	return nil
}

// DisconnectFromNetwork disconnects the container from the given network.
// The resource's Container field is automatically updated with the latest
// network settings after disconnection.
func (r *Resource) DisconnectFromNetwork(ctx context.Context, net *Network) error {
	if r.pool == nil || r.pool.client == nil {
		return nil
	}

	disconnectOpts := mobyclient.NetworkDisconnectOptions{
		Container: r.Container.ID,
		Force:     false,
	}

	_, err := r.pool.client.NetworkDisconnect(ctx, net.Network.ID, disconnectOpts)
	if err != nil {
		return err
	}

	// Refresh container inspection to get updated network settings
	inspectResp, err := r.pool.client.ContainerInspect(ctx, r.Container.ID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	r.Container = inspectResp.Container
	return nil
}

// GetIPInNetwork returns the container's IP address in the given network.
// Returns empty string if the container is not connected to the network.
func (r *Resource) GetIPInNetwork(net *Network) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	if r.Container.NetworkSettings.Networks == nil {
		return ""
	}

	networkName := net.Network.Name
	endpoint, ok := r.Container.NetworkSettings.Networks[networkName]
	if !ok {
		return ""
	}

	return endpoint.IPAddress.String()
}
