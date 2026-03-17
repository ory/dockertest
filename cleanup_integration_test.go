// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"testing"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
)

func TestCleanupAfterSubtests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	var containerIDs []string

	t.Run("postgres-passes", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		r := pool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
			dockertest.WithoutReuse(),
		)
		containerIDs = append(containerIDs, r.ID())
	})

	t.Run("redis-passes", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		r := pool.RunT(t, "alpine",
			dockertest.WithTag("3.19"),
			dockertest.WithCmd([]string{"sleep", "300"}),
			dockertest.WithoutReuse(),
		)
		containerIDs = append(containerIDs, r.ID())
	})

	t.Run("third-container", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		r := pool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
			dockertest.WithoutReuse(),
		)
		containerIDs = append(containerIDs, r.ID())
	})

	for _, id := range containerIDs {
		_, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
		if !errdefs.IsNotFound(err) {
			t.Fatalf("ContainerInspect(%s) after subtests: error = %v, want not found", id, err)
		}
	}
}

func TestCleanupRefCountingTwoRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	var containerID string

	t.Run("two-refs", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		r1 := pool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
		)
		r2 := pool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
		)
		if r1.ID() != r2.ID() {
			t.Fatalf("expected same container ID, got %s and %s", r1.ID(), r2.ID())
		}
		containerID = r1.ID()
	})

	_, err = dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect(%s) after subtest: error = %v, want not found", containerID, err)
	}
}

func TestCleanupSubtestBeforeParent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	var containerID string

	t.Run("parent", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		t.Run("child", func(t *testing.T) {
			r := pool.RunT(t, "alpine",
				dockertest.WithoutReuse(),
				dockertest.WithCmd([]string{"sleep", "300"}),
			)
			containerID = r.ID()
		})

		// After child subtest completes, its cleanup should have already run.
		_, err := dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
		if !errdefs.IsNotFound(err) {
			t.Fatalf("ContainerInspect(%s) after child subtest: error = %v, want not found", containerID, err)
		}
	})
}

func TestCleanupRefCountingParentChild(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	var containerID string

	t.Run("parent-holds-ref", func(t *testing.T) {
		// Parent acquires ref via RunT: refs=1
		parentPool := dockertest.NewPoolT(t, "")
		r1 := parentPool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
		)
		containerID = r1.ID()

		t.Run("child-acquires-second-ref", func(t *testing.T) {
			// Child acquires second ref to same container: refs=2
			childPool := dockertest.NewPoolT(t, "")
			r2 := childPool.RunT(t, "alpine",
				dockertest.WithCmd([]string{"sleep", "300"}),
			)
			if r2.ID() != containerID {
				t.Fatalf("expected same container ID %s, got %s", containerID, r2.ID())
			}
		})

		// After child subtest: child's cleanup released one ref (refs=1).
		// Container should still be alive because parent's ref remains.
		_, err := dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
		if err != nil {
			t.Fatalf("ContainerInspect(%s) after child cleanup: error = %v, want container still alive", containerID, err)
		}
	})

	// After parent subtest: parent's cleanup released last ref (refs=0).
	// Container should now be removed from Docker.
	_, err = dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect(%s) after parent cleanup: error = %v, want not found", containerID, err)
	}
}

func TestNoTestMainNeeded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	var containerID string

	t.Run("run-and-cleanup", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		r := pool.RunT(t, "alpine",
			dockertest.WithCmd([]string{"sleep", "300"}),
			dockertest.WithoutReuse(),
		)
		containerID = r.ID()
	})

	// No TestMain exists in this file — NewPoolT + RunT handles all cleanup
	// via t.Cleanup callbacks without requiring any global teardown.
	_, err = dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect(%s) after subtest: error = %v, want not found (no TestMain needed)", containerID, err)
	}
}
