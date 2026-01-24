// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	mobyclient "github.com/moby/moby/client"
)

// BuildOptions configures image building from a Dockerfile.
// Use with Pool.BuildAndRun to build and run custom Docker images.
//
// Only ContextDir is required. All other fields are optional and have sensible defaults.
//
//nolint:govet // field alignment traded for readability
type BuildOptions struct {
	// Dockerfile is the name of the Dockerfile within the ContextDir.
	// Defaults to "Dockerfile" if empty.
	Dockerfile string

	// ContextDir is the directory containing the Dockerfile and build context.
	// This directory will be archived and sent to the Docker daemon.
	// REQUIRED - build will fail if empty.
	ContextDir string

	// Tags are the tags to apply to the built image.
	// If empty, the image name from BuildAndRun will be used.
	Tags []string

	// BuildArgs are build-time variables passed to the Dockerfile.
	// Use pointers to distinguish between empty string and unset.
	// Example: map[string]*string{"VERSION": &versionStr}
	BuildArgs map[string]*string

	// Labels are metadata to apply to the built image.
	// Example: map[string]string{"version": "1.0", "env": "test"}
	Labels map[string]string

	// NoCache disables build cache when set to true.
	// Useful for ensuring a clean build.
	NoCache bool

	// Remove removes intermediate containers after a successful build.
	// Defaults to true.
	Remove bool

	// ForceRemove always removes intermediate containers, even on build failure.
	// Useful for keeping the build environment clean.
	ForceRemove bool
}

// BuildAndRun builds a Docker image from a Dockerfile and runs it as a container.
//
// The name parameter is used as the image tag. buildOpts.ContextDir is required.
// The built image is cleaned up on error, but not on success - it will be reused
// for subsequent runs with the same name, making repeated test runs faster.
//
// Example:
//
//	resource, err := pool.BuildAndRun(ctx, "myapp:test",
//		&dockertest.BuildOptions{
//			ContextDir: "./testdata",
//			Dockerfile: "Dockerfile.test",
//		},
//	)
//	if err != nil {
//		panic(err)
//	}
//	defer resource.Close(ctx)
func (p *Pool) BuildAndRun(ctx context.Context, name string, buildOpts *BuildOptions, runOpts ...RunOption) (*Resource, error) {
	if buildOpts == nil {
		return nil, fmt.Errorf("buildOpts cannot be nil")
	}

	if buildOpts.ContextDir == "" {
		return nil, fmt.Errorf("buildOpts.ContextDir cannot be empty")
	}

	dockerfile := buildOpts.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	// Create tar archive of build context
	buildContext, err := createBuildContext(buildOpts.ContextDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}
	defer func() {
		if closeErr := buildContext.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	// Prepare tags
	tags := buildOpts.Tags
	if len(tags) == 0 {
		tags = []string{name}
	}

	// Build image
	imageBuildOpts := mobyclient.ImageBuildOptions{
		Tags:        tags,
		Dockerfile:  dockerfile,
		BuildArgs:   buildOpts.BuildArgs,
		NoCache:     buildOpts.NoCache,
		Remove:      buildOpts.Remove || !buildOpts.ForceRemove, // default to true
		ForceRemove: buildOpts.ForceRemove,
		Labels:      buildOpts.Labels,
	}

	buildResult, err := p.client.ImageBuild(ctx, buildContext, imageBuildOpts)
	if err != nil {
		return nil, fmt.Errorf("image build failed: %w", err)
	}
	defer func() {
		_ = buildResult.Body.Close() //nolint:errcheck // Best effort close in defer
	}()

	// Drain build response to complete the build
	if _, drainErr := io.Copy(io.Discard, buildResult.Body); drainErr != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		for _, tag := range tags {
			_, _ = p.client.ImageRemove(cleanupCtx, tag, mobyclient.ImageRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		}
		return nil, fmt.Errorf("failed to drain build response: %w", drainErr)
	}

	// Run the built image
	// Use the first tag as the repository
	// Add noPull option since we just built the image locally
	noPullOpt := RunOption(func(rc *runConfig) error {
		rc.noPull = true
		return nil
	})
	allOpts := append([]RunOption{noPullOpt}, runOpts...)

	resource, err := p.Run(ctx, tags[0], allOpts...)
	if err != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		for _, tag := range tags {
			_, _ = p.client.ImageRemove(cleanupCtx, tag, mobyclient.ImageRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		}
		return nil, err
	}

	return resource, nil
}

// BuildAndRunT is a test helper that uses t.Context() and calls t.Fatalf on error.
func (p *Pool) BuildAndRunT(t TestingTB, name string, buildOpts *BuildOptions, runOpts ...RunOption) *Resource {
	t.Helper()

	r, err := p.BuildAndRun(t.Context(), name, buildOpts, runOpts...)
	if err != nil {
		t.Fatalf("BuildAndRunT failed: %v", err)
	}

	return r
}

// createBuildContext creates a tar archive of the given directory for Docker build context.
//
//nolint:unparam // error is always nil by design; errors communicated via pipe
func createBuildContext(contextDir string) (io.ReadCloser, error) {
	// Create a pipe for streaming the tar archive
	pr, pw := io.Pipe()

	go func() {
		defer func() {
			_ = pw.Close() //nolint:errcheck // Error handled via CloseWithError below
		}()

		tw := tar.NewWriter(pw)
		defer func() {
			_ = tw.Close() //nolint:errcheck // Error handled via CloseWithError below
		}()

		// Walk the context directory and add files to tar
		err := filepath.Walk(contextDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Get relative path from context directory
			relPath, err := filepath.Rel(contextDir, path)
			if err != nil {
				return err
			}

			// Skip the context directory itself
			if relPath == "." {
				return nil
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			// Use forward slashes in tar (Docker expects this)
			header.Name = filepath.ToSlash(relPath)

			// Write header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Write file content for regular files
			if !info.IsDir() {
				// #nosec G304 -- path is from filepath.Walk of a known build context directory
				file, err := os.Open(path)
				if err != nil {
					return err
				}

				if _, err := io.Copy(tw, file); err != nil {
					_ = file.Close() //nolint:errcheck // Prioritize returning the copy error
					return err
				}
				if err := file.Close(); err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	return pr, nil
}
