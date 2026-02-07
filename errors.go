// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import "errors"

// ErrImagePullFailed is returned when pulling a Docker image fails.
var ErrImagePullFailed = errors.New("image pull failed")

// ErrContainerCreateFailed is returned when container creation fails.
var ErrContainerCreateFailed = errors.New("container creation failed")

// ErrContainerStartFailed is returned when starting a container fails.
var ErrContainerStartFailed = errors.New("container start failed")

// ErrClientClosed is returned when an operation is attempted on a resource
// whose pool or client has already been closed.
var ErrClientClosed = errors.New("client is closed")
