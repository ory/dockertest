// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"os"
	"path/filepath"
	"testing"

	dockertest "github.com/ory/dockertest/v4"
)

func TestBuildAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// Create a temporary directory for build context
	tmpDir := t.TempDir()

	// Write a simple Dockerfile
	dockerfile := `FROM alpine:latest
CMD ["sleep", "300"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Build and run
	buildOpts := &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
	}

	r := pool.BuildAndRunT(t, "test-build-image", buildOpts)

	// Verify container was created
	if r == nil {
		t.Fatal("BuildAndRunT returned nil resource")
	}
	if r.ID() == "" {
		t.Fatal("Resource has empty container ID")
	}

	// Cleanup
	r.CloseT(t)
}

func TestBuildAndRunWithBuildArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// Create a temporary directory for build context
	tmpDir := t.TempDir()

	// Write a Dockerfile that uses build args
	dockerfile := `FROM alpine:latest
ARG TEST_ARG
ENV TEST_ENV=${TEST_ARG}
CMD ["sleep", "300"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Build and run with build args
	testValue := "test-value"
	buildOpts := &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
		BuildArgs:  map[string]*string{"TEST_ARG": &testValue},
	}

	r := pool.BuildAndRunT(t, "test-build-args", buildOpts)

	// Verify container was created
	if r == nil {
		t.Fatal("BuildAndRunT returned nil resource")
	}
	if r.ID() == "" {
		t.Fatal("Resource has empty container ID")
	}

	// Cleanup
	r.CloseT(t)
}

func TestBuildAndRunWithRunOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	defer dockertest.ResetRegistry()

	pool := dockertest.NewPoolT(t, "")

	// Create a temporary directory for build context
	tmpDir := t.TempDir()

	// Write a simple Dockerfile
	dockerfile := `FROM alpine:latest
CMD ["sh", "-c", "echo $TEST_VAR && sleep 300"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Build and run with environment variable
	buildOpts := &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
	}

	r := pool.BuildAndRunT(t, "test-build-env", buildOpts,
		dockertest.WithEnv([]string{"TEST_VAR=hello"}),
	)

	// Verify container was created
	if r == nil {
		t.Fatal("BuildAndRunT returned nil resource")
	}
	if r.ID() == "" {
		t.Fatal("Resource has empty container ID")
	}

	// Cleanup
	r.CloseT(t)
}
