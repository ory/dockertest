// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"archive/tar"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/jsonstream"
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
func (p *pool) BuildAndRun(ctx context.Context, name string, buildOpts *BuildOptions, runOpts ...RunOption) (ClosableResource, error) {
	if buildOpts == nil {
		return nil, fmt.Errorf("buildOpts cannot be nil")
	}

	if buildOpts.ContextDir == "" {
		return nil, fmt.Errorf("buildOpts.ContextDir cannot be empty")
	}

	dockerfile := cmp.Or(buildOpts.Dockerfile, "Dockerfile")

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
		Remove:      true,
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

	// Consume build response and check for errors in the JSON stream.
	// Docker embeds build errors (failed RUN, syntax errors) as {"errorDetail":...}
	// messages rather than returning them from ImageBuild directly.
	if buildErr := drainBuildStream(buildResult.Body); buildErr != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		for _, tag := range tags {
			_, _ = p.client.ImageRemove(cleanupCtx, tag, mobyclient.ImageRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
		}
		return nil, fmt.Errorf("image build failed: %w", buildErr)
	}

	// Run the built image.
	repository, tag, err := splitImageReference(tags[0])
	if err != nil {
		return nil, fmt.Errorf("invalid image reference %q: %w", tags[0], err)
	}

	// Add noPull option since we just built the image locally.
	noPullOpt := RunOption(func(rc *runConfig) error {
		rc.noPull = true
		return nil
	})
	allOpts := make([]RunOption, 0, len(runOpts)+2)
	allOpts = append(allOpts, runOpts...)
	allOpts = append(allOpts, noPullOpt, WithTag(tag))

	resource, err := p.Run(ctx, repository, allOpts...)
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
// The returned ManagedResource does not expose Close, CloseT, or Cleanup;
// the resource is automatically cleaned up when the test finishes.
func (p *pool) BuildAndRunT(t TestingTB, name string, buildOpts *BuildOptions, runOpts ...RunOption) Resource {
	t.Helper()

	r, err := p.BuildAndRun(t.Context(), name, buildOpts, runOpts...)
	if err != nil {
		t.Fatalf("BuildAndRunT failed: %v", err)
	}

	r.Cleanup(t)

	return r
}

func splitImageReference(ref string) (repository, tag string, err error) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return "", "", err
	}

	repository = reference.FamiliarName(named)
	tag = "latest"
	if tagged, ok := named.(reference.Tagged); ok {
		tag = tagged.Tag()
	}

	return repository, tag, nil
}

// createBuildContext creates a tar archive of the given directory for Docker build context.
func createBuildContext(contextDir string) (io.ReadCloser, error) {
	info, err := os.Stat(contextDir)
	if err != nil {
		return nil, fmt.Errorf("build context directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("build context path %q is not a directory", contextDir)
	}

	// Create a pipe for streaming the tar archive
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)

		// Walk the context directory and add files to tar
		err := filepath.WalkDir(contextDir, func(path string, d os.DirEntry, err error) error {
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

			info, err := d.Info()
			if err != nil {
				return err
			}

			// Resolve symlink target for tar header
			var link string
			if info.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}

			// Skip non-regular files (devices, sockets, named pipes)
			if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return nil
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}

			// Use forward slashes in tar (Docker expects this)
			header.Name = filepath.ToSlash(relPath)

			// Write header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Write file content for regular files only
			if info.Mode().IsRegular() {
				// #nosec G304 -- path is from filepath.WalkDir of a known build context directory
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

		// Close tar writer to flush end-of-archive marker
		if closeErr := tw.Close(); closeErr != nil && err == nil {
			err = closeErr
		}

		// Always signal the pipe reader: nil for EOF, non-nil for error
		pw.CloseWithError(err)
	}()

	return pr, nil
}

// drainBuildStream consumes the Docker build JSON stream and returns the first
// error found. Docker embeds build errors (failed RUN commands, syntax errors)
// as {"errorDetail":...} messages in the stream rather than returning them from
// ImageBuild directly.
func drainBuildStream(r io.Reader) error {
	dec := json.NewDecoder(r)
	for dec.More() {
		var msg jsonstream.Message
		if err := dec.Decode(&msg); err != nil {
			return fmt.Errorf("decoding build stream: %w", err)
		}
		if msg.Error != nil {
			return msg.Error
		}
	}
	return nil
}
