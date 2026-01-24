// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	dockertest "github.com/ory/dockertest/v4"
)

func TestPoolRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	// Run a simple container
	r := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)

	// Verify container was created
	if r == nil {
		t.Fatal("RunT returned nil resource")
	}
	if r.ID() == "" {
		t.Fatal("Resource has empty container ID")
	}

	// Cleanup
	r.CloseT(t)
}

func TestPoolRunWithReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// First run
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)

	// Second run should reuse
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)

	if r1.ID() != r2.ID() {
		t.Errorf("Reuse failed: got different containers %s != %s", r1.ID(), r2.ID())
	}

	r1.CloseT(t)
}

func TestPoolRunWithoutReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// First run
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithoutReuse(),
	)

	// Second run should create new container
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithoutReuse(),
	)

	if r1.ID() == r2.ID() {
		t.Errorf("WithoutReuse failed: got same container %s", r1.ID())
	}

	r1.CloseT(t)
	r2.CloseT(t)
}

func TestPoolRunWithReuseID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// First run with custom reuse ID
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("my-custom-id"),
	)

	// Second run with same reuse ID should reuse the container
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("my-custom-id"),
	)

	if r1.ID() != r2.ID() {
		t.Errorf("WithReuseID failed to reuse: got different containers %s != %s", r1.ID(), r2.ID())
	}

	r1.CloseT(t)
}

func TestPoolRunWithDifferentReuseIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// First run with reuse ID "id-1"
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("id-1"),
	)

	// Second run with different reuse ID "id-2" should create new container
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("id-2"),
	)

	if r1.ID() == r2.ID() {
		t.Errorf("Different WithReuseID should create different containers, got same: %s", r1.ID())
	}

	r1.CloseT(t)
	r2.CloseT(t)
}

func TestPoolRunWithReuseIDIgnoresTag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// First run with tag "3.19" and custom reuse ID
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("3.19"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("ignore-tag-test"),
	)

	// Second run with different tag "latest" but same reuse ID should reuse
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithReuseID("ignore-tag-test"),
	)

	if r1.ID() != r2.ID() {
		t.Errorf("WithReuseID should override tag-based reuse: got different containers %s != %s", r1.ID(), r2.ID())
	}

	r1.CloseT(t)
}

func TestRunWithUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithUser("nobody"),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	defer resource.CloseT(t)

	// Verify the user was set in container config
	if resource.Container.Config.User != "nobody" {
		t.Errorf("expected user 'nobody', got %q", resource.Container.Config.User)
	}
}

func TestRunWithWorkingDir(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithWorkingDir("/tmp"),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	defer resource.CloseT(t)

	if resource.Container.Config.WorkingDir != "/tmp" {
		t.Errorf("expected working dir '/tmp', got %q", resource.Container.Config.WorkingDir)
	}
}

func TestRunWithLabelsAndHostname(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	labels := map[string]string{
		"test":    "true",
		"service": "db",
	}

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithLabels(labels),
		dockertest.WithHostname("test-host"),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	defer resource.CloseT(t)

	// Verify labels
	for k, v := range labels {
		if resource.Container.Config.Labels[k] != v {
			t.Errorf("expected label %s=%s, got %s", k, v, resource.Container.Config.Labels[k])
		}
	}

	// Verify hostname
	if resource.Container.Config.Hostname != "test-host" {
		t.Errorf("expected hostname 'test-host', got %q", resource.Container.Config.Hostname)
	}
}

func TestRunWithContainerConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	stopTimeout := 5

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithContainerConfig(func(cfg *container.Config) {
			cfg.StopTimeout = &stopTimeout
			cfg.StopSignal = "SIGTERM"
		}),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	defer resource.CloseT(t)

	if resource.Container.Config.StopTimeout == nil || *resource.Container.Config.StopTimeout != 5 {
		t.Errorf("expected stop timeout 5, got %v", resource.Container.Config.StopTimeout)
	}

	if resource.Container.Config.StopSignal != "SIGTERM" {
		t.Errorf("expected stop signal 'SIGTERM', got %q", resource.Container.Config.StopSignal)
	}
}
