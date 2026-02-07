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

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
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
		return ErrClientClosed
	}

	// Stop container (ignore errors if already stopped)
	_, _ = r.pool.client.ContainerStop(ctx, r.Container.ID, mobyclient.ContainerStopOptions{}) //nolint:errcheck // Best effort stop

	// Remove container (tolerate already-removed containers)
	_, err := r.pool.client.ContainerRemove(ctx, r.Container.ID, mobyclient.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
	if err != nil && !errdefs.IsNotFound(err) {
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
		if err := r.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Logf("Resource.Cleanup: close failed: %v", err)
		}
	})
}

// Logs returns the container logs, demultiplexing stdout and stderr streams.
// Both stdout and stderr are combined in the returned string.
func (r *Resource) Logs(ctx context.Context) (string, error) {
	if r.pool == nil || r.pool.client == nil {
		return "", ErrClientClosed
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
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("failed to read log header: %w", err)
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

		const maxLogMessageSize = 64 * 1024 * 1024 // 64 MiB per message
		const maxTotalLogSize = 256 * 1024 * 1024  // 256 MiB total
		if size > maxLogMessageSize {
			return "", fmt.Errorf("log message size %d exceeds maximum %d", size, maxLogMessageSize)
		}

		if uint64(result.Len())+uint64(size) > maxTotalLogSize {
			return "", fmt.Errorf("total log size exceeds maximum %d bytes", maxTotalLogSize)
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

// ExecResult holds the output of a command executed inside a container.
type ExecResult struct {
	StdOut   string
	StdErr   string
	ExitCode int
}

// Exec runs a command inside the container and returns the result.
func (r *Resource) Exec(ctx context.Context, cmd []string) (ExecResult, error) {
	if r.pool == nil || r.pool.client == nil {
		return ExecResult{}, ErrClientClosed
	}

	createResp, err := r.pool.client.ExecCreate(ctx, r.Container.ID, mobyclient.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec create failed: %w", err)
	}

	attachResp, err := r.pool.client.ExecAttach(ctx, createResp.ID, mobyclient.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec attach failed: %w", err)
	}
	defer attachResp.Conn.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader); err != nil {
		return ExecResult{}, fmt.Errorf("exec read failed: %w", err)
	}

	inspectResp, err := r.pool.client.ExecInspect(ctx, createResp.ID, mobyclient.ExecInspectOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec inspect failed: %w", err)
	}

	return ExecResult{
		StdOut:   stdout.String(),
		StdErr:   stderr.String(),
		ExitCode: inspectResp.ExitCode,
	}, nil
}
