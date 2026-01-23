// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/go-connections/nat"
)

// Resource represents a running Docker container.
type Resource struct {
	pool      *Pool
	Container types.ContainerJSON
}

// GetPort returns the host port bound to the given container port.
// The portID should be in the format "port/protocol", e.g., "5432/tcp".
func (r *Resource) GetPort(portID string) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	bindings, ok := r.Container.NetworkSettings.Ports[nat.Port(portID)]
	if !ok || len(bindings) == 0 {
		return ""
	}

	return bindings[0].HostPort
}

// GetBoundIP returns the host IP address bound to the given container port.
// Returns "localhost" for 0.0.0.0 or empty IP addresses.
func (r *Resource) GetBoundIP(portID string) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	bindings, ok := r.Container.NetworkSettings.Ports[nat.Port(portID)]
	if !ok || len(bindings) == 0 {
		return ""
	}

	ip := bindings[0].HostIP
	if ip == "0.0.0.0" || ip == "" {
		return "localhost"
	}
	return ip
}

// GetHostPort returns the host address and port in "ip:port" format.
// Returns "localhost:port" for 0.0.0.0 or empty IP addresses.
func (r *Resource) GetHostPort(portID string) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	bindings, ok := r.Container.NetworkSettings.Ports[nat.Port(portID)]
	if !ok || len(bindings) == 0 {
		return ""
	}

	ip := bindings[0].HostIP
	if ip == "0.0.0.0" || ip == "" {
		ip = "localhost"
	}

	return ip + ":" + bindings[0].HostPort
}

// GetIPInNetwork returns the container's IP address in the given network.
func (r *Resource) GetIPInNetwork(network *Network) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	endpoint, ok := r.Container.NetworkSettings.Networks[network.Network.Name]
	if !ok {
		return ""
	}

	return endpoint.IPAddress
}

// Close stops and removes the container.
func (r *Resource) Close(ctx context.Context) error {
	// Unregister from global registry
	registry.unregister(r)

	// Stop container
	timeout := 10 * time.Second
	if err := r.pool.client.ContainerStop(ctx, r.Container.ID, &timeout); err != nil {
		// Continue even if stop fails (container might already be stopped)
	}

	// Remove container
	removeOpts := types.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}
	if err := r.pool.client.ContainerRemove(ctx, r.Container.ID, removeOpts); err != nil {
		return wrapError(ErrTypeUnknown, "failed to remove container", err)
	}

	return nil
}

// CloseT stops and removes the container, failing the test on error.
func (r *Resource) CloseT(t testing.TB) {
	t.Helper()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Cleanup registers a cleanup function with t.Cleanup() to remove the container.
func (r *Resource) Cleanup(t testing.TB) {
	t.Helper()
	t.Cleanup(func() {
		_ = r.Close(context.Background())
	})
}

// Expire sets a TTL on the container after which it will be automatically removed.
func (r *Resource) Expire(ctx context.Context, seconds uint) error {
	timeout := time.Duration(seconds) * time.Second

	// Start a goroutine to stop the container after the specified duration
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-timer.C:
			stopTimeout := 10 * time.Second
			_ = r.pool.client.ContainerStop(context.Background(), r.Container.ID, &stopTimeout)
			removeOpts := types.ContainerRemoveOptions{
				RemoveVolumes: true,
				Force:         true,
			}
			_ = r.pool.client.ContainerRemove(context.Background(), r.Container.ID, removeOpts)
		case <-ctx.Done():
			return
		}
	}()

	return nil
}

// ConnectToNetwork connects the container to a network.
func (r *Resource) ConnectToNetwork(ctx context.Context, network *Network) error {
	err := r.pool.client.NetworkConnect(ctx, network.Network.ID, r.Container.ID, nil)
	if err != nil {
		return wrapError(ErrTypeUnknown, "failed to connect container to network", err)
	}

	// Refresh container info to get updated network settings
	updated, err := r.pool.client.ContainerInspect(ctx, r.Container.ID)
	if err != nil {
		return wrapError(ErrTypeUnknown, "failed to inspect container after network connect", err)
	}
	r.Container = updated

	return nil
}

// DisconnectFromNetwork disconnects the container from a network.
func (r *Resource) DisconnectFromNetwork(ctx context.Context, network *Network) error {
	err := r.pool.client.NetworkDisconnect(ctx, network.Network.ID, r.Container.ID, false)
	if err != nil {
		return wrapError(ErrTypeUnknown, "failed to disconnect container from network", err)
	}

	// Refresh container info
	updated, err := r.pool.client.ContainerInspect(ctx, r.Container.ID)
	if err != nil {
		return wrapError(ErrTypeUnknown, "failed to inspect container after network disconnect", err)
	}
	r.Container = updated

	return nil
}
