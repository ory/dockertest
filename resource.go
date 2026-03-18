// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	mobynetwork "github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

// Resource provides access to a Docker container.
// Returned by *T variants; does not expose Close, CloseT, or Cleanup.
type Resource interface {
	ID() string
	Container() container.InspectResponse
	GetPort(portID string) string
	GetBoundIP(portID string) string
	GetHostPort(portID string) string
	Logs(ctx context.Context) (stdout, stderr string, err error)
	FollowLogs(ctx context.Context, stdout, stderr io.Writer) error
	Exec(ctx context.Context, cmd []string) (ExecResult, error)
	ConnectToNetwork(ctx context.Context, net Network) error
	DisconnectFromNetwork(ctx context.Context, net Network) error
	GetIPInNetwork(net Network) string
}

// ClosableResource extends Resource with explicit lifecycle management.
// Returned by Run and BuildAndRun; the caller is responsible for calling Close.
type ClosableResource interface {
	Resource
	Close(ctx context.Context) error
	CloseT(t TestingTB)
	Cleanup(t TestingTB)
}

// GetPort returns the host port bound to the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *resource) GetPort(portID string) string {
	if r.container.NetworkSettings == nil {
		return ""
	}

	port, err := mobynetwork.ParsePort(portID)
	if err != nil {
		return ""
	}
	bindings := r.container.NetworkSettings.Ports[port]
	if len(bindings) == 0 {
		return ""
	}

	return bindings[0].HostPort
}

// GetBoundIP returns the host IP bound to the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *resource) GetBoundIP(portID string) string {
	if r.container.NetworkSettings == nil {
		return ""
	}

	port, err := mobynetwork.ParsePort(portID)
	if err != nil {
		return ""
	}
	bindings := r.container.NetworkSettings.Ports[port]
	if len(bindings) == 0 {
		return ""
	}

	ip := bindings[0].HostIP.String()
	if ip == "" || ip == "0.0.0.0" || ip == "::" {
		return "127.0.0.1"
	}

	return ip
}

// GetHostPort returns the host:port combination for the given container port.
// The portID parameter should include the protocol (e.g., "5432/tcp").
func (r *resource) GetHostPort(portID string) string {
	ip := r.GetBoundIP(portID)
	port := r.GetPort(portID)

	if ip == "" || port == "" {
		return ""
	}

	return net.JoinHostPort(ip, port)
}

// Close stops and removes the container.
// Anonymous volumes created by the container are also removed.
//
// For reused containers (those with a reuseID), Close only removes the Docker
// container when the last reference is released. If other callers still hold
// references, Close simply untracks the resource from this pool.
func (r *resource) Close(ctx context.Context) error {
	if r.pool == nil || r.pool.client == nil {
		return ErrClientClosed
	}

	if r.reuseID != "" {
		registryKey := r.pool.registryKey(r.reuseID)
		r.reuseID = "" // prevent double-release on repeated Close calls
		if !release(registryKey) {
			// Other callers still hold references; just untrack from this pool.
			r.pool.untrackResource(r.container.ID)
			return nil
		}
	}

	// Stop container (ignore errors if already stopped)
	_, _ = r.pool.client.ContainerStop(ctx, r.container.ID, mobyclient.ContainerStopOptions{}) //nolint:errcheck // Best effort stop

	// Remove container (tolerate already-removed containers)
	_, err := r.pool.client.ContainerRemove(ctx, r.container.ID, mobyclient.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
	if err != nil && !errdefs.IsNotFound(err) {
		return err
	}

	r.pool.untrackResource(r.container.ID)

	return nil
}

// CloseT stops and removes the container and calls t.Fatalf on error.
func (r *resource) CloseT(t TestingTB) {
	t.Helper()
	if err := r.Close(context.WithoutCancel(t.Context())); err != nil {
		t.Fatalf("CloseT failed: %v", err)
	}
}

// Cleanup registers container cleanup with t.Cleanup.
// The container will be removed when the test finishes.
func (r *resource) Cleanup(t TestingTB) {
	t.Helper()
	t.Cleanup(func() {
		if err := r.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Logf("Resource.Cleanup: close failed: %v", err)
		}
	})
}

