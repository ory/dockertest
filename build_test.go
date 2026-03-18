// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockertest "github.com/ory/dockertest/v4"
)

func TestBuildAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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

}

func TestBuildAndRunWithBuildArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	// Create a temporary directory for build context
	tmpDir := t.TempDir()

	// Write a Dockerfile that uses build args and prints the value
	dockerfile := `FROM alpine:latest
ARG TEST_ARG
ENV TEST_ENV=${TEST_ARG}
CMD ["sh", "-c", "echo TEST_ENV=$TEST_ENV && sleep 300"]
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

	// Verify build arg was applied by checking container env via logs
	var logs dockertest.LogResult
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		logs, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(logs.Combined(), "TEST_ENV=test-value") {
			return fmt.Errorf("logs do not yet contain expected env")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'TEST_ENV=test-value', got: %s", logs.Combined())
	}
}

func TestBuildAndRunWithRunOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

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

	// Verify env var took effect via logs
	var logs dockertest.LogResult
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		logs, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(logs.Combined(), "hello") {
			return fmt.Errorf("logs do not yet contain expected env value")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'hello', got: %s", logs.Combined())
	}
}

func TestBuildAndRunWithBuildContext(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")

	// Use test fixture with Go program (main.go, go.mod, Dockerfile)
	contextDir := filepath.Join("testdata", "build_context")

	buildOpts := &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: contextDir,
	}

	r := pool.BuildAndRunT(t, "test-build-context", buildOpts)

	// Verify container was created
	if r == nil {
		t.Fatal("BuildAndRunT returned nil resource")
	}
	if r.ID() == "" {
		t.Fatal("Resource has empty container ID")
	}

	// Poll for expected log output
	var logs dockertest.LogResult
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		logs, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(logs.Combined(), "Hello, World!") {
			return fmt.Errorf("logs do not yet contain 'Hello, World!'")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'Hello, World!', got: %s (error: %v)", logs.Combined(), err)
	}

}

func TestBuildAndRunWithTaggedName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")
	tmpDir := t.TempDir()

	dockerfile := `FROM alpine:latest
CMD ["sleep", "300"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	imageRef := "dockertest-build-tagged:v1"
	r := pool.BuildAndRunT(t, imageRef, &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
	})

	if r.Container().Config.Image != imageRef {
		t.Fatalf("container image = %q, want %q", r.Container().Config.Image, imageRef)
	}
}

func TestRunUsesLocalImageWithoutPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(func() {
		dockertest.ResetRegistry()
	})

	pool := dockertest.NewPoolT(t, "")
	tmpDir := t.TempDir()

	dockerfile := `FROM alpine:latest
CMD ["sleep", "300"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// If Run attempts a pull, this registry will fail DNS resolution.
	repository := "example.invalid/dockertest-local-skip-pull"

	pool.BuildAndRunT(t, repository, &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
	}, dockertest.WithoutReuse())

	pool.RunT(t, repository,
		dockertest.WithTag("latest"),
		dockertest.WithoutReuse(),
	)
}
