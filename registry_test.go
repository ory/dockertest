// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"sync"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestRegistry(t *testing.T) {
	// Reset registry before test
	ResetRegistry()

	t.Run("Register and Get", func(t *testing.T) {
		ResetRegistry()

		r1 := &Resource{Container: container.InspectResponse{ID: "container-1"}}

		// Register a resource
		err := Register("reuse-1", r1)
		if err != nil {
			t.Fatalf("Register() error = %v, want nil", err)
		}

		// Get the resource
		got, ok := Get("reuse-1")
		if !ok {
			t.Fatal("Get() ok = false, want true")
		}
		if got.Container.ID != r1.Container.ID {
			t.Errorf("Get() ID = %v, want %v", got.Container.ID, r1.Container.ID)
		}
	})

	t.Run("Get non-existent resource", func(t *testing.T) {
		ResetRegistry()

		_, ok := Get("non-existent")
		if ok {
			t.Error("Get() ok = true, want false for non-existent resource")
		}
	})

	t.Run("Register duplicate reuseID", func(t *testing.T) {
		ResetRegistry()

		r1 := &Resource{Container: container.InspectResponse{ID: "container-1"}}
		r2 := &Resource{Container: container.InspectResponse{ID: "container-2"}}

		// Register first resource
		err := Register("reuse-1", r1)
		if err != nil {
			t.Fatalf("Register() first call error = %v, want nil", err)
		}

		// Attempt to register second resource with same reuseID
		err = Register("reuse-1", r2)
		if err != nil {
			t.Fatalf("Register() second call error = %v, want nil", err)
		}

		// Should return the first registered resource
		got, ok := Get("reuse-1")
		if !ok {
			t.Fatal("Get() ok = false, want true")
		}
		if got.ID() != r1.ID() {
			t.Errorf("Get() ID = %v, want %v (first registered)", got.ID(), r1.ID())
		}
	})

	t.Run("GetAll returns all resources", func(t *testing.T) {
		ResetRegistry()

		r1 := &Resource{Container: container.InspectResponse{ID: "container-1"}}
		r2 := &Resource{Container: container.InspectResponse{ID: "container-2"}}
		r3 := &Resource{Container: container.InspectResponse{ID: "container-3"}}

		Register("reuse-1", r1)
		Register("reuse-2", r2)
		Register("reuse-3", r3)

		all := GetAll()
		if len(all) != 3 {
			t.Errorf("GetAll() length = %v, want 3", len(all))
		}

		// Verify all resources are present
		ids := make(map[string]bool)
		for _, r := range all {
			ids[r.Container.ID] = true
		}

		if !ids["container-1"] || !ids["container-2"] || !ids["container-3"] {
			t.Error("GetAll() missing expected resources")
		}
	})

	t.Run("ResetRegistry clears all resources", func(t *testing.T) {
		ResetRegistry()

		r1 := &Resource{Container: container.InspectResponse{ID: "container-1"}}
		Register("reuse-1", r1)

		// Verify resource exists
		_, ok := Get("reuse-1")
		if !ok {
			t.Fatal("Get() ok = false before reset, want true")
		}

		// Reset registry
		ResetRegistry()

		// Verify resource is gone
		_, ok = Get("reuse-1")
		if ok {
			t.Error("Get() ok = true after reset, want false")
		}

		all := GetAll()
		if len(all) != 0 {
			t.Errorf("GetAll() length = %v after reset, want 0", len(all))
		}
	})
}

func TestRegistryConcurrency(t *testing.T) {
	ResetRegistry()

	const numGoroutines = 100
	reuseID := "concurrent-resource"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch 100 goroutines trying to register the same reuseID
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			r := &Resource{Container: container.InspectResponse{ID: "container-" + string(rune(id))}}
			Register(reuseID, r)
		}(i)
	}

	wg.Wait()

	// Verify exactly one resource was registered
	got, ok := Get(reuseID)
	if !ok {
		t.Fatal("Get() ok = false, want true after concurrent registration")
	}
	if got == nil {
		t.Fatal("Get() returned nil resource")
	}

	// Verify GetAll returns exactly 1 resource
	all := GetAll()
	if len(all) != 1 {
		t.Errorf("GetAll() length = %v, want 1 (only first registration should succeed)", len(all))
	}
}

func TestRegistryScopes(t *testing.T) {
	ResetRegistry()

	r1 := &Resource{Container: container.InspectResponse{ID: "scope-a-container"}}
	r2 := &Resource{Container: container.InspectResponse{ID: "scope-b-container"}}

	_, loadedA := registerWithScope("scope-a", "same-reuse-id", r1)
	if loadedA {
		t.Fatal("registerWithScope(scope-a) loaded = true, want false")
	}
	_, loadedB := registerWithScope("scope-b", "same-reuse-id", r2)
	if loadedB {
		t.Fatal("registerWithScope(scope-b) loaded = true, want false")
	}

	gotA, okA := getWithScope("scope-a", "same-reuse-id")
	if !okA || gotA.ID() != "scope-a-container" {
		t.Fatalf("scope-a lookup failed: ok=%v id=%v", okA, gotA)
	}

	gotB, okB := getWithScope("scope-b", "same-reuse-id")
	if !okB || gotB.ID() != "scope-b-container" {
		t.Fatalf("scope-b lookup failed: ok=%v id=%v", okB, gotB)
	}

	resetRegistryWithScope("scope-a")
	if _, ok := getWithScope("scope-a", "same-reuse-id"); ok {
		t.Fatal("scope-a entry still present after resetRegistryWithScope")
	}
	if _, ok := getWithScope("scope-b", "same-reuse-id"); !ok {
		t.Fatal("scope-b entry unexpectedly removed by scope-a reset")
	}
}

