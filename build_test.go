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

func writeDockerfile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}
}

func assertImageGone(t *testing.T, dc *mobyclient.Client, ref string) {
	t.Helper()
	_, err := dc.ImageInspect(t.Context(), ref)
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ImageInspect(%s) error = %v, want not found", ref, err)
	}
}

func TestBuildImageRemovedAfterLastContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nCMD [\"sleep\", \"300\"]\n")
	tag := "dockertest-build-removed:test"
	buildOpts := &dockertest.BuildOptions{ContextDir: tmpDir}

	r1, err := pool.BuildAndRun(ctx, tag, buildOpts)
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	r2, err := pool.BuildAndRun(ctx, tag, buildOpts)
	if err != nil {
		t.Fatalf("second BuildAndRun() error = %v", err)
	}
	if r1.ID() != r2.ID() {
		t.Fatalf("unchanged build did not reuse the container: %s vs %s", r1.ID(), r2.ID())
	}
	imageID := r1.Container().Image
	if labels := mustInspectImage(t, dc, imageID).Config.Labels; labels["io.ory.dockertest.managed"] != "true" || labels["io.ory.dockertest.run"] == "" {
		t.Fatalf("built image labels = %v, want ownership labels", labels)
	}

	if err := r1.Close(ctx); err != nil {
		t.Fatalf("r1.Close() error = %v", err)
	}
	mustInspectImage(t, dc, imageID) // still referenced by r2

	if err := r2.Close(ctx); err != nil {
		t.Fatalf("r2.Close() error = %v", err)
	}
	assertImageGone(t, dc, imageID)
	assertImageGone(t, dc, tag)
	mustInspectImage(t, dc, "alpine:latest") // downloaded base image survives
}

func mustInspectImage(t *testing.T, dc *mobyclient.Client, ref string) mobyclient.ImageInspectResult {
	t.Helper()
	img, err := dc.ImageInspect(t.Context(), ref)
	if err != nil {
		t.Fatalf("ImageInspect(%s) error = %v", ref, err)
	}
	return img
}

func TestBuildRetainImageSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nCMD [\"sleep\", \"300\"]\n")
	tag := "dockertest-build-retained:test"
	t.Cleanup(func() {
		_, _ = dc.ImageRemove(context.WithoutCancel(ctx), tag, mobyclient.ImageRemoveOptions{})
	})

	r, err := pool.BuildAndRun(ctx, tag, &dockertest.BuildOptions{ContextDir: tmpDir, RetainImage: true}, dockertest.WithoutReuse())
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	imageID := r.Container().Image
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := pool.Close(ctx); err != nil {
		t.Fatalf("pool.Close() error = %v", err)
	}
	labels := mustInspectImage(t, dc, imageID).Config.Labels
	if labels["io.ory.dockertest.retain"] != "true" || labels["io.ory.dockertest.run"] != "" {
		t.Fatalf("retained image labels = %v", labels)
	}
}

func TestBuildChangedImageUnderSameTag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nENV VERSION=2\nCMD [\"sleep\", \"300\"]\n")
	r2, err := pool.BuildAndRun(ctx, tag, buildOpts)
	if err != nil {
		t.Fatalf("second BuildAndRun() error = %v", err)
	}
	if r1.ID() == r2.ID() {
		t.Fatal("changed build reused the container of the old build")
	}
	if r1.Container().Image == r2.Container().Image {
		t.Fatal("changed build produced the same image ID")
	}

	if err := r1.Close(ctx); err != nil {
		t.Fatalf("r1.Close() error = %v", err)
	}
	assertImageGone(t, dc, r1.Container().Image)
	mustInspectImage(t, dc, r2.Container().Image)

	if err := r2.Close(ctx); err != nil {
		t.Fatalf("r2.Close() error = %v", err)
	}
	assertImageGone(t, dc, r2.Container().Image)
	assertImageGone(t, dc, tag)
}

func TestBuildFailureKeepsPreexistingTag(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

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
	t.Cleanup(func() {
		_, _ = dc.ImageRemove(context.WithoutCancel(ctx), tag, mobyclient.ImageRemoveOptions{})
	})
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

func TestBuildImageWithExtraTagsIsKeptUntilUntagged(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dockertest.ResetRegistry()
	t.Cleanup(dockertest.ResetRegistry)

	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })

	ctx := t.Context()
	tags := []string{"dockertest-build-multitag:a", "dockertest-build-multitag:b"}
	t.Cleanup(func() {
		for _, tag := range tags {
			_, _ = dc.ImageRemove(context.WithoutCancel(ctx), tag, mobyclient.ImageRemoveOptions{})
		}
	})
	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { pool.Close(context.WithoutCancel(ctx)) })

	tmpDir := t.TempDir()
	writeDockerfile(t, tmpDir, "FROM alpine:latest\nCMD [\"sleep\", \"300\"]\n")
	r, err := pool.BuildAndRun(ctx, tags[0], &dockertest.BuildOptions{ContextDir: tmpDir, Tags: tags}, dockertest.WithoutReuse())
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	imageID := r.Container().Image

	// The daemon refuses to remove an image tagged twice by its ID, and
	// dockertest never removes tags, which other processes may have moved.
	if err := r.Close(ctx); !errdefs.IsConflict(err) {
		t.Fatalf("Close() error = %v, want a conflict", err)
	}
	for _, tag := range tags {
		if got := mustInspectImage(t, dc, tag).ID; got != imageID {
			t.Fatalf("tag %s points to %s, want %s", tag, got, imageID)
		}
	}

	// Once the extra tag is gone, the pool's cleanup removes the image.
	if _, err := dc.ImageRemove(ctx, tags[1], mobyclient.ImageRemoveOptions{}); err != nil {
		t.Fatalf("untagging %s: %v", tags[1], err)
	}
	if err := pool.Close(ctx); err != nil {
		t.Fatalf("pool.Close() error = %v", err)
	}
	assertImageGone(t, dc, imageID)
	assertImageGone(t, dc, tags[0])
}
