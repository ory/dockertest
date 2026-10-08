// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"fmt"
	"sync"
)

// registryEntry is one physical, reusable container together with the number
// of handles that currently reference it. The container is only removed from
// Docker when the last handle is released.
type registryEntry struct {
	resource *resource
	refs     int
}

// globalRegistry is the package-level container reuse registry. All
// transitions are serialized by its mutex so that a handle can never acquire
// an entry that is concurrently being released and removed.
var globalRegistry = struct { //nolint:govet // field alignment traded for readability
	sync.Mutex
	entries map[string]*registryEntry
}{entries: map[string]*registryEntry{}}

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
// The registry only contains reusable containers that are currently
// referenced; it is not an inventory of everything dockertest created. Use
// Main for process-wide cleanup.
func GetAll() []ClosableResource {
	internal := getAll()
	result := make([]ClosableResource, len(internal))
	for i, r := range internal {
		result[i] = r
	}
	return result
}

// register stores r under reuseID with one reference, or adds a reference to
// the existing entry. It returns the canonical resource and whether an entry
// already existed.
func register(reuseID string, r *resource) (*resource, bool) {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	if existing, ok := globalRegistry.entries[reuseID]; ok {
		existing.refs++
		return existing.resource, true
	}
	globalRegistry.entries[reuseID] = &registryEntry{resource: r, refs: 1}
	return r, false
}

// acquire looks up an existing entry and increments its ref count.
// Returns the resource and true if found, nil and false otherwise.
// Entries are deleted under the same lock when their last reference is
// released, so a container that is being removed can never be acquired.
func acquire(reuseID string) (*resource, bool) {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	entry, ok := globalRegistry.entries[reuseID]
	if !ok {
		return nil, false
	}
	entry.refs++
	return entry.resource, true
}

// release decrements the reference count for the given reuseID.
// Returns true if this was the last reference (caller should remove the container).
func release(reuseID string) bool {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	entry, ok := globalRegistry.entries[reuseID]
	if !ok {
		return true // Not found, treat as last reference
	}
	entry.refs--
	if entry.refs > 0 {
		return false
	}
	delete(globalRegistry.entries, reuseID)
	return true
}

func get(reuseID string) (*resource, bool) {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	entry, ok := globalRegistry.entries[reuseID]
	if !ok {
		return nil, false
	}
	return entry.resource, true
}

func getAll() []*resource {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	resources := make([]*resource, 0, len(globalRegistry.entries))
	for _, entry := range globalRegistry.entries {
		resources = append(resources, entry.resource)
	}
	return resources
}

// ResetRegistry clears all resources from the global registry.
// This does NOT stop or remove the containers themselves.
func ResetRegistry() {
	globalRegistry.Lock()
	defer globalRegistry.Unlock()
	clear(globalRegistry.entries)
}
