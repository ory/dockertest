// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"sync"

	"github.com/moby/moby/api/types/container"
)

// Resource represents a Docker container managed by dockertest.
// It provides methods for interacting with the container (getting ports, closing, etc.)
// and accessing the underlying Docker container details.
type Resource struct {
	pool      *Pool
	Container container.InspectResponse
}

// ID returns the container ID.
func (r *Resource) ID() string {
	return r.Container.ID
}

// globalRegistry is the package-level container registry using sync.Map for thread-safety.
var globalRegistry sync.Map

// Register stores a resource with the given reuseID in the global registry.
//
// If a resource with the same reuseID already exists, the existing resource is kept
// and no error is returned. This ensures that concurrent registration attempts
// result in exactly one resource being stored. The race winner's resource becomes
// the canonical one in the registry, which is safe because both resources represent
// containers with the same configuration (same image, tag, env, etc.).
//
// The global registry enables container reuse across tests. Containers registered
// here can be retrieved with Get.
//
// Note: This function is called automatically by Pool.Run when container reuse
// is enabled. You typically don't need to call it directly.
func Register(reuseID string, r *Resource) error {
	// LoadOrStore is atomic; race conditions are safe because competing
	// resources have identical configurations.
	globalRegistry.LoadOrStore(reuseID, r)
	return nil
}

// Get retrieves a resource from the global registry by reuseID.
// Returns the resource and true if found, nil and false otherwise.
//
// The global registry stores containers for reuse. By default, containers are
// registered with a reuseID of "repository:tag".
//
// Note: This function is called automatically by Pool.Run when checking for
// existing containers. You typically don't need to call it directly.
func Get(reuseID string) (*Resource, bool) {
	val, ok := globalRegistry.Load(reuseID)
	if !ok {
		return nil, false
	}
	resource, ok := val.(*Resource)
	if !ok {
		return nil, false
	}
	return resource, true
}

// GetAll returns a slice of all resources in the global registry.
// The order of resources in the returned slice is not guaranteed.
//
// This is useful for cleanup operations that need to process all registered containers:
//
//	func cleanupAll(ctx context.Context) {
//		for _, resource := range dockertest.GetAll() {
//			_ = resource.Close(ctx)
//		}
//		dockertest.ResetRegistry()
//	}
func GetAll() []*Resource {
	var resources []*Resource

	globalRegistry.Range(func(_, value any) bool {
		if resource, ok := value.(*Resource); ok {
			resources = append(resources, resource)
		}
		return true
	})

	return resources
}

// ResetRegistry clears all resources from the global registry.
// This does NOT stop or remove the containers themselves.
func ResetRegistry() {
	// Collect all keys first to avoid modifying the map while iterating
	var keys []any
	globalRegistry.Range(func(key, _ any) bool {
		keys = append(keys, key)
		return true
	})

	// Delete all collected keys
	for _, key := range keys {
		globalRegistry.Delete(key)
	}
}
