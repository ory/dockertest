// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"archive/tar"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/jsonstream"
	mobyclient "github.com/moby/moby/client"
)

// BuildOptions configures image building from a Dockerfile.
// Use with Pool.BuildAndRun to build and run custom Docker images.
//
// Only ContextDir is required. All other fields are optional and have sensible defaults.
// Built images and builder cache are not automatically removed.
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
	// It does not remove intermediate images or builder cache.
	ForceRemove bool

	// Version specifies the builder to use. "1" for classic, "2" for BuildKit.
	// Defaults to the daemon's configured default if empty.
	Version string
}

// BuildAndRun builds a Docker image from a Dockerfile and runs it as a container.
//
// The name parameter is used as the image tag. buildOpts.ContextDir is required.
// The container is created from the immutable ID of the built image, so a
// changed build under the same tag never reuses a container of an older build.
// Built images remain available after container cleanup.
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
		return nil, fmt.Errorf("%w: buildOpts cannot be nil", ErrInvalidOption)
	}
	if buildOpts.ContextDir == "" {
		return nil, fmt.Errorf("%w: buildOpts.ContextDir cannot be empty", ErrInvalidOption)
	}
	cfg, err := buildRunConfig(runOpts)
	if err != nil {
		return nil, err
	}

	tags := buildOpts.Tags
	if len(tags) == 0 {
		tags = []string{name}
	}
	repository, tag, err := splitImageReference(tags[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid image reference %q: %w", ErrInvalidOption, tags[0], err)
	}

	ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	imageID, err := p.buildImage(ctx, tags, buildOpts)
	if err != nil {
		return nil, err
	}

	cfg.tag = tag
	cfg.image = imageID
	r, err := p.run(ctx, repository, cfg)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// buildImage builds an image and resolves the returned immutable image ID.
func (p *pool) buildImage(ctx context.Context, tags []string, buildOpts *BuildOptions) (string, error) {
	buildContext, err := createBuildContext(buildOpts.ContextDir)
	if err != nil {
		return "", fmt.Errorf("%w: failed to create build context: %w", ErrImageBuildFailed, err)
	}
	defer func() {
		_ = buildContext.Close() //nolint:errcheck // Best effort close in defer
	}()

	buildResult, err := p.client.ImageBuild(ctx, buildContext, mobyclient.ImageBuildOptions{
		Tags:        tags,
		Dockerfile:  cmp.Or(buildOpts.Dockerfile, "Dockerfile"),
		BuildArgs:   buildOpts.BuildArgs,
		NoCache:     buildOpts.NoCache,
		Remove:      true,
		ForceRemove: buildOpts.ForceRemove,
		Labels:      buildOpts.Labels,
		Version:     build.BuilderVersion(buildOpts.Version),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrImageBuildFailed, err)
	}
	defer func() {
		_ = buildResult.Body.Close() //nolint:errcheck // Best effort close in defer
	}()

	imageID, err := readBuildResult(buildResult.Body)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrImageBuildFailed, err)
	}

	inspect, err := p.client.ImageInspect(ctx, imageID)
	if err != nil {
		return "", fmt.Errorf("%w: inspecting built image %s: %w", ErrImageBuildFailed, imageID, err)
	}
	if inspect.ID == "" {
		return "", fmt.Errorf("%w: built image %s has no ID", ErrImageBuildFailed, imageID)
	}
	return inspect.ID, nil
}

// BuildAndRunT is a test helper that uses t.Context() and calls t.Fatalf on error.
// The returned Resource does not expose Close, CloseT, or Cleanup;
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

// readBuildResult consumes the Docker build JSON stream and returns the ID of
// the built image. Docker embeds build errors (failed RUN commands, syntax
// errors) as {"errorDetail":...} messages in the stream rather than returning
// them from ImageBuild directly, and reports the final image as a structured
// {"aux":{"ID":...}} message. The ID is only accepted after the stream has
// ended cleanly, so a truncated stream never yields an image.
func readBuildResult(r io.Reader) (string, error) {
	var imageID string
	dec := json.NewDecoder(r)
	for {
		var msg jsonstream.Message
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("decoding build stream: %w", err)
		}
		if msg.Error != nil {
			return "", msg.Error
		}
		// BuildKit tags its trace messages with an ID; the image result is
		// either untagged (classic builder) or tagged "moby.image.id".
		if msg.Aux == nil || (msg.ID != "" && msg.ID != "moby.image.id") {
			continue
		}
		var result build.Result
		if err := json.Unmarshal(*msg.Aux, &result); err != nil {
			return "", fmt.Errorf("decoding build result: %w", err)
		}
		if result.ID != "" {
			imageID = result.ID
		}
	}
	if imageID == "" {
		return "", errors.New("build stream ended without an image ID")
	}
	return imageID, nil
}
