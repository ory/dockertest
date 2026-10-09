// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import "errors"

// ErrImagePullFailed is returned when pulling a Docker image fails.
var ErrImagePullFailed = errors.New("image pull failed")

// ErrImageBuildFailed is returned when building a Docker image fails.
var ErrImageBuildFailed = errors.New("image build failed")

// ErrContainerCreateFailed is returned when container creation fails.
var ErrContainerCreateFailed = errors.New("container creation failed")

// ErrContainerStartFailed is returned when starting a container fails.
var ErrContainerStartFailed = errors.New("container start failed")

// ErrClientClosed is returned when an operation is attempted on a resource
// whose pool or client has already been closed.
var ErrClientClosed = errors.New("client is closed")

// ErrShuttingDown is returned when a new Docker resource is requested after
// Main has started process shutdown.
var ErrShuttingDown = errors.New("dockertest is shutting down")

// ErrInvalidOption is returned for invalid pool, build, or Main options.
var ErrInvalidOption = errors.New("invalid option")
