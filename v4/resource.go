// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"

	"github.com/docker/docker/api/types"
)

// Resource represents a running Docker container.
type Resource struct {
	pool      *Pool
	Container types.ContainerJSON
}

// Close stops and removes the container.
func (r *Resource) Close(ctx context.Context) error {
	// Stub implementation - will be completed in Task 6
	registry.unregister(r)
	return nil
}
