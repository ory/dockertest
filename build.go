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
// Image cleanup requires complete ownership labels. The classic builder can leave
// intermediate images with missing or partial labels; these and builder cache
// may remain even after successful cleanup. Dockertest does not prune parents
// or builder cache, so cleanup does not bound builder disk usage.
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
	//
	// Docker refuses to remove an image that carries more than one tag by its
	// ID, and dockertest never removes tags, which another process may have
	// moved to an unrelated image. Without RetainImage, an image with several
	// tags therefore stays: its removal fails with a conflict that is
	// returned, and retried, by every later cleanup until the extra tags are
	// gone.
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

	// RetainImage keeps the built image after its last container is removed,
	// across test runs, so that Docker can reuse it as a cache. Retained
	// images carry stable ownership labels without a run ID and are never
	// removed automatically, not even by Main's recovery of abandoned runs.
	//
	// Without RetainImage, the owned final image is removed once the last
	// container created from it has been removed, subject to removal conflicts
	// (see Tags). While a build, or a Run creating a
	// container from a tag, is pending on the same daemon, the removal is
	// deferred until the last of them ends; that BuildAndRun or Run performs
	// it, bounded by the pool's cleanup timeout, and reports failures as
	// warnings. Failed removals are retried by Pool.Close and Main. If the
	// built image cannot be inspected, BuildAndRun fails and the image is only
	// removed once an inspection shows that it carries this process's
	// ownership labels; until then, Pool.Close and Main retry.
	RetainImage bool
}

// BuildAndRun builds a Docker image from a Dockerfile and runs it as a container.
//
// The name parameter is used as the image tag. buildOpts.ContextDir is required.
// The container is created from the immutable ID of the built image, so a
// changed build under the same tag never reuses a container of an older build.
// The owned final image is removed once its last container is gone unless
// BuildOptions.RetainImage is set or removal conflicts (see BuildOptions.Tags).
// Intermediate images without complete ownership labels and builder cache may
// remain after cleanup; see BuildOptions for the image-cleanup scope.
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
	// The build holds one image user until the container exists; releasing it
	// afterwards deletes the image if the container could not be created.
	defer func() {
		releaseCtx, cancel := p.rollbackContext(ctx)
		defer cancel()
		if err := owner.releaseImage(releaseCtx, p.client, imageKey{daemon: p.daemonID, id: imageID}); err != nil {
			owner.warn("%v", err)
		}
	}()

	cfg.tag = tag
	cfg.image = imageID
	return p.run(ctx, repository, cfg)
}

// buildImage builds the image, verifies that the image reported by the daemon
// carries this process's ownership labels, and registers it with one user
// (the pending build). Images that are not owned, for example because a
// pre-existing image was returned, are used but never deleted. If the image
// cannot be inspected, a non-retained image is kept as unverified: ending the
// build, and later cleanups, verify its labels before deleting it.
func (p *pool) buildImage(ctx context.Context, tags []string, buildOpts *BuildOptions) (string, error) {
	buildContext, err := createBuildContext(buildOpts.ContextDir)
	if err != nil {
		return "", fmt.Errorf("%w: failed to create build context: %w", ErrImageBuildFailed, err)
	}
	defer func() {
		_ = buildContext.Close() //nolint:errcheck // Best effort close in defer
	}()

	endBuild, err := owner.beginImageAcquisition(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrImageBuildFailed, err)
	}
	defer endBuild()

	labels := owner.withOwnershipLabels(buildOpts.Labels, buildOpts.RetainImage)
	buildResult, err := p.client.ImageBuild(ctx, buildContext, mobyclient.ImageBuildOptions{
		Tags:        tags,
		Dockerfile:  cmp.Or(buildOpts.Dockerfile, "Dockerfile"),
		BuildArgs:   buildOpts.BuildArgs,
		NoCache:     buildOpts.NoCache,
		Remove:      true,
		ForceRemove: buildOpts.ForceRemove,
		Labels:      labels,
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

	id, owned, err := inspectOwnership(ctx, p.client, imageID, labels)
	if err != nil {
		if !buildOpts.RetainImage {
			owner.addUnverifiedImage(imageKey{daemon: p.daemonID, id: imageID}, p.client, labels)
		}
		return "", fmt.Errorf("%w: inspecting built image %s: %w", ErrImageBuildFailed, imageID, err)
	}
	if owned {
		owner.addImageUser(imageKey{daemon: p.daemonID, id: id}, p.client, buildOpts.RetainImage)
	}
	return id, nil
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
