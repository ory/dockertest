// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

func TestNewPool(t *testing.T) {
	t.Run("creates pool with default options", func(t *testing.T) {
		ctx := t.Context()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		if pool == nil {
			t.Fatal("NewPool() pool = nil, want non-nil")
		}
		t.Cleanup(func() {
			pool.Close(t.Context())
		})

		if pool.MaxWait != 60*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 60*time.Second)
		}
		if pool.client == nil {
			t.Error("pool.client = nil, want non-nil")
		}
	})

	t.Run("creates pool with custom MaxWait", func(t *testing.T) {
		ctx := t.Context()
		pool, err := NewPool(ctx, "", WithMaxWait(30*time.Second))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			pool.Close(t.Context())
		})

		if pool.MaxWait != 30*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 30*time.Second)
		}
	})

	t.Run("creates pool with custom client", func(t *testing.T) {
		ctx := t.Context()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			customClient.Close()
		})

		pool, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			pool.Close(t.Context())
		})

		if pool.client != customClient {
			t.Error("pool.client != customClient, want same client")
		}
	})

	t.Run("custom client option prevents client creation", func(t *testing.T) {
		ctx := t.Context()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			customClient.Close()
		})

		// Use invalid endpoint - should not fail because custom client is used
		pool, err := NewPool(ctx, "invalid://endpoint", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil (custom client should bypass endpoint)", err)
		}
		t.Cleanup(func() {
			pool.Close(t.Context())
		})
	})
}

func TestNewPoolT(t *testing.T) {
	t.Run("creates pool with t.Context and t.Cleanup", func(t *testing.T) {
		pool := NewPoolT(t, "")
		if pool == nil {
			t.Fatal("NewPoolT() pool = nil, want non-nil")
		}

		// t.Cleanup should be registered, so we can't test it directly
		// but we can verify the pool is functional
		if pool.client == nil {
			t.Error("pool.client = nil, want non-nil")
		}
	})

	t.Run("accepts options", func(t *testing.T) {
		pool := NewPoolT(t, "", WithMaxWait(45*time.Second))
		if pool.MaxWait != 45*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 45*time.Second)
		}
	})
}

func TestPoolClose(t *testing.T) {
	t.Run("closes client", func(t *testing.T) {
		ctx := t.Context()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		err = pool.Close(ctx)
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("does not close custom client", func(t *testing.T) {
		ctx := t.Context()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			customClient.Close()
		})

		pool, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		// Close pool - should not close custom client
		err = pool.Close(ctx)
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}

		// Verify custom client is still usable by pinging
		_, pingErr := customClient.Ping(ctx, mobyclient.PingOptions{})
		if pingErr != nil {
			t.Error("custom client was closed by pool.Close(), want it to remain open")
		}
	})
}

func TestPoolCleanup(t *testing.T) {
	t.Run("cleanup with empty registry", func(t *testing.T) {
		ResetRegistry()
		ctx := t.Context()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			pool.Close(t.Context())
		})

		err = pool.cleanup(ctx)
		if err != nil {
			t.Errorf("cleanup() error = %v, want nil", err)
		}
	})
}

func TestCheckForExistingUsesPoolScope(t *testing.T) {
	ResetRegistry()

	resource := &Resource{Container: container.InspectResponse{ID: "scoped-container"}}
	_, _ = registerWithScope("scope-a", "reuse-id", resource)

	poolA := &Pool{reuseScope: "scope-a"}
	if got := checkForExisting(poolA, "reuse-id"); got == nil || got.ID() != "scoped-container" {
		t.Fatalf("checkForExisting(scope-a) = %#v, want container scoped-container", got)
	}

	poolB := &Pool{reuseScope: "scope-b"}
	if got := checkForExisting(poolB, "reuse-id"); got != nil {
		t.Fatalf("checkForExisting(scope-b) = %#v, want nil", got)
	}
}

func TestDefaultPoolsShareReuseScope(t *testing.T) {
	ctx := t.Context()
	poolA, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { poolA.Close(t.Context()) })

	poolB, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { poolB.Close(t.Context()) })

	if poolA.reuseScope != poolB.reuseScope {
		t.Fatalf("default pools have different reuse scopes: %q vs %q", poolA.reuseScope, poolB.reuseScope)
	}

	if poolA.reuseScope != defaultRegistryScope {
		t.Fatalf("default pool reuse scope = %q, want %q", poolA.reuseScope, defaultRegistryScope)
	}
}

func TestCustomClientPoolHasIsolatedScope(t *testing.T) {
	ctx := t.Context()
	customClient, err := client.NewMobyClient(ctx)
	if err != nil {
		t.Fatalf("NewMobyClient() error = %v", err)
	}
	t.Cleanup(func() { customClient.Close() })

	poolCustom, err := NewPool(ctx, "", WithMobyClient(customClient))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { poolCustom.Close(t.Context()) })

	poolDefault, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { poolDefault.Close(t.Context()) })

	if poolCustom.reuseScope == poolDefault.reuseScope {
		t.Fatal("custom client pool should have isolated reuse scope from default pool")
	}
}

func TestPoolCloseT(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	pool := NewPoolT(t, "")
	resource := pool.RunT(t, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)
	containerID := resource.ID()

	pool.CloseT(t)

	// After CloseT, the container should be removed
	newPool, err := NewPool(t.Context(), "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { newPool.Close(t.Context()) })

	_, inspectErr := newPool.client.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(inspectErr) {
		t.Fatalf("ContainerInspect() error = %v, want not found", inspectErr)
	}
}

func TestCheckForExistingIncrementsRefCount(t *testing.T) {
	ResetRegistry()

	resource := &Resource{Container: container.InspectResponse{ID: "ref-count-check"}}
	// Register: refs=1
	registerWithScope("scope", "id", resource)

	pool := &Pool{reuseScope: "scope"}
	// checkForExisting calls acquireWithScope: refs=2
	got := checkForExisting(pool, "id")
	if got == nil || got.ID() != resource.ID() {
		t.Fatalf("checkForExisting returned %v, want resource %q", got, resource.ID())
	}

	// First release: refs=1, not last
	if releaseWithScope("scope", "id") {
		t.Fatal("first releaseWithScope was last, want false")
	}

	// Second release: refs=0, last
	if !releaseWithScope("scope", "id") {
		t.Fatal("second releaseWithScope was not last, want true")
	}
}

func TestPoolCleanupForceRemovesReusedContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	pool := NewPoolT(t, "")

	// Run first reused container: refs=1
	r1 := pool.RunT(t, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)

	// Run second reused container (same repo:tag): refs=2
	r2 := pool.RunT(t, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)

	if r1.ID() != r2.ID() {
		t.Fatalf("expected same container ID, got %s and %s", r1.ID(), r2.ID())
	}

	containerID := r1.ID()
	reuseScope := pool.reuseScope

	// cleanup() should force-remove everything regardless of ref count
	if err := pool.cleanup(t.Context()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}

	// Container should be gone from Docker
	_, err := pool.client.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() error = %v, want not found", err)
	}

	// Registry entry should be cleared
	if _, ok := getWithScope(reuseScope, "alpine:latest"); ok {
		t.Fatal("registry entry still present after cleanup, want it removed")
	}
}

func TestPoolCleanupRemovesWithoutReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	pool := NewPoolT(t, "")
	resource := pool.RunT(t, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)

	if err := pool.cleanup(t.Context()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}

	_, err := pool.client.ContainerInspect(t.Context(), resource.ID(), mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() error = %v, want not found", err)
	}
}