func (r *resource) containerLogReader(ctx context.Context, follow bool) (io.ReadCloser, error) {
	if r.pool == nil || r.pool.client == nil {
		return nil, ErrClientClosed
	}
	reader, err := r.pool.client.ContainerLogs(ctx, r.container.ID, mobyclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}
	return reader, nil
}

// Logs returns the container logs, demultiplexing stdout and stderr.
func (r *resource) Logs(ctx context.Context) (stdout, stderr string, err error) {
	reader, err := r.containerLogReader(ctx, false)
	if err != nil {
		return "", "", err
	}
	defer reader.Close()

	var outBuf, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&outBuf, &errBuf, reader); err != nil {
		return "", "", fmt.Errorf("failed to read container logs: %w", err)
	}

	return outBuf.String(), errBuf.String(), nil
}

// FollowLogs streams container logs to stdout and stderr until ctx is cancelled
// or the container exits. Pass io.Discard for writers you don't need.
func (r *resource) FollowLogs(ctx context.Context, stdout, stderr io.Writer) error {
	reader, err := r.containerLogReader(ctx, true)
	if err != nil {
		return err
	}
	defer reader.Close()

	if _, err := stdcopy.StdCopy(stdout, stderr, reader); err != nil {
		return fmt.Errorf("failed to follow container logs: %w", err)
	}
	return nil
}

// ExecResult holds the output of a command executed inside a container.
type ExecResult struct {
	StdOut   string
	StdErr   string
	ExitCode int
}

// Exec runs a command inside the container and returns the result.
func (r *resource) Exec(ctx context.Context, cmd []string) (ExecResult, error) {
	if r.pool == nil || r.pool.client == nil {
		return ExecResult{}, ErrClientClosed
	}

	createResp, err := r.pool.client.ExecCreate(ctx, r.container.ID, mobyclient.ExecCreateOptions{
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

// ConnectToNetwork connects the container to the given network.
// The resource's container field is automatically updated with the latest
// network settings after connection.
func (r *resource) ConnectToNetwork(ctx context.Context, net Network) error {
	if r.pool == nil || r.pool.client == nil {
		return ErrClientClosed
	}

	connectOpts := mobyclient.NetworkConnectOptions{
		Container: r.container.ID,
	}

	_, err := r.pool.client.NetworkConnect(ctx, net.ID(), connectOpts)
	if err != nil {
		return err
	}

	// Refresh container inspection to get updated network settings
	inspectResp, err := r.pool.client.ContainerInspect(ctx, r.container.ID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	r.container = inspectResp.Container
	return nil
}

// DisconnectFromNetwork disconnects the container from the given network.
// The resource's container field is automatically updated with the latest
// network settings after disconnection.
func (r *resource) DisconnectFromNetwork(ctx context.Context, net Network) error {
	if r.pool == nil || r.pool.client == nil {
		return ErrClientClosed
	}

	disconnectOpts := mobyclient.NetworkDisconnectOptions{
		Container: r.container.ID,
		Force:     false,
	}

	_, err := r.pool.client.NetworkDisconnect(ctx, net.ID(), disconnectOpts)
	if err != nil {
		return err
	}

	// Refresh container inspection to get updated network settings
	inspectResp, err := r.pool.client.ContainerInspect(ctx, r.container.ID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	r.container = inspectResp.Container
	return nil
}

// GetIPInNetwork returns the container's IP address in the given network.
// Returns empty string if the container is not connected to the network.
func (r *resource) GetIPInNetwork(net Network) string {
	if r.container.NetworkSettings == nil {
		return ""
	}

	if r.container.NetworkSettings.Networks == nil {
		return ""
	}

	endpoint, ok := r.container.NetworkSettings.Networks[net.Inspect().Name]
	if !ok {
		return ""
	}

	return endpoint.IPAddress.String()
}
