// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultExpiry is the default expiry time for reused containers.
	// This prevents resource leaks if Cleanup() is not called.
	DefaultExpiry = 10 * time.Minute
)

// containerRegistry manages container reuse.
type containerRegistry struct {
	mu           sync.RWMutex
	byReuseID    map[string]*Resource // reuseID -> Resource
	allResources map[*Resource]struct{}
}

// Global registry instance
var registry = newContainerRegistry()

func newContainerRegistry() *containerRegistry {
	return &containerRegistry{
		byReuseID:    make(map[string]*Resource),
		allResources: make(map[*Resource]struct{}),
	}
}

// register adds a resource to the registry with a reuse ID.
func (r *containerRegistry) register(reuseID string, resource *Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byReuseID[reuseID] = resource
	r.allResources[resource] = struct{}{}
}

// lookup finds a resource by reuse ID.
func (r *containerRegistry) lookup(reuseID string) *Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byReuseID[reuseID]
}

// unregister removes a resource from the registry.
func (r *containerRegistry) unregister(resource *Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove from allResources map
	delete(r.allResources, resource)

	// Remove from byReuseID map (find and delete)
	for id, res := range r.byReuseID {
		if res == resource {
			delete(r.byReuseID, id)
			break
		}
	}
}

// all returns all registered resources.
func (r *containerRegistry) all() []*Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()

	resources := make([]*Resource, 0, len(r.allResources))
	for res := range r.allResources {
		resources = append(resources, res)
	}
	return resources
}

// clear removes all resources from the registry.
func (r *containerRegistry) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byReuseID = make(map[string]*Resource)
	r.allResources = make(map[*Resource]struct{})
}

// Cleanup removes all containers registered in the global registry.
// This should be called in TestMain with defer:
//
//	func TestMain(m *testing.M) {
//	    defer dockertest.Cleanup()
//	    code := m.Run()
//	    os.Exit(code)
//	}
func Cleanup() error {
	resources := registry.all()

	var lastErr error
	for _, res := range resources {
		if err := res.Close(context.Background()); err != nil {
			lastErr = err
		}
	}

	registry.clear()
	return lastErr
}
