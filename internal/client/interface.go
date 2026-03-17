// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package client provides Docker client abstraction for testability.
package client

import (
	"context"
	"io"

	mobyclient "github.com/moby/moby/client"
)

// DockerClient defines the Docker operations needed by dockertest.
// This interface abstracts github.com/moby/moby/client for testability.
type DockerClient interface {
	ContainerCreate(ctx context.Context, options mobyclient.ContainerCreateOptions) (mobyclient.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string, options mobyclient.ContainerStartOptions) (mobyclient.ContainerStartResult, error)
	ContainerStop(ctx context.Context, containerID string, options mobyclient.ContainerStopOptions) (mobyclient.ContainerStopResult, error)
	ContainerInspect(ctx context.Context, containerID string, options mobyclient.ContainerInspectOptions) (mobyclient.ContainerInspectResult, error)
	ContainerRemove(ctx context.Context, containerID string, options mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error)
	ContainerList(ctx context.Context, options mobyclient.ContainerListOptions) (mobyclient.ContainerListResult, error)
	ContainerLogs(ctx context.Context, containerID string, options mobyclient.ContainerLogsOptions) (mobyclient.ContainerLogsResult, error)

	ExecCreate(ctx context.Context, containerID string, options mobyclient.ExecCreateOptions) (mobyclient.ExecCreateResult, error)
	ExecStart(ctx context.Context, execID string, options mobyclient.ExecStartOptions) (mobyclient.ExecStartResult, error)
	ExecInspect(ctx context.Context, execID string, options mobyclient.ExecInspectOptions) (mobyclient.ExecInspectResult, error)
	ExecAttach(ctx context.Context, execID string, options mobyclient.ExecAttachOptions) (mobyclient.ExecAttachResult, error)

	ImagePull(ctx context.Context, refStr string, options mobyclient.ImagePullOptions) (mobyclient.ImagePullResponse, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...mobyclient.ImageInspectOption) (mobyclient.ImageInspectResult, error)
	ImageBuild(ctx context.Context, buildContext io.Reader, options mobyclient.ImageBuildOptions) (mobyclient.ImageBuildResult, error)
	ImageRemove(ctx context.Context, imageID string, options mobyclient.ImageRemoveOptions) (mobyclient.ImageRemoveResult, error)

	NetworkCreate(ctx context.Context, name string, options mobyclient.NetworkCreateOptions) (mobyclient.NetworkCreateResult, error)
	NetworkInspect(ctx context.Context, networkID string, options mobyclient.NetworkInspectOptions) (mobyclient.NetworkInspectResult, error)
	NetworkConnect(ctx context.Context, networkID string, options mobyclient.NetworkConnectOptions) (mobyclient.NetworkConnectResult, error)
	NetworkDisconnect(ctx context.Context, networkID string, options mobyclient.NetworkDisconnectOptions) (mobyclient.NetworkDisconnectResult, error)
	NetworkRemove(ctx context.Context, networkID string, options mobyclient.NetworkRemoveOptions) (mobyclient.NetworkRemoveResult, error)
	NetworkList(ctx context.Context, options mobyclient.NetworkListOptions) (mobyclient.NetworkListResult, error)

	Ping(ctx context.Context, options mobyclient.PingOptions) (mobyclient.PingResult, error)
	DaemonHost() string
	Close() error
}
