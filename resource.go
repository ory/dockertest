// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

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

	ip := bindings[0].HostIP.String()
	if ip == "" || ip == "0.0.0.0" || ip == "::" {
		return "localhost"
	}

	return ip
}

// GetHostPort returns the host:port combination for the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *Resource) GetHostPort(portID string) string {
	ip := r.GetBoundIP(portID)
	port := r.GetPort(portID)

	if ip == "" || port == "" {
		return ""
	}

	return net.JoinHostPort(ip, port)
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
	if err != nil {
		return err
	}

	if r.reuseID != "" {
		unregisterWithScope(r.pool.reuseScope, r.reuseID)
	}
	r.pool.untrackResource(r.Container.ID)

	return nil
}

// CloseT stops and removes the container and calls t.Fatalf on error.
func (r *Resource) CloseT(t TestingTB) {
	t.Helper()
	if err := r.Close(context.WithoutCancel(t.Context())); err != nil {
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

// Logs returns the container logs, demultiplexing stdout and stderr streams.
// Both stdout and stderr are combined in the returned string.
func (r *Resource) Logs(ctx context.Context) (string, error) {
	if r.pool == nil || r.pool.client == nil {
		return "", fmt.Errorf("pool or client is nil")
	}

	reader, err := r.pool.client.ContainerLogs(ctx, r.Container.ID, mobyclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	// Read and demultiplex Docker log format
	var result bytes.Buffer
	header := make([]byte, 8)

	for {
		// Read header: [stream_type (1 byte), padding (3 bytes), size (4 bytes)]
		n, err := io.ReadFull(reader, header)
		if err == io.EOF {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("failed to read log header: %w", err)
		}
		if n == 0 {
			break
		}
		if n < 8 {
			// Partial header at end of stream, ignore
			break
		}

		// Extract size from header (big-endian uint32 at bytes 4-7)
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}

		// Read the log message
		message := make([]byte, size)
		if _, err := io.ReadFull(reader, message); err != nil {
			return "", fmt.Errorf("failed to read log message: %w", err)
		}

		result.Write(message)
	}

	return result.String(), nil
}
