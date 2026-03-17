// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/moby/moby/api/types/container"
)

// resource represents a Docker container managed by dockertest.
// It implements both Resource and ClosableResource interfaces.
type resource struct {
	pool      *pool
	container container.InspectResponse
	reuseID   string
}

// Container returns the Docker container inspection response.
func (r *resource) Container() container.InspectResponse {
	return r.container
}

// ID returns the container ID.
func (r *resource) ID() string {
	return r.container.ID
}

// NewResource creates a Resource for testing purposes.
// This is intended for unit tests that need a Resource without a Docker container.
func NewResource(c container.InspectResponse) ClosableResource {
	return &resource{container: c}
}

// registryEntry wraps a resource with an atomic reference count.
// When multiple callers share a reused container, the ref count tracks
// how many are still using it. The container is only removed from Docker
// when the last reference is released.
type registryEntry struct {
	resource *resource
	refs     atomic.Int32
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
func Register(reuseID string, r ClosableResource) error {
	if reuseID == "" {
		return fmt.Errorf("reuseID cannot be empty")
	}
	if r == nil {
		return fmt.Errorf("resource cannot be nil")
	}
	res, ok := r.(*resource)
	if !ok {
		return fmt.Errorf("resource must be created by this package")
	}
	_, _ = register(reuseID, res)
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
func Get(reuseID string) (ClosableResource, bool) {
	r, ok := get(reuseID)
	if !ok {
		return nil, false
	}
	return r, true
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
func GetAll() []ClosableResource {
	internal := getAll()
	result := make([]ClosableResource, len(internal))
	for i, r := range internal {
		result[i] = r
	}
	return result
}

func register(reuseID string, r *resource) (*resource, bool) {
	entry := &registryEntry{resource: r}
	entry.refs.Store(1)
	actual, loaded := globalRegistry.LoadOrStore(reuseID, entry)
	existing, ok := actual.(*registryEntry)
	if !ok {
		return r, loaded
	}
	if loaded {
		existing.refs.Add(1)
		return existing.resource, true
	}
	return existing.resource, false
}

// acquire looks up an existing entry and increments its ref count.
// Returns the resource and true if found, nil and false otherwise.
func acquire(reuseID string) (*resource, bool) {
	val, ok := globalRegistry.Load(reuseID)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		return nil, false
	}
	entry.refs.Add(1)
	// Verify the entry is still in the map (not deleted and replaced between Load and Add).
	if current, ok := globalRegistry.Load(reuseID); !ok || current != val {
		entry.refs.Add(-1)
		return nil, false
	}
	return entry.resource, true
}

// release decrements the reference count for the given reuseID.
// Returns true if this was the last reference (caller should remove the container).
func release(reuseID string) bool {
	val, ok := globalRegistry.Load(reuseID)
	if !ok {
		return true // Not found, treat as last reference
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		globalRegistry.Delete(reuseID)
		return true
	}
	if entry.refs.Add(-1) <= 0 {
		globalRegistry.Delete(reuseID)
		return true
	}
	return false
}

func get(reuseID string) (*resource, bool) {
	val, ok := globalRegistry.Load(reuseID)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		return nil, false
	}
	return entry.resource, true
}

func getAll() []*resource {
	var resources []*resource

	globalRegistry.Range(func(_, value any) bool {
		if entry, ok := value.(*registryEntry); ok {
			resources = append(resources, entry.resource)
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
