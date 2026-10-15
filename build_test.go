// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
)

func TestBuildAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "test-build-image")

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
	trackImage(r.Container().Image)

}

func TestBuildAndRunWithBuildArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "test-build-args")

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

	trackImage(r.Container().Image)

	// Verify build arg was applied by checking container env via logs
	var stdout string
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		stdout, _, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(stdout, "TEST_ENV=test-value") {
			return fmt.Errorf("logs do not yet contain expected env")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'TEST_ENV=test-value', got: %s", stdout)
	}
}

func TestBuildAndRunWithRunOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "test-build-env")

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

	trackImage(r.Container().Image)

	// Verify env var took effect via logs
	var stdout string
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		stdout, _, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(stdout, "hello") {
			return fmt.Errorf("logs do not yet contain expected env value")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'hello', got: %s", stdout)
	}
}

func TestBuildAndRunWithBuildContext(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "test-build-context")

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
	trackImage(r.Container().Image)

	// Poll for expected log output
	var stdout string
	err := pool.Retry(t.Context(), 10*time.Second, func() error {
		var logErr error
		stdout, _, logErr = r.Logs(t.Context())
		if logErr != nil {
			return logErr
		}
		if !strings.Contains(stdout, "Hello, World!") {
			return fmt.Errorf("logs do not yet contain 'Hello, World!'")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Expected logs to contain 'Hello, World!', got: %s (error: %v)", stdout, err)
	}

}

func TestBuildAndRunWithTaggedName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "dockertest-build-tagged:v1")

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

	trackImage(r.Container().Image)

	// The caller's tag is applied, but the container is created from the
	// immutable image ID so that a changed build can never reuse it.
	tagged, err := pool.Client().ImageInspect(t.Context(), imageRef)
	if err != nil {
		t.Fatalf("ImageInspect(%s) error = %v", imageRef, err)
	}
	if r.Container().Config.Image != tagged.ID || r.Container().Image != tagged.ID {
		t.Fatalf("container image = %q / %q, want image ID %q", r.Container().Config.Image, r.Container().Image, tagged.ID)
	}
}

func TestBuildAndRunWithBuildKit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "test-build-buildkit")

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

	buildOpts := &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
		Version:    "2", // BuildKit
	}

	r := pool.BuildAndRunT(t, "test-build-buildkit", buildOpts)
	if r == nil {
		t.Fatal("BuildAndRunT returned nil resource")
	}
	trackImage(r.Container().Image)
}

func TestRunUsesLocalImageWithoutPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "example.invalid/dockertest-local-skip-pull:latest")

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

	r := pool.BuildAndRunT(t, repository, &dockertest.BuildOptions{
		Dockerfile: "Dockerfile",
		ContextDir: tmpDir,
	}, dockertest.WithoutReuse())

	trackImage(r.Container().Image)

	pool.RunT(t, repository,
		dockertest.WithTag("latest"),
		dockertest.WithoutReuse(),
	)
}

// cleanupBuildImages runs after the subsequently registered pool cleanup.
// Keep both the test tag and IDs because another build can replace the tag.
func cleanupBuildImages(t *testing.T, tag string) func(string) {
	t.Helper()
	var ids []string
	t.Cleanup(func() {
		dc, err := mobyclient.New(mobyclient.FromEnv)
		if err != nil {
			t.Errorf("image cleanup client: %v", err)
			return
		}
		defer dc.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := dc.ImageRemove(ctx, tag, mobyclient.ImageRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
			t.Errorf("remove test image tag %s: %v", tag, err)
		}
		for _, id := range ids {
			img, err := dc.ImageInspect(ctx, id)
			if errdefs.IsNotFound(err) {
				continue
			}
			if err != nil {
				t.Errorf("inspect test image %s during cleanup: %v", id, err)
				continue
			}
			// A cached build may also be tagged by another caller. Preserve it.
			if len(img.RepoTags) != 0 {
				continue
			}
			if _, err := dc.ImageRemove(ctx, id, mobyclient.ImageRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
				t.Errorf("remove test image %s: %v", id, err)
			}
		}
	})
	return func(id string) { ids = append(ids, id) }
}

func writeDockerfile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}
}

func mustInspectImage(t *testing.T, dc *mobyclient.Client, ref string) mobyclient.ImageInspectResult {
	t.Helper()
	img, err := dc.ImageInspect(t.Context(), ref)
	if err != nil {
		t.Fatalf("ImageInspect(%s) error = %v", ref, err)
	}
	return img
}

func TestBuildChangedImageUnderSameTag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	trackImage := cleanupBuildImages(t, "dockertest-build-changed:test")

	dockertest.ResetRegistry()
	t.Cleanup(dockertest.ResetRegistry)

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	ctx := t.Context()
	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { pool.Close(context.WithoutCancel(ctx)) })

	tmpDir := t.TempDir()
	tag := "dockertest-build-changed:test"
	buildOpts := &dockertest.BuildOptions{ContextDir: tmpDir}

	writeDockerfile(t, tmpDir, "FROM alpine:latest\nENV VERSION=1\nCMD [\"sleep\", \"300\"]\n")
	r1, err := pool.BuildAndRun(ctx, tag, buildOpts)
	if err != nil {
		t.Fatalf("first BuildAndRun() error = %v", err)
	}
	trackImage(r1.Container().Image)
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nENV VERSION=2\nCMD [\"sleep\", \"300\"]\n")
	r2, err := pool.BuildAndRun(ctx, tag, buildOpts)
	if err != nil {
		t.Fatalf("second BuildAndRun() error = %v", err)
	}
	trackImage(r2.Container().Image)
	if r1.ID() == r2.ID() {
		t.Fatal("changed build reused the container of the old build")
	}
	if r1.Container().Image == r2.Container().Image {
		t.Fatal("changed build produced the same image ID")
	}

	if err := r1.Close(ctx); err != nil {
		t.Fatalf("r1.Close() error = %v", err)
	}
	mustInspectImage(t, dc, r1.Container().Image)
	mustInspectImage(t, dc, r2.Container().Image)

	if err := r2.Close(ctx); err != nil {
		t.Fatalf("r2.Close() error = %v", err)
	}
	mustInspectImage(t, dc, r2.Container().Image)
	mustInspectImage(t, dc, tag)
}

func TestBuildFailureKeepsPreexistingTag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanupBuildImages(t, "dockertest-preexisting:test")

	dockertest.ResetRegistry()
	t.Cleanup(dockertest.ResetRegistry)

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	ctx := t.Context()
	pool := dockertest.NewPoolT(t, "")
	pool.RunT(t, "alpine", dockertest.WithCmd([]string{"sleep", "300"})) // ensures alpine:latest exists

	tag := "dockertest-preexisting:test"
	if _, err := dc.ImageTag(ctx, mobyclient.ImageTagOptions{Source: "alpine:latest", Target: tag}); err != nil {
		t.Fatalf("ImageTag() error = %v", err)
	}
	before := mustInspectImage(t, dc, tag).ID

	tmpDir := t.TempDir()
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nRUN false\n")
	_, buildErr := pool.BuildAndRun(ctx, tag, &dockertest.BuildOptions{ContextDir: tmpDir})
	if !errors.Is(buildErr, dockertest.ErrImageBuildFailed) {
		t.Fatalf("BuildAndRun() error = %v, want ErrImageBuildFailed", buildErr)
	}
	if after := mustInspectImage(t, dc, tag).ID; after != before {
		t.Fatalf("pre-existing tag changed from %s to %s", before, after)
	}
}
