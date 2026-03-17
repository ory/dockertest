// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
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
		p, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		if p == nil {
			t.Fatal("NewPool() pool = nil, want non-nil")
		}
		t.Cleanup(func() {
			p.Close(t.Context())
		})

		rawPool := p.(*pool)
		if rawPool.maxWait != 60*time.Second {
			t.Errorf("pool.maxWait = %v, want %v", rawPool.maxWait, 60*time.Second)
		}
		if rawPool.client == nil {
			t.Error("pool.client = nil, want non-nil")
		}
	})

	t.Run("creates pool with custom MaxWait", func(t *testing.T) {
		ctx := t.Context()
		p, err := NewPool(ctx, "", WithMaxWait(30*time.Second))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			p.Close(t.Context())
		})

		if p.(*pool).maxWait != 30*time.Second {
			t.Errorf("pool.maxWait = %v, want %v", p.(*pool).maxWait, 30*time.Second)
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

		p, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			p.Close(t.Context())
		})

		if p.(*pool).client != customClient {
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
		p, err := NewPool(ctx, "invalid://endpoint", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil (custom client should bypass endpoint)", err)
		}
		t.Cleanup(func() {
			p.Close(t.Context())
		})
	})
}

func TestNewPoolT(t *testing.T) {
	t.Run("creates pool with t.Context and t.Cleanup", func(t *testing.T) {
		p := NewPoolT(t, "")
		if p == nil {
			t.Fatal("NewPoolT() pool = nil, want non-nil")
		}

		// t.Cleanup should be registered, so we can't test it directly
		// but we can verify the pool is functional
		if p.Client() == nil {
			t.Error("pool.Client() = nil, want non-nil")
		}
	})

	t.Run("accepts options", func(t *testing.T) {
		p := NewPoolT(t, "", WithMaxWait(45*time.Second))
		if p.(*pool).maxWait != 45*time.Second {
			t.Errorf("pool.maxWait = %v, want %v", p.(*pool).maxWait, 45*time.Second)
		}
	})
}

func TestPoolClose(t *testing.T) {
	t.Run("closes client", func(t *testing.T) {
		ctx := t.Context()
		p, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		err = p.Close(ctx)
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

		p, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		// Close pool - should not close custom client
		err = p.Close(ctx)
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
		p, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			p.Close(t.Context())
		})

		err = p.(*pool).cleanup(ctx)
		if err != nil {
			t.Errorf("cleanup() error = %v, want nil", err)
		}
	})
}

func TestCheckForExistingIncrementsRefCount(t *testing.T) {
	ResetRegistry()

	res := &resource{container: container.InspectResponse{ID: "ref-count-check"}}
	p := &pool{daemonHost: "test-host"}

	// Pre-register with the pool's scoped key: refs=1
	register(p.registryKey("id"), res)

	// checkForExisting calls acquire with the same scoped key: refs=2
	got := checkForExisting(p, "id")
	if got == nil || got.ID() != res.ID() {
		t.Fatalf("checkForExisting returned %v, want resource %q", got, res.ID())
	}

	key := p.registryKey("id")

	// First release: refs=1, not last
	if release(key) {
		t.Fatal("first release was last, want false")
	}

	// Second release: refs=0, last
	if !release(key) {
		t.Fatal("second release was not last, want true")
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

	p, err := NewPool(t.Context(), "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	res := p.RunT(t, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)
	containerID := res.ID()

	p.CloseT(t)

	// After CloseT, the container should be removed
	newPool, err := NewPool(t.Context(), "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { newPool.Close(t.Context()) })

	_, inspectErr := newPool.(*pool).client.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(inspectErr) {
		t.Fatalf("ContainerInspect() error = %v, want not found", inspectErr)
	}
}

func TestPoolCleanupReleasesReusedContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	ctx := t.Context()
	p, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	// Run first reused container: refs=1
	r1, err := p.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Run second reused container (same repo:tag): refs=2
	r2, err := p.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if r1.ID() != r2.ID() {
		t.Fatalf("expected same container ID, got %s and %s", r1.ID(), r2.ID())
	}

	containerID := r1.ID()

	rawPool := p.(*pool)

	// cleanup() should close both tracked resources, releasing all refs
	if err := rawPool.cleanup(ctx); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}

	// Container should be gone from Docker (both refs released)
	_, err = rawPool.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() error = %v, want not found", err)
	}

	// Registry entry should be cleared
	if _, ok := get(rawPool.registryKey("alpine:latest")); ok {
		t.Fatal("registry entry still present after cleanup, want it removed")
	}

	// Close just the client (cleanup already done)
	if rawPool.ownedClient && rawPool.client != nil {
		rawPool.client.Close()
		rawPool.client = nil
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

	ctx := t.Context()
	p, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	r, err := p.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	containerID := r.ID()

	rawPool := p.(*pool)
	if err := rawPool.cleanup(ctx); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}

	_, err = rawPool.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() error = %v, want not found", err)
	}

	if rawPool.ownedClient && rawPool.client != nil {
		rawPool.client.Close()
		rawPool.client = nil
	}
}

func TestTwoPoolsShareReusedContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	ctx := t.Context()

	poolA, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool(A) error = %v", err)
	}

	poolB, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool(B) error = %v", err)
	}

	// Pool A runs alpine: refs=1
	rA, err := poolA.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("poolA.Run() error = %v", err)
	}

	// Pool B runs alpine: refs=2 (same container)
	rB, err := poolB.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("poolB.Run() error = %v", err)
	}

	if rA.ID() != rB.ID() {
		t.Fatalf("expected same container ID across pools, got %s and %s", rA.ID(), rB.ID())
	}

	containerID := rA.ID()

	// Close pool A: refs=1, container should still be alive
	if err := poolA.Close(ctx); err != nil {
		t.Fatalf("poolA.Close() error = %v", err)
	}

	_, err = poolB.(*pool).client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect() after closing pool A: error = %v, want container still alive", err)
	}

	// Close pool B: refs=0, container should be removed
	if err := poolB.Close(ctx); err != nil {
		t.Fatalf("poolB.Close() error = %v", err)
	}

	// Need a fresh client to inspect since both pools are closed
	dc, err := client.NewMobyClient(ctx)
	if err != nil {
		t.Fatalf("NewMobyClient() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	_, err = dc.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() after closing pool B: error = %v, want not found", err)
	}
}

func TestResourceDoubleClose(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	ctx := t.Context()
	p, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { p.Close(context.WithoutCancel(t.Context())) })

	r, err := p.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// First close should succeed
	if err := r.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	// Second close should not panic or error
	if err := r.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestPoolCloseAfterResourceClose(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ResetRegistry()
	t.Cleanup(func() {
		ResetRegistry()
	})

	ctx := t.Context()
	p, err := NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}

	r, err := p.Run(ctx, "alpine",
		WithTag("latest"),
		WithCmd([]string{"sleep", "300"}),
		WithoutReuse(),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Close resource manually first
	if err := r.Close(ctx); err != nil {
		t.Fatalf("resource.Close() error = %v", err)
	}

	// Then close pool — should not error
	if err := p.Close(ctx); err != nil {
		t.Fatalf("pool.Close() error = %v", err)
	}
}
