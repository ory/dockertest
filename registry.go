// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/moby/moby/api/types/container"
)

// Resource represents a Docker container managed by dockertest.
// It provides methods for interacting with the container (getting ports, closing, etc.)
// and accessing the underlying Docker container details.
type Resource struct {
	pool      *Pool
	Container container.InspectResponse
	reuseID   string
}

// ID returns the container ID.
func (r *Resource) ID() string {
	return r.Container.ID
}

type registryKey struct {
	scope   string
	reuseID string
}

// registryEntry wraps a Resource with an atomic reference count.
// When multiple callers share a reused container, the ref count tracks
// how many are still using it. The container is only removed from Docker
// when the last reference is released.
type registryEntry struct {
	resource *Resource
	refs     atomic.Int32
}

const defaultRegistryScope = "default"

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
	if reuseID == "" {
		return fmt.Errorf("reuseID cannot be empty")
	}
	if r == nil {
		return fmt.Errorf("resource cannot be nil")
	}
	_, _ = registerWithScope(defaultRegistryScope, reuseID, r)
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
	return getWithScope(defaultRegistryScope, reuseID)
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
	return getAllWithScope(defaultRegistryScope)
}

func registerWithScope(scope, reuseID string, r *Resource) (*Resource, bool) {
	key := registryKey{scope: scope, reuseID: reuseID}
	entry := &registryEntry{resource: r}
	entry.refs.Store(1)
	actual, loaded := globalRegistry.LoadOrStore(key, entry)
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

// acquireWithScope looks up an existing entry and increments its ref count.
// Returns the resource and true if found, nil and false otherwise.
func acquireWithScope(scope, reuseID string) (*Resource, bool) {
	key := registryKey{scope: scope, reuseID: reuseID}
	val, ok := globalRegistry.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		return nil, false
	}
	entry.refs.Add(1)
	// Verify the entry is still in the map (not deleted and replaced between Load and Add).
	if current, ok := globalRegistry.Load(key); !ok || current != val {
		entry.refs.Add(-1)
		return nil, false
	}
	return entry.resource, true
}

// releaseWithScope decrements the reference count for the given reuseID.
// Returns true if this was the last reference (caller should remove the container).
func releaseWithScope(scope, reuseID string) bool {
	key := registryKey{scope: scope, reuseID: reuseID}
	val, ok := globalRegistry.Load(key)
	if !ok {
		return true // Not found, treat as last reference
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		globalRegistry.Delete(key)
		return true
	}
	if entry.refs.Add(-1) <= 0 {
		globalRegistry.Delete(key)
		return true
	}
	return false
}

func getWithScope(scope, reuseID string) (*Resource, bool) {
	val, ok := globalRegistry.Load(registryKey{scope: scope, reuseID: reuseID})
	if !ok {
		return nil, false
	}
	entry, ok := val.(*registryEntry)
	if !ok {
		return nil, false
	}
	return entry.resource, true
}

func getAllWithScope(scope string) []*Resource {
	var resources []*Resource

	globalRegistry.Range(func(key, value any) bool {
		regKey, ok := key.(registryKey)
		if !ok || regKey.scope != scope {
			return true
		}
		if entry, ok := value.(*registryEntry); ok {
			resources = append(resources, entry.resource)
		}
		return true
	})

	return resources
}

func resetRegistryWithScope(scope string) {
	var keys []registryKey
	globalRegistry.Range(func(key, _ any) bool {
		regKey, ok := key.(registryKey)
		if ok && regKey.scope == scope {
			keys = append(keys, regKey)
		}
		return true
	})

	for _, key := range keys {
		globalRegistry.Delete(key)
	}
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
