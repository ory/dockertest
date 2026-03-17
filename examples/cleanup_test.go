// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"testing"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"

	"github.com/ory/dockertest/v4"
)

// TestExplicitCleanup demonstrates explicit resource lifecycle management
// using NewPool/Run/Close directly, without any *T helper auto-cleanup.
func TestExplicitCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := t.Context()

	// Standalone Docker client for verification only.
	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("creating docker client: %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	// Create pool explicitly — caller owns Close.
	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}

	// Run a non-reused container (refs not tracked in registry).
	containerA, err := pool.Run(ctx, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithoutReuse(),
	)
	if err != nil {
		t.Fatalf("running containerA: %v", err)
	}
	containerAID := containerA.ID()

	// Run a reused container (refs=1).
	containerB1, err := pool.Run(ctx, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("running containerB1: %v", err)
	}

	// Run the same reused container again (refs=2).
	containerB2, err := pool.Run(ctx, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)
	if err != nil {
		t.Fatalf("running containerB2: %v", err)
	}
	containerBID := containerB1.ID()

	if containerB1.ID() != containerB2.ID() {
		t.Fatalf("expected containerB1 and containerB2 to share the same ID, got %s and %s", containerB1.ID(), containerB2.ID())
	}

	// Close containerA — it has no reuse ref-count, so it is removed immediately.
	if err := containerA.Close(ctx); err != nil {
		t.Fatalf("closing containerA: %v", err)
	}
	_, err = dc.ContainerInspect(ctx, containerAID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("containerA should be removed, got inspect error: %v", err)
	}

	// Close containerB1 (refs drops to 1) — container must still exist.
	if err := containerB1.Close(ctx); err != nil {
		t.Fatalf("closing containerB1: %v", err)
	}
	if _, err := dc.ContainerInspect(ctx, containerBID, mobyclient.ContainerInspectOptions{}); err != nil {
		t.Fatalf("containerB should still exist after closing one ref, got: %v", err)
	}

	// pool.Close releases containerB2's ref (last ref) and closes the client.
	if err := pool.Close(ctx); err != nil {
		t.Fatalf("closing pool: %v", err)
	}
	_, err = dc.ContainerInspect(ctx, containerBID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("containerB should be removed after pool.Close, got inspect error: %v", err)
	}
}
