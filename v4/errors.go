// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import "errors"

// Sentinel errors for common dockertest failures.
// Use errors.Is() to check for these errors in wrapped error chains.
var (
	// ErrContainerNotFound indicates a container was not found.
	ErrContainerNotFound = errors.New("container not found")

	// ErrImageNotFound indicates an image was not found.
	ErrImageNotFound = errors.New("image not found")

	// ErrNetworkNotFound indicates a network was not found.
	ErrNetworkNotFound = errors.New("network not found")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")

	// ErrConnectionRefused indicates a connection was refused.
	ErrConnectionRefused = errors.New("connection refused")

	// ErrImagePullFailed indicates an image pull operation failed.
	ErrImagePullFailed = errors.New("image pull failed")

	// ErrContainerCreateFailed indicates container creation failed.
	ErrContainerCreateFailed = errors.New("container create failed")

	// ErrContainerStartFailed indicates container start failed.
	ErrContainerStartFailed = errors.New("container start failed")

	// ErrExecFailed indicates an exec operation failed.
	ErrExecFailed = errors.New("exec failed")
)
