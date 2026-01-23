// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import "errors"

// Common errors returned by dockertest operations.
// Use errors.Is() to check for these errors in wrapped error chains.
var (
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