func TestRegisterWithScopeLoadOrStore(t *testing.T) {
	ResetRegistry()

	first := &Resource{Container: container.InspectResponse{ID: "first"}}
	second := &Resource{Container: container.InspectResponse{ID: "second"}}

	stored, loaded := registerWithScope("scope", "reuse", first)
	if loaded {
		t.Fatal("first registerWithScope call loaded = true, want false")
	}
	if stored.ID() != first.ID() {
		t.Fatalf("first stored resource = %q, want %q", stored.ID(), first.ID())
	}

	stored, loaded = registerWithScope("scope", "reuse", second)
	if !loaded {
		t.Fatal("second registerWithScope call loaded = false, want true")
	}
	if stored.ID() != first.ID() {
		t.Fatalf("second stored resource = %q, want %q", stored.ID(), first.ID())
	}
}

func TestRegistryRefCounting(t *testing.T) {
	ResetRegistry()

	r := &Resource{Container: container.InspectResponse{ID: "refcount-container"}}

	// Register: refs=1
	_, loaded := registerWithScope("scope", "rc-id", r)
	if loaded {
		t.Fatal("first register loaded = true, want false")
	}

	// Acquire: refs=2
	got, ok := acquireWithScope("scope", "rc-id")
	if !ok {
		t.Fatal("acquireWithScope returned false, want true")
	}
	if got.ID() != r.ID() {
		t.Fatalf("acquireWithScope returned %q, want %q", got.ID(), r.ID())
	}

	// Release once: refs=1, should NOT be last
	if releaseWithScope("scope", "rc-id") {
		t.Fatal("first releaseWithScope returned true (last ref), want false")
	}
	// Entry should still exist
	if _, ok := getWithScope("scope", "rc-id"); !ok {
		t.Fatal("entry removed after first release, want it to remain")
	}

	// Release again: refs=0, should be last
	if !releaseWithScope("scope", "rc-id") {
		t.Fatal("second releaseWithScope returned false, want true (last ref)")
	}
	// Entry should be gone
	if _, ok := getWithScope("scope", "rc-id"); ok {
		t.Fatal("entry still present after last release, want it removed")
	}
}

func TestRegistryRefCountingRegisterIncrementsOnDuplicate(t *testing.T) {
	ResetRegistry()

	r1 := &Resource{Container: container.InspectResponse{ID: "dup-1"}}
	r2 := &Resource{Container: container.InspectResponse{ID: "dup-2"}}

	// Register r1: refs=1
	registerWithScope("scope", "dup-id", r1)
	// Register r2 with same key: refs=2 (r1 is canonical)
	stored, loaded := registerWithScope("scope", "dup-id", r2)
	if !loaded {
		t.Fatal("second register loaded = false, want true")
	}
	if stored.ID() != r1.ID() {
		t.Fatalf("canonical resource = %q, want %q", stored.ID(), r1.ID())
	}

	// Need 2 releases to remove
	if releaseWithScope("scope", "dup-id") {
		t.Fatal("first release was last, want false")
	}
	if !releaseWithScope("scope", "dup-id") {
		t.Fatal("second release was not last, want true")
	}
}

func TestAcquireWithScopeNonExistent(t *testing.T) {
	ResetRegistry()

	_, ok := acquireWithScope("scope", "nonexistent")
	if ok {
		t.Fatal("acquireWithScope returned true for nonexistent entry, want false")
	}
}

func TestReleaseWithScopeNonExistent(t *testing.T) {
	ResetRegistry()

	// Releasing a nonexistent entry should return true (treat as last reference)
	if !releaseWithScope("scope", "nonexistent") {
		t.Fatal("releaseWithScope returned false for nonexistent entry, want true")
	}
}

func TestGetWithScopeDoesNotIncrementRefs(t *testing.T) {
	ResetRegistry()

	r := &Resource{Container: container.InspectResponse{ID: "get-no-inc"}}

	// Register: refs=1
	registerWithScope("s", "id", r)

	// getWithScope five times — should NOT increment refs
	for range 5 {
		got, ok := getWithScope("s", "id")
		if !ok {
			t.Fatal("getWithScope returned false, want true")
		}
		if got.ID() != r.ID() {
			t.Fatalf("getWithScope returned %q, want %q", got.ID(), r.ID())
		}
	}

	// Single release should be the last ref (still 1)
	if !releaseWithScope("s", "id") {
		t.Fatal("releaseWithScope returned false, want true (last ref)")
	}

	// Entry should be gone
	if _, ok := getWithScope("s", "id"); ok {
		t.Fatal("entry still present after last release, want it removed")
	}
}
