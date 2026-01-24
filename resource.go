// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

// GetPort returns the host port bound to the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *Resource) GetPort(portID string) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	port, err := network.ParsePort(portID)
	if err != nil {
		return ""
	}
	bindings := r.Container.NetworkSettings.Ports[port]
	if len(bindings) == 0 {
		return ""
	}

	return bindings[0].HostPort
}

// GetBoundIP returns the host IP bound to the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *Resource) GetBoundIP(portID string) string {
	if r.Container.NetworkSettings == nil {
		return ""
	}

	port, err := network.ParsePort(portID)
	if err != nil {
		return ""
	}
	bindings := r.Container.NetworkSettings.Ports[port]
	if len(bindings) == 0 {
		return ""
	}

	return bindings[0].HostIP.String()
}

// GetHostPort returns the host:port combination for the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *Resource) GetHostPort(portID string) string {
	ip := r.GetBoundIP(portID)
	port := r.GetPort(portID)

	if ip == "" || port == "" {
		return ""
	}

	return fmt.Sprintf("%s:%s", ip, port)
}

// Close stops and removes the container.
// Anonymous volumes created by the container are also removed.
func (r *Resource) Close(ctx context.Context) error {
	if r.pool == nil || r.pool.client == nil {
		return nil
	}

	// Stop container (ignore errors if already stopped)
	_, _ = r.pool.client.ContainerStop(ctx, r.Container.ID, mobyclient.ContainerStopOptions{}) //nolint:errcheck // Best effort stop

	// Remove container
	_, err := r.pool.client.ContainerRemove(ctx, r.Container.ID, mobyclient.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
	return err
}

// CloseT stops and removes the container and calls t.Fatalf on error.
func (r *Resource) CloseT(t TestingTB) {
	t.Helper()
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("CloseT failed: %v", err)
	}
}

// Cleanup registers container cleanup with t.Cleanup.
// The container will be removed when the test finishes.
func (r *Resource) Cleanup(t TestingTB) {
	t.Helper()
	t.Cleanup(func() {
		_ = r.Close(context.Background()) //nolint:errcheck // Best effort cleanup in test cleanup
	})
}
