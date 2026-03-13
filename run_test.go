// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithUser("nobody"),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithWorkingDir("/tmp"),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	if resource.Container.Config.WorkingDir != "/tmp" {
		t.Errorf("expected working dir '/tmp', got %q", resource.Container.Config.WorkingDir)
	}
}

func TestRunWithLabelsAndHostname(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		resource.CloseT(t)
	})

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
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	if resource.Container.Config.StopTimeout == nil || *resource.Container.Config.StopTimeout != 5 {
		t.Errorf("expected stop timeout 5, got %v", resource.Container.Config.StopTimeout)
	}

	if resource.Container.Config.StopSignal != "SIGTERM" {
		t.Errorf("expected stop signal 'SIGTERM', got %q", resource.Container.Config.StopSignal)
	}
}

func TestRunWithHostConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyOnFailure, MaximumRetryCount: 3}
		}),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	if resource.Container.HostConfig.RestartPolicy.Name != container.RestartPolicyOnFailure {
		t.Errorf("expected restart policy %q, got %q", container.RestartPolicyOnFailure, resource.Container.HostConfig.RestartPolicy.Name)
	}
	if resource.Container.HostConfig.RestartPolicy.MaximumRetryCount != 3 {
		t.Errorf("expected max retry count 3, got %d", resource.Container.HostConfig.RestartPolicy.MaximumRetryCount)
	}
}

func TestResourceExec(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	result, err := resource.Exec(t.Context(), []string{"echo", "hello world"})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("Exec() exit code = %d, want 0", result.ExitCode)
	}
	if result.StdOut != "hello world\n" {
		t.Errorf("Exec() stdout = %q, want %q", result.StdOut, "hello world\n")
	}
	if result.StdErr != "" {
		t.Errorf("Exec() stderr = %q, want empty", result.StdErr)
	}
}

func TestResourceExecNonZeroExit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	result, err := resource.Exec(t.Context(), []string{"sh", "-c", "echo err >&2; exit 1"})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	if result.ExitCode != 1 {
		t.Errorf("Exec() exit code = %d, want 1", result.ExitCode)
	}
	if result.StdErr != "err\n" {
		t.Errorf("Exec() stderr = %q, want %q", result.StdErr, "err\n")
	}
}

func TestRunWithName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	containerName := "dockertest-test-named-" + t.Name()
	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithName(containerName),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	if resource.Container.Name != "/"+containerName && resource.Container.Name != containerName {
		t.Errorf("expected container name containing %q, got %q", containerName, resource.Container.Name)
	}
}

func TestRunWithPortBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	port, err := network.ParsePort("80/tcp")
	if err != nil {
		t.Fatalf("ParsePort() error = %v", err)
	}

	bindings := network.PortMap{
		port: []network.PortBinding{
			{HostPort: "18080"},
		},
	}

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithPortBindings(bindings),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	if len(resource.Container.HostConfig.PortBindings[port]) == 0 {
		t.Fatalf("expected port binding for %s, got none", port)
	}
	if resource.Container.HostConfig.PortBindings[port][0].HostPort != "18080" {
		t.Errorf("expected host port 18080, got %q", resource.Container.HostConfig.PortBindings[port][0].HostPort)
	}
}

func TestResourceCloseRefCounting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	// First run creates the container: refs=1
	r1 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)

	// Second run reuses the same container: refs=2
	r2 := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"sleep", "300"}),
	)

	if r1.ID() != r2.ID() {
		t.Fatalf("expected same container ID for reused container, got %s and %s", r1.ID(), r2.ID())
	}

	containerID := r1.ID()

	// Create a standalone Docker client for container inspection
	// (pool.client is unexported from this external test package)
	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	// Close first reference: refs=1, container should still exist
	r1.CloseT(t)

	// Container must still be alive (one reference remains)
	resp, err := dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect() after first close: error = %v, want container still alive", err)
	}
	if resp.Container.ID != containerID {
		t.Fatalf("ContainerInspect() returned ID %s, want %s", resp.Container.ID, containerID)
	}

	// Close second reference: refs=0, container should be removed
	r2.CloseT(t)

	_, err = dc.ContainerInspect(t.Context(), containerID, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect() after last close: error = %v, want not found", err)
	}
}

func TestRunWithMounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "alpine",
		dockertest.WithTag("latest"),
		dockertest.WithMounts([]string{"/tmp:/mnt/test:ro"}),
		dockertest.WithCmd([]string{"sleep", "10"}),
		dockertest.WithoutReuse(),
	)
	t.Cleanup(func() {
		resource.CloseT(t)
	})

	found := false
	for _, bind := range resource.Container.HostConfig.Binds {
		if bind == "/tmp:/mnt/test:ro" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bind mount /tmp:/mnt/test:ro in %v", resource.Container.HostConfig.Binds)
	}
}
