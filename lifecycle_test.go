// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

func TestRunStartFailureRollsBackWithCanceledContext(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	startErr := errors.New("boom")
	ctx, cancel := context.WithCancel(t.Context())
	// The caller gives up after the container was created.
	c.containerStart = func(context.Context, string) error { cancel(); return startErr }
	p := newFakePool(t, c)

	_, err := p.Run(ctx, "alpine", WithoutReuse())
	if !errors.Is(err, ErrContainerStartFailed) || !errors.Is(err, startErr) {
		t.Fatalf("Run() error = %v, want ErrContainerStartFailed wrapping %v", err, startErr)
	}

	want := []string{"ContainerRemove container-1 force=true volumes=true canceled=false"}
	if got := c.callsMatching("ContainerRemove"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ContainerRemove calls = %q, want %q", got, want)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestRunStartFailureKeepsRecordWhenRemovalFailsAndPoolCloseRetries(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	startErr := errors.New("start failed")
	removeErr := errors.New("daemon busy")
	c.containerStart = func(context.Context, string) error { return startErr }
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return removeErr }
	p := newFakePool(t, c)

	_, err := p.Run(t.Context(), "alpine", WithoutReuse())
	if !errors.Is(err, startErr) || !errors.Is(err, removeErr) {
		t.Fatalf("Run() error = %v, want both the start error and the removal error", err)
	}
	if left := o.leftovers(); len(left) != 1 || left[0] != "container container-1" {
		t.Fatalf("leftovers = %v, want the unremoved container", left)
	}

	c.containerRemove = nil
	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v, want nil after successful retry", err)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers after retry = %v, want none", left)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 2 {
		t.Fatalf("ContainerRemove calls = %q, want one failed and one retried removal", got)
	}
}

func TestRunInspectFailureRollsBack(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	inspectErr := errors.New("inspect failed")
	c.containerInspect = func(context.Context, string) (container.InspectResponse, error) {
		return container.InspectResponse{}, inspectErr
	}
	p := newFakePool(t, c)

	_, err := p.Run(t.Context(), "alpine")
	if !errors.Is(err, inspectErr) {
		t.Fatalf("Run() error = %v, want %v", err, inspectErr)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 || got[0] != "ContainerRemove container-1 force=true volumes=true canceled=false" {
		t.Fatalf("ContainerRemove calls = %q", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
	if _, ok := get(p.registryKey("alpine:latest")); ok {
		t.Fatal("registry entry present after failed inspect, want none")
	}
}

func TestRunDuplicateRegistrationRemovesDuplicateContainer(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	p := newFakePool(t, c)

	// A competing registration wins the race while our container is being inspected.
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		register(p.registryKey("alpine:latest"), &resource{pool: p, reuseID: "alpine:latest", container: container.InspectResponse{ID: "winner"}})
		return container.InspectResponse{ID: id}, nil
	}

	r, err := p.Run(t.Context(), "alpine")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if r.ID() != "winner" {
		t.Fatalf("Run() ID = %q, want the canonical container", r.ID())
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 || got[0] != "ContainerRemove container-1 force=true volumes=true canceled=false" {
		t.Fatalf("ContainerRemove calls = %q, want the duplicate removed with volumes", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestNetworkInspectFailureRollsBack(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	inspectErr := errors.New("inspect failed")
	ctx, cancel := context.WithCancel(t.Context())
	// The caller gives up after the network was created.
	c.networkInspect = func(context.Context, string) (network.Inspect, error) { cancel(); return network.Inspect{}, inspectErr }
	p := newFakePool(t, c)

	_, err := p.CreateNetwork(ctx, "net", nil)
	if !errors.Is(err, inspectErr) {
		t.Fatalf("CreateNetwork() error = %v, want %v", err, inspectErr)
	}
	if got := c.callsMatching("NetworkRemove"); len(got) != 1 || got[0] != "NetworkRemove network-net canceled=false" {
		t.Fatalf("NetworkRemove calls = %q", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestCleanupDeadline(t *testing.T) {
	const timeout = time.Hour
	for _, tc := range []struct {
		name  string
		outer time.Duration // caller deadline relative to the start; zero for none
		want  time.Duration // expected removal deadline relative to the start
	}{
		{name: "pool timeout without caller deadline", want: timeout},
		{name: "earlier caller deadline is preserved", outer: time.Minute, want: time.Minute},
		{name: "later caller deadline is shortened", outer: 2 * timeout, want: timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testOwner(t)
				c := newFakeClient()
				var seen time.Time
				c.containerRemove = func(ctx context.Context, _ string, _ mobyclient.ContainerRemoveOptions) error {
					seen, _ = ctx.Deadline()
					return nil
				}
				p := newFakePool(t, c, WithCleanupTimeout(timeout))
				r, err := p.Run(t.Context(), "alpine", WithoutReuse())
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}

				start := time.Now()
				ctx := t.Context()
				if tc.outer != 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, start.Add(tc.outer))
					t.Cleanup(cancel)
				}
				if err := r.Close(ctx); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
				if want := start.Add(tc.want); !seen.Equal(want) {
					t.Fatalf("removal deadline = %v, want %v", seen, want)
				}
			})
		})
	}

	t.Run("expired removal is kept for retry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o, _ := testOwner(t)
			c := newFakeClient()
			c.containerRemove = func(ctx context.Context, _ string, _ mobyclient.ContainerRemoveOptions) error {
				<-ctx.Done()
				return ctx.Err()
			}
			p := newFakePool(t, c, WithCleanupTimeout(timeout))
			if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			start := time.Now()
			if err := p.Close(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Close() error = %v, want context.DeadlineExceeded", err)
			}
			if elapsed := time.Since(start); elapsed != timeout {
				t.Fatalf("Close() took %v, want the cleanup timeout %v", elapsed, timeout)
			}
			if left := o.leftovers(); !slices.Equal(left, []string{"container container-1"}) {
				t.Fatalf("leftovers = %v, want the container", left)
			}
		})
	})
}

func TestResourceCloseReleasesOnceUnderConcurrency(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	p := newFakePool(t, c)
	r, err := p.Run(t.Context(), "alpine", WithoutReuse())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := r.Close(t.Context()); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
	}
	wg.Go(func() {
		if err := p.Close(t.Context()); err != nil {
			t.Errorf("pool Close() error = %v", err)
		}
	})
	wg.Wait()

	if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
		t.Fatalf("ContainerRemove calls = %q, want exactly one", got)
	}
}

func TestReusedContainerSharedAcrossPoolsRemovedOnce(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	a := newFakePool(t, c)
	b := newFakePool(t, c)
	ra, err := a.Run(t.Context(), "alpine")
	if err != nil {
		t.Fatalf("a.Run() error = %v", err)
	}
	rb, err := b.Run(t.Context(), "alpine")
	if err != nil {
		t.Fatalf("b.Run() error = %v", err)
	}
	if ra.ID() != rb.ID() {
		t.Fatalf("IDs differ: %s vs %s", ra.ID(), rb.ID())
	}
	if err := a.Close(t.Context()); err != nil {
		t.Fatalf("a.Close() error = %v", err)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
		t.Fatalf("ContainerRemove after first pool closed = %q, want none", got)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatalf("b.Close() error = %v", err)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
		t.Fatalf("ContainerRemove after both pools closed = %q, want one", got)
	}
}

func TestAcquireFailsOnceLastReferenceReleased(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	register("key", &resource{container: container.InspectResponse{ID: "c"}})
	if !release("key") {
		t.Fatal("release() = false, want last reference")
	}
	if _, ok := acquire("key"); ok {
		t.Fatal("acquire() succeeded for a released entry, want failure")
	}
}

func TestShutdownRejectsNewResourcesAndCancelsOperations(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	started := make(chan struct{})
	c.containerCreate = func(ctx context.Context, _ mobyclient.ContainerCreateOptions) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	p := newFakePool(t, c)

	result := make(chan error, 1)
	go func() {
		_, err := p.Run(t.Context(), "alpine", WithoutReuse())
		result <- err
	}()
	<-started

	drain := o.beginShutdown()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight Run() error = %v, want context.Canceled", err)
	}
	if err := drain(t.Context()); err != nil {
		t.Fatalf("drain() error = %v", err)
	}
	if _, err := p.Run(t.Context(), "alpine"); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run() after shutdown error = %v, want ErrShuttingDown", err)
	}
	if _, err := p.CreateNetwork(t.Context(), "n", nil); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("CreateNetwork() after shutdown error = %v, want ErrShuttingDown", err)
	}
}

func TestOwnershipLabelsDoNotModifyCallerMap(t *testing.T) {
	o, _ := testOwner(t)
	user := map[string]string{"team": "a"}
	labels := o.withOwnershipLabels(user, false)
	if len(user) != 1 {
		t.Fatalf("caller map modified: %v", user)
	}
	if labels["team"] != "a" || labels[labelManaged] != "true" || labels[labelRun] != "run-test" || labels[labelHost] != "host-test" {
		t.Fatalf("labels = %v", labels)
	}
	if _, ok := labels[labelScope]; ok {
		t.Fatal("scope label present without Main")
	}
	retained := o.withOwnershipLabels(nil, true)
	if _, ok := retained[labelRun]; ok || retained[labelRetain] != "true" {
		t.Fatalf("retained labels = %v, want retain without run ID", retained)
	}
}

func TestContainerLabelsIncludeOwnership(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	var got map[string]string
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		got = opts.Config.Labels
		return "c1", nil
	}
	p := newFakePool(t, c)
	user := map[string]string{"team": "a"}
	if _, err := p.Run(t.Context(), "alpine", WithLabels(user), WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got["team"] != "a" || got[labelManaged] != "true" || got[labelRun] != "run-test" {
		t.Fatalf("container labels = %v", got)
	}
	if len(user) != 1 {
		t.Fatalf("caller labels modified: %v", user)
	}
}

func TestReadBuildResult(t *testing.T) {
	cases := []struct {
		name    string
		stream  string
		wantID  string
		wantErr string
	}{
		{name: "classic builder", stream: `{"stream":"Step 1"}` + "\n" + `{"aux":{"ID":"sha256:abc"}}` + "\n", wantID: "sha256:abc"},
		{name: "buildkit", stream: `{"id":"moby.buildkit.trace","aux":"AAAA"}` + "\n" + `{"id":"moby.image.id","aux":{"ID":"sha256:def"}}` + "\n", wantID: "sha256:def"},
		{name: "multi stage reports last", stream: `{"aux":{"ID":"sha256:first"}}` + "\n" + `{"aux":{"ID":"sha256:final"}}` + "\n", wantID: "sha256:final"},
		{name: "embedded error", stream: `{"aux":{"ID":"sha256:abc"}}` + "\n" + `{"errorDetail":{"message":"RUN failed"},"error":"RUN failed"}` + "\n", wantErr: "RUN failed"},
		{name: "truncated", stream: `{"aux":{"ID":"sha256:abc"}}` + "\n" + `{"stream":"Succ`, wantErr: "decoding build stream"},
		{name: "no image", stream: `{"stream":"Step 1"}` + "\n", wantErr: "without an image ID"},
		{name: "malformed aux", stream: `{"aux":[1]}` + "\n", wantErr: "decoding build result"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := readBuildResult(strings.NewReader(tc.stream))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || id != tc.wantID {
				t.Fatalf("readBuildResult() = %q, %v; want %q", id, err, tc.wantID)
			}
		})
	}
}

func TestBuildAndRunValidatesOptionsBeforeBuilding(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	p := newFakePool(t, c)
	if _, err := p.BuildAndRun(t.Context(), "img", nil); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("nil options error = %v, want ErrInvalidOption", err)
	}
	if _, err := p.BuildAndRun(t.Context(), "img", &BuildOptions{}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("empty ContextDir error = %v, want ErrInvalidOption", err)
	}
	bad := RunOption(func(*runConfig) error { return errors.New("bad option") })
	if _, err := p.BuildAndRun(t.Context(), "img", &BuildOptions{ContextDir: t.TempDir()}, bad); err == nil || err.Error() != "bad option" {
		t.Fatalf("bad run option error = %v", err)
	}
	if _, err := p.BuildAndRun(t.Context(), "Not:Valid:Ref", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("invalid reference error = %v, want ErrInvalidOption", err)
	}
	if got := c.callsMatching("ImageBuild"); len(got) != 0 {
		t.Fatalf("ImageBuild called despite invalid options: %q", got)
	}
}

func TestBuildImageRemovedAfterLastContainer(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:img1")
	var createdFrom string
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		createdFrom = opts.Config.Image
		return "c1", nil
	}
	p := newFakePool(t, c)

	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	if createdFrom != "sha256:img1" {
		t.Fatalf("container created from %q, want the immutable image ID", createdFrom)
	}
	if users, ok := o.imageUsers("daemon-1", "sha256:img1"); !ok || users != 1 {
		t.Fatalf("image users = %d, %t; want 1 live container", users, ok)
	}
	if _, ok := get(p.registryKey("myapp:test@sha256:img1")); !ok {
		t.Fatal("reuse key does not include the image ID")
	}

	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	want := "ImageRemove sha256:img1 force=false prune=false"
	if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != want {
		t.Fatalf("ImageRemove calls = %q, want [%q]", got, want)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestBuildImageSharedByPoolsRemovedAfterLastUser(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:shared")
	a := newFakePool(t, c)
	b := newFakePool(t, c)
	opts := &BuildOptions{ContextDir: t.TempDir()}

	ra, err := a.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("a.BuildAndRun() error = %v", err)
	}
	rb, err := b.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("b.BuildAndRun() error = %v", err)
	}
	if ra.ID() != rb.ID() {
		t.Fatalf("unchanged build did not reuse the container: %s vs %s", ra.ID(), rb.ID())
	}
	if users, _ := o.imageUsers("daemon-1", "sha256:shared"); users != 1 {
		t.Fatalf("image users = %d, want 1 (one container, no pending builds)", users)
	}

	if err := a.Close(t.Context()); err != nil {
		t.Fatalf("a.Close() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("image removed while pool b still uses it: %q", got)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatalf("b.Close() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 {
		t.Fatalf("ImageRemove calls = %q, want one", got)
	}
}

func TestBuildChangedImageUnderSameTagDoesNotReuseContainer(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string // reuse key of the second build
		opts []RunOption
	}{
		{name: "default reuse key", key: "myapp:test@sha256:v2"},
		{name: "custom reuse key", opts: []RunOption{WithReuseID("custom")}, key: "custom@sha256:v2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testOwner(t)
			c := newFakeClient()
			p := newFakePool(t, c)
			opts := &BuildOptions{ContextDir: t.TempDir()}
			build := func(image string) ClosableResource {
				t.Helper()
				c.imageBuild = c.buildStream(image)
				r, err := p.BuildAndRun(t.Context(), "myapp:test", opts, tc.opts...)
				if err != nil {
					t.Fatalf("BuildAndRun() of %s error = %v", image, err)
				}
				if r.Container().Image != image {
					t.Fatalf("container image = %q, want %q", r.Container().Image, image)
				}
				return r
			}

			r1 := build("sha256:v1")
			r2 := build("sha256:v2")
			r3 := build("sha256:v2")
			if r1.ID() == r2.ID() {
				t.Fatal("changed build reused the container of the previous build")
			}
			if r2.ID() != r3.ID() {
				t.Fatal("unchanged build did not reuse its container")
			}
			if _, ok := get(p.registryKey(tc.key)); !ok {
				t.Fatalf("registry has no entry for %q", tc.key)
			}
		})
	}
}

func TestBuildRetainImageIsNeverRemoved(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:cached")
	p := newFakePool(t, c)
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir(), RetainImage: true})
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	if labels := c.images["sha256:cached"]; labels[labelRetain] != "true" || labels[labelRun] != "" {
		t.Fatalf("retained image labels = %v", labels)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := o.cleanup(t.Context()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("retained image was removed: %q", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none (retained images are not leftovers)", left)
	}
}

func TestBuildDoesNotOwnPreexistingImage(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	c.imageBuild = func(context.Context, mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"aux":{"ID":"sha256:foreign"}}` + "\n")), nil
	}
	c.imageInspect = func(_ context.Context, id string) (image.InspectResponse, error) {
		return inspectWithLabels(id, map[string]string{labelManaged: "true", labelRun: "someone-else"}), nil
	}
	p := newFakePool(t, c)
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("foreign image was removed: %q", got)
	}
}

func TestBuildFailureLeavesImagesUntouched(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = func(context.Context, mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"errorDetail":{"message":"RUN exited 1"}}` + "\n")), nil
	}
	p := newFakePool(t, c)
	_, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if !errors.Is(err, ErrImageBuildFailed) || !strings.Contains(err.Error(), "RUN exited 1") {
		t.Fatalf("BuildAndRun() error = %v, want ErrImageBuildFailed with the daemon message", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("ImageRemove called after failed build: %q", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestBuildRunFailureRemovesFreshImage(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:fresh")
	c.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
		return "", errors.New("no space")
	}
	p := newFakePool(t, c)
	_, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if !errors.Is(err, ErrContainerCreateFailed) {
		t.Fatalf("BuildAndRun() error = %v, want ErrContainerCreateFailed", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:fresh force=false prune=false" {
		t.Fatalf("ImageRemove calls = %q, want the unused image removed without force", got)
	}
}

func TestSweepRunOnlyRemovesFullyMatchingResources(t *testing.T) {
	want := map[string]string{labelManaged: "true", labelScope: "s", labelHost: "h", labelRun: "r1"}
	variants := []struct {
		labels map[string]string
		name   string
	}{
		{labels: map[string]string{labelManaged: "true", labelScope: "s", labelHost: "h", labelRun: "r1"}, name: "owned"},
		{labels: map[string]string{labelScope: "s", labelHost: "h", labelRun: "r1"}, name: "unmanaged"},
		{labels: map[string]string{labelManaged: "true", labelHost: "h", labelRun: "r1"}, name: "no-scope"},
		{labels: map[string]string{labelManaged: "true", labelScope: "other", labelHost: "h", labelRun: "r1"}, name: "other-scope"},
		{labels: map[string]string{labelManaged: "true", labelScope: "s", labelRun: "r1"}, name: "no-host"},
		{labels: map[string]string{labelManaged: "true", labelScope: "s", labelHost: "other", labelRun: "r1"}, name: "other-host"},
		{labels: map[string]string{labelManaged: "true", labelScope: "s", labelHost: "h"}, name: "no-run"},
		{labels: map[string]string{labelManaged: "true", labelScope: "s", labelHost: "h", labelRun: "r2"}, name: "other-run"},
	}
	c := newFakeClient()
	var filters mobyclient.Filters
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		filters = opts.Filters
		all := make([]container.Summary, 0, 2*len(variants))
		for _, v := range variants {
			all = append(all,
				container.Summary{ID: "running-" + v.name, State: container.StateRunning, Labels: v.labels},
				container.Summary{ID: "stopped-" + v.name, State: container.StateExited, Labels: v.labels})
		}
		return listedContainers(opts, all...), nil
	}
	c.networkList = func(context.Context, mobyclient.NetworkListOptions) ([]network.Summary, error) {
		items := make([]network.Summary, 0, len(variants))
		for _, v := range variants {
			items = append(items, network.Summary{Network: network.Network{ID: "network-" + v.name, Labels: v.labels}})
		}
		return items, nil
	}
	c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
		items := make([]image.Summary, 0, len(variants)+1)
		items = append(items, image.Summary{ID: "image-retained", Labels: map[string]string{labelManaged: "true", labelScope: "s", labelHost: "h", labelRun: "r1", labelRetain: "true"}})
		for _, v := range variants {
			items = append(items, image.Summary{ID: "image-" + v.name, Labels: v.labels})
		}
		return items, nil
	}

	if err := sweepRun(t.Context(), c, want); err != nil {
		t.Fatalf("sweepRun() error = %v", err)
	}
	for _, k := range []string{labelManaged + "=true", labelScope + "=s", labelHost + "=h", labelRun + "=r1"} {
		if !filters["label"][k] {
			t.Fatalf("list filters %v miss label %q", filters, k)
		}
	}
	got := slices.Concat(c.callsMatching("ContainerRemove"), c.callsMatching("NetworkRemove"), c.callsMatching("ImageRemove"))
	wantCalls := []string{
		"ContainerRemove running-owned force=true volumes=true canceled=false",
		"ContainerRemove stopped-owned force=true volumes=true canceled=false",
		"NetworkRemove network-owned canceled=false",
		"ImageRemove image-owned force=false prune=false",
	}
	if !slices.Equal(got, wantCalls) {
		t.Fatalf("removals = %q\nwant       %q", got, wantCalls)
	}
}

func TestOwnerCleanupReconcilesLostCreations(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	if err := o.install("scope", nil); err != nil {
		t.Fatalf("install() error = %v", err)
	}
	// A container whose create response was lost: it exists on the daemon with
	// this run's labels but was never tracked.
	c.containerList = func(context.Context, mobyclient.ContainerListOptions) ([]container.Summary, error) {
		return []container.Summary{{ID: "lost", Labels: map[string]string{labelManaged: "true", labelRun: "run-test", labelHost: "host-test", labelScope: "scope"}}}, nil
	}
	p := newFakePool(t, c)
	r, err := p.Run(t.Context(), "alpine", WithoutReuse())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := o.cleanup(t.Context()); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	got := c.callsMatching("ContainerRemove")
	if len(got) != 2 || !strings.HasPrefix(got[0], "ContainerRemove "+r.ID()) || !strings.HasPrefix(got[1], "ContainerRemove lost") {
		t.Fatalf("ContainerRemove calls = %q, want the tracked container and the lost one", got)
	}
	if err := o.closeClients(); err != nil {
		t.Fatalf("closeClients() error = %v", err)
	}
	if c.closed {
		t.Fatal("caller-supplied client was closed")
	}
}

func TestPoolCloseKeepsClientAfterFailedCleanup(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	removeErr := errors.New("busy")
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return removeErr }
	p := newFakePool(t, c)
	p.ownedClient = true // pretend the pool created the client
	if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := p.Close(t.Context()); !errors.Is(err, removeErr) {
		t.Fatalf("Close() error = %v, want %v", err, removeErr)
	}
	if p.client == nil || c.closed {
		t.Fatal("client closed after failed cleanup, want it kept for retry")
	}
	c.containerRemove = nil
	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if p.client != nil || !c.closed {
		t.Fatal("owned client not closed after successful cleanup without Main")
	}
}

func TestPoolCloseKeepsOwnedClientUnderMain(t *testing.T) {
	o, _ := testOwner(t)
	if err := o.install("scope", nil); err != nil {
		t.Fatalf("install() error = %v", err)
	}
	c := newFakeClient()
	p := newFakePool(t, c)
	p.ownedClient = true
	if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if p.client == nil || c.closed {
		t.Fatal("client closed under Main, want Main to close it at shutdown")
	}
	if err := o.closeClients(); err != nil || !c.closed {
		t.Fatalf("closeClients() error = %v, closed = %t", err, c.closed)
	}
}

func TestImageDeletionDeferredWhileBuildPending(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:same")
	p := newFakePool(t, c)
	opts := &BuildOptions{ContextDir: t.TempDir()}
	r1, err := p.BuildAndRun(t.Context(), "myapp:test", opts, WithoutReuse())
	if err != nil {
		t.Fatalf("first BuildAndRun() error = %v", err)
	}

	// A second build is pending and will report the same content-addressed
	// image before it can register as a user.
	building, proceed := make(chan struct{}), make(chan struct{})
	stream := c.buildStream("sha256:same")
	c.imageBuild = func(ctx context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		close(building)
		<-proceed
		return stream(ctx, opts)
	}
	var r2 ClosableResource
	var wg sync.WaitGroup
	wg.Go(func() {
		var buildErr error
		if r2, buildErr = p.BuildAndRun(t.Context(), "myapp:test", opts, WithoutReuse()); buildErr != nil {
			t.Errorf("second BuildAndRun() error = %v", buildErr)
		}
	})
	<-building

	if err := r1.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := o.removeUnusedImages(t.Context()); err != nil {
		t.Fatalf("removeUnusedImages() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("image removed while a build was pending: %q", got)
	}

	close(proceed)
	wg.Wait()
	if users, ok := o.imageUsers("daemon-1", "sha256:same"); !ok || users != 1 {
		t.Fatalf("image users = %d, %t; want the new container", users, ok)
	}
	if err := r2.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 {
		t.Fatalf("ImageRemove calls = %q, want one after the last user", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestBuildWaitsForInflightImageDeletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		c.imageBuild = c.buildStream("sha256:same")
		p := newFakePool(t, c)
		opts := &BuildOptions{ContextDir: t.TempDir()}
		r, err := p.BuildAndRun(t.Context(), "myapp:test", opts, WithoutReuse())
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}

		proceed := make(chan struct{})
		c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error {
			<-proceed
			return nil
		}
		go func() {
			if closeErr := r.Close(t.Context()); closeErr != nil {
				t.Errorf("Close() error = %v", closeErr)
			}
		}()
		synctest.Wait()

		// Both builds wait for the deletion; one gives up, the other proceeds
		// once the deletion is done.
		ctx, cancel := context.WithCancel(t.Context())
		canceled := make(chan error, 1)
		go func() {
			_, buildErr := p.BuildAndRun(ctx, "myapp:test", opts, WithoutReuse())
			canceled <- buildErr
		}()
		var released ClosableResource
		releasedDone := make(chan error, 1)
		go func() {
			var buildErr error
			released, buildErr = p.BuildAndRun(t.Context(), "myapp:test", opts, WithoutReuse())
			releasedDone <- buildErr
		}()
		synctest.Wait()
		if got := c.callsMatching("ImageBuild"); len(got) != 1 {
			t.Fatalf("ImageBuild calls = %q, want no build during the deletion", got)
		}

		cancel()
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled BuildAndRun() error = %v, want context.Canceled", err)
		}
		close(proceed)
		if err := <-releasedDone; err != nil {
			t.Fatalf("released BuildAndRun() error = %v", err)
		}
		if got := c.callsMatching("ImageBuild"); len(got) != 2 {
			t.Fatalf("ImageBuild calls = %q, want one build after the deletion", got)
		}
		if users, ok := o.imageUsers("daemon-1", "sha256:same"); !ok || users != 1 {
			t.Fatalf("image users = %d, %t; want the rebuilt image owned by its container", users, ok)
		}
		if err := released.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})
}

func TestImageRecordsAreScopedByDaemon(t *testing.T) {
	o, _ := testOwner(t)
	newClient := func(daemonID string) *fakeClient {
		c := newFakeClient()
		c.daemonID = daemonID
		c.imageBuild = c.buildStream("sha256:same")
		// Container IDs are random and therefore unique across daemons.
		c.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
			return "container-on-" + daemonID, nil
		}
		return c
	}
	c1, c2 := newClient("daemon-1"), newClient("daemon-2")
	p1, p2 := newFakePool(t, c1), newFakePool(t, c2)
	opts := &BuildOptions{ContextDir: t.TempDir()}

	r1, err := p1.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("BuildAndRun() on daemon-1 error = %v", err)
	}
	r2, err := p2.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("BuildAndRun() on daemon-2 error = %v", err)
	}
	for _, d := range []string{"daemon-1", "daemon-2"} {
		if users, ok := o.imageUsers(d, "sha256:same"); !ok || users != 1 {
			t.Fatalf("image users on %s = %d, %t; want 1", d, users, ok)
		}
	}

	if err := r1.Close(t.Context()); err != nil {
		t.Fatalf("Close() on daemon-1 error = %v", err)
	}
	if got := c1.callsMatching("ImageRemove"); len(got) != 1 {
		t.Fatalf("daemon-1 ImageRemove calls = %q, want one", got)
	}
	if got := c2.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("daemon-2 ImageRemove calls = %q, want none while in use", got)
	}
	if err := r2.Close(t.Context()); err != nil {
		t.Fatalf("Close() on daemon-2 error = %v", err)
	}
	if got := c2.callsMatching("ImageRemove"); len(got) != 1 {
		t.Fatalf("daemon-2 ImageRemove calls = %q, want one", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestPoolCloseHonorsDeadlineWhileRemovalInFlight(t *testing.T) {
	for _, kind := range []string{"container", "network"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testOwner(t)
				c := newFakeClient()
				proceed := make(chan struct{})
				c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { <-proceed; return nil }
				c.networkRemove = func(context.Context, string) error { <-proceed; return nil }
				p := newFakePool(t, c)

				var closer interface{ Close(context.Context) error }
				var err error
				if kind == "container" {
					closer, err = p.Run(t.Context(), "alpine", WithoutReuse())
				} else {
					closer, err = p.CreateNetwork(t.Context(), "net", nil)
				}
				if err != nil {
					t.Fatalf("create error = %v", err)
				}
				go func() {
					if closeErr := closer.Close(t.Context()); closeErr != nil {
						t.Errorf("Close() error = %v", closeErr)
					}
				}()
				synctest.Wait()

				// The pool's Close shares the in-flight removal but gives up
				// at its own deadline, before the pool's cleanup timeout.
				const deadline = 30 * time.Second
				ctx, cancel := context.WithTimeout(t.Context(), deadline)
				defer cancel()
				start := time.Now()
				if err := p.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Pool.Close() error = %v, want context.DeadlineExceeded", err)
				}
				if elapsed := time.Since(start); elapsed != deadline {
					t.Fatalf("Pool.Close() took %v, want %v", elapsed, deadline)
				}
				close(proceed)
			})
		})
	}
}

func TestDaemonRegistrationWaitsForHook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		proceed := make(chan struct{})
		hookErr := errors.New("manifest write failed")
		var calls int
		err := o.install("scope", func(context.Context, string, client.DockerClient) error {
			calls++
			if calls == 1 {
				<-proceed
				return hookErr
			}
			return nil
		})
		if err != nil {
			t.Fatalf("install() error = %v", err)
		}
		a, b := newFakePool(t, c), newFakePool(t, c)

		run := func(ctx context.Context, p *pool) <-chan error {
			result := make(chan error, 1)
			go func() {
				_, runErr := p.Run(ctx, "alpine", WithoutReuse())
				result <- runErr
			}()
			return result
		}
		first := run(t.Context(), a)
		synctest.Wait()

		// The daemon is not registered while the hook is pending: other
		// callers wait for it instead of creating resources.
		ctx, cancel := context.WithCancel(t.Context())
		canceled := run(ctx, b)
		waiting := run(t.Context(), b)
		synctest.Wait()
		if got := c.callsMatching("ContainerCreate"); len(got) != 0 {
			t.Fatalf("container created before the daemon hook finished: %q", got)
		}
		cancel()
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Run() error = %v, want context.Canceled", err)
		}

		close(proceed)
		if err := <-first; !errors.Is(err, hookErr) {
			t.Fatalf("first Run() error = %v, want the hook error", err)
		}
		// The waiting caller runs the failed hook again.
		if err := <-waiting; err != nil {
			t.Fatalf("waiting Run() error = %v", err)
		}
		if calls != 2 {
			t.Fatalf("hook calls = %d, want the failed hook retried", calls)
		}
		if got := c.callsMatching("ContainerCreate"); len(got) != 1 {
			t.Fatalf("ContainerCreate calls = %q, want only the retried caller's", got)
		}
	})
}

func TestImageRemoveConflictIsRetainedForRetry(t *testing.T) {
	conflict := fmt.Errorf("%w: unable to delete (must be forced) - image is referenced in multiple repositories", errdefs.ErrConflict)
	// newClient returns a client on which image sha256:x is tagged twice, so
	// that deletion by ID conflicts. Tag myapp:b may meanwhile point to an
	// unrelated image, so it must never be removed.
	newClient := func() *fakeClient {
		c := newFakeClient()
		c.imageBuild = c.buildStream("sha256:x")
		c.imageInspect = func(_ context.Context, id string) (image.InspectResponse, error) {
			c.mu.Lock()
			resp := inspectWithLabels(id, c.images[id])
			c.mu.Unlock()
			resp.RepoTags = []string{"myapp:a", "myapp:b"}
			return resp, nil
		}
		c.imageRemove = func(_ context.Context, ref string, _ mobyclient.ImageRemoveOptions) error {
			if ref == "sha256:x" {
				return conflict
			}
			return nil
		}
		return c
	}
	onlyRemovedByID := func(t *testing.T, c *fakeClient, attempts int) {
		t.Helper()
		got := c.callsMatching("ImageRemove")
		if len(got) != attempts {
			t.Fatalf("ImageRemove calls = %q, want %d", got, attempts)
		}
		for _, call := range got {
			if call != "ImageRemove sha256:x force=false prune=false" {
				t.Fatalf("ImageRemove calls = %q, want only non-forced removals by image ID", got)
			}
		}
	}

	t.Run("owned image", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newClient()
		p := newFakePool(t, c)
		r, err := p.BuildAndRun(t.Context(), "myapp:a", &BuildOptions{ContextDir: t.TempDir(), Tags: []string{"myapp:a", "myapp:b"}}, WithoutReuse())
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		if err := r.Close(t.Context()); !errors.Is(err, errdefs.ErrConflict) {
			t.Fatalf("Close() error = %v, want the conflict", err)
		}
		onlyRemovedByID(t, c, 1)
		if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
			t.Fatalf("leftovers = %v, want the conflicting image kept for retry", left)
		}

		// Once the extra tag is gone, a later cleanup retries by ID.
		c.imageRemove = nil
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		onlyRemovedByID(t, c, 2)
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("sweep", func(t *testing.T) {
		c := newClient()
		want := map[string]string{labelManaged: "true", labelRun: "r1", labelHost: "h"}
		c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
			return []image.Summary{{ID: "sha256:x", Labels: want}}, nil
		}
		if err := sweepRun(t.Context(), c, want); !errors.Is(err, errdefs.ErrConflict) {
			t.Fatalf("sweepRun() error = %v, want the conflict", err)
		}
		onlyRemovedByID(t, c, 1)
	})
}

func TestSharedContainerRemovalRetriedByLastReleasingPool(t *testing.T) {
	o, _ := testOwner(t)
	// Two clients for the same daemon, and one for an unrelated daemon.
	newClient := func(daemonID string) *fakeClient {
		c := newFakeClient()
		c.daemonID = daemonID
		c.imageBuild = c.buildStream("sha256:img")
		return c
	}
	ca, cb, other := newClient("daemon-1"), newClient("daemon-1"), newClient("daemon-2")
	a, b, unrelated := newFakePool(t, ca), newFakePool(t, cb), newFakePool(t, other)
	a.ownedClient = true // a closes its client once its cleanup succeeded
	// Container IDs are random and therefore unique across daemons.
	other.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
		return "container-on-daemon-2", nil
	}
	opts := &BuildOptions{ContextDir: t.TempDir()}

	ra, err := a.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("a.BuildAndRun() error = %v", err)
	}
	rb, err := b.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("b.BuildAndRun() error = %v", err)
	}
	if ra.ID() != rb.ID() {
		t.Fatalf("container not reused: %s vs %s", ra.ID(), rb.ID())
	}
	if _, err := unrelated.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatalf("unrelated.Run() error = %v", err)
	}

	if err := a.Close(t.Context()); err != nil {
		t.Fatalf("a.Close() error = %v", err)
	}
	if got := ca.callsMatching("ContainerRemove"); len(got) != 0 {
		t.Fatalf("ContainerRemove while b still holds a reference = %q", got)
	}
	if !ca.closed || a.client != nil {
		t.Fatal("a's owned client not closed after its cleanup succeeded")
	}

	removeErr := errors.New("busy")
	cb.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return removeErr }
	if err := rb.Close(t.Context()); !errors.Is(err, removeErr) {
		t.Fatalf("rb.Close() error = %v, want %v", err, removeErr)
	}
	cb.containerRemove = nil
	if err := unrelated.Close(t.Context()); err != nil {
		t.Fatalf("unrelated.Close() error = %v", err)
	}
	if got := other.callsMatching("ContainerRemove " + rb.ID()); len(got) != 0 {
		t.Fatalf("container retried through another daemon's client: %q", got)
	}

	// b released the last reference, so b's cleanup retries the removal.
	if err := b.Close(t.Context()); err != nil {
		t.Fatalf("b.Close() error = %v", err)
	}
	want := "ContainerRemove " + rb.ID() + " force=true volumes=true canceled=false"
	if got := cb.callsMatching("ContainerRemove"); len(got) != 2 || got[1] != want {
		t.Fatalf("b's ContainerRemove calls = %q, want a failed attempt and a retry", got)
	}
	if got := cb.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:img force=false prune=false" {
		t.Fatalf("b's ImageRemove calls = %q, want the image removed once", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}

func TestOwnershipLabelsOverrideReservedRetainLabel(t *testing.T) {
	o, _ := testOwner(t)
	// A derived image inherits its base image's labels, and callers may pass
	// the reserved label themselves: only RetainImage decides retention.
	inherited := map[string]string{labelRetain: labelTrue}
	if got := o.withOwnershipLabels(inherited, false)[labelRetain]; got != "false" {
		t.Fatalf("retain label of a non-retained resource = %q, want false", got)
	}
	if got := o.withOwnershipLabels(map[string]string{labelRetain: "false"}, true)[labelRetain]; got != labelTrue {
		t.Fatalf("retain label of a retained image = %q, want true", got)
	}
	if inherited[labelRetain] != labelTrue {
		t.Fatalf("caller map modified: %v", inherited)
	}

	// Recovery removes an image whose retention was overridden.
	c := newFakeClient()
	want := map[string]string{labelManaged: "true", labelHost: "host-test", labelRun: "run-test"}
	derived := o.withOwnershipLabels(inherited, false)
	c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
		return []image.Summary{{ID: "sha256:derived", Labels: derived}}, nil
	}
	if err := sweepRun(t.Context(), c, want); err != nil {
		t.Fatalf("sweepRun() error = %v", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:derived force=false prune=false" {
		t.Fatalf("ImageRemove calls = %q, want the derived image removed", got)
	}
}

func TestDeferredImageDeletionRetriedWhenBuildsDrain(t *testing.T) {
	buildX := func(t *testing.T, p *pool, c *fakeClient) ClosableResource {
		t.Helper()
		c.imageBuild = c.buildStream("sha256:x")
		x, err := p.BuildAndRun(t.Context(), "myapp:x", &BuildOptions{ContextDir: t.TempDir()}, WithoutReuse())
		if err != nil {
			t.Fatalf("BuildAndRun(x) error = %v", err)
		}
		return x
	}
	// startY starts a build of myapp:y through p, which blocks in the daemon
	// until proceed is closed and then reports image id, or fails if id is
	// empty. The build's resource is sent on the returned channel.
	startY := func(t *testing.T, p *pool, c *fakeClient, id string, proceed <-chan struct{}) <-chan ClosableResource {
		c.imageBuild = func(ctx context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
			<-proceed
			if id == "" {
				return io.NopCloser(strings.NewReader(`{"errorDetail":{"message":"RUN exited 1"}}` + "\n")), nil
			}
			return c.buildStream(id)(ctx, opts)
		}
		y := make(chan ClosableResource, 1)
		go func() {
			r, err := p.BuildAndRun(t.Context(), "myapp:y", &BuildOptions{ContextDir: t.TempDir()}, WithoutReuse())
			if (err == nil) != (id != "") {
				t.Errorf("BuildAndRun(y) error = %v", err)
			}
			y <- r
		}()
		return y
	}
	removedX := func(t *testing.T, c *fakeClient) {
		t.Helper()
		if got := c.callsMatching("ImageRemove sha256:x"); len(got) != 1 {
			t.Fatalf("ImageRemove calls for x = %q, want one", got)
		}
	}

	for _, result := range []string{"", "sha256:y"} {
		t.Run("second build "+cmp.Or(result, "fails"), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				o, _ := testOwner(t)
				c := newFakeClient()
				p := newFakePool(t, c)
				x := buildX(t, p, c)
				proceed := make(chan struct{})
				pending := startY(t, p, c, result, proceed)
				synctest.Wait()

				if err := x.Close(t.Context()); err != nil {
					t.Fatalf("x.Close() error = %v", err)
				}
				if got := c.callsMatching("ImageRemove"); len(got) != 0 {
					t.Fatalf("image removed while a build was pending: %q", got)
				}

				// Only resources are closed: no pool cleanup triggers the retry.
				close(proceed)
				y := <-pending
				removedX(t, c)
				if y != nil {
					if err := y.Close(t.Context()); err != nil {
						t.Fatalf("y.Close() error = %v", err)
					}
				}
				if left := o.leftovers(); len(left) != 0 {
					t.Fatalf("leftovers = %v, want none", left)
				}
			})
		})
	}

	t.Run("originating pool closed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o, _ := testOwner(t)
			ca, cb := newFakeClient(), newFakeClient()
			a, b := newFakePool(t, ca), newFakePool(t, cb)
			a.ownedClient = true
			x := buildX(t, a, ca)
			proceed := make(chan struct{})
			pending := startY(t, b, cb, "", proceed)
			synctest.Wait()

			if err := x.Close(t.Context()); err != nil {
				t.Fatalf("x.Close() error = %v", err)
			}
			if err := a.Close(t.Context()); err != nil {
				t.Fatalf("a.Close() error = %v", err)
			}
			if !ca.closed {
				t.Fatal("a's owned client not closed")
			}
			close(proceed)
			<-pending
			if got := ca.callsMatching("ImageRemove"); len(got) != 0 {
				t.Fatalf("image removed through the closed client: %q", got)
			}
			removedX(t, cb)
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	})

	t.Run("failed retry is kept", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o, stderr := testOwner(t)
			c := newFakeClient()
			p := newFakePool(t, c)
			x := buildX(t, p, c)
			proceed := make(chan struct{})
			pending := startY(t, p, c, "", proceed)
			synctest.Wait()

			if err := x.Close(t.Context()); err != nil {
				t.Fatalf("x.Close() error = %v", err)
			}
			removeErr := errors.New("busy")
			c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error { return removeErr }
			close(proceed)
			<-pending
			if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
				t.Fatalf("leftovers = %v, want x kept for retry", left)
			}
			if !strings.Contains(stderr.String(), removeErr.Error()) {
				t.Fatalf("stderr = %q, want the failed retry reported", stderr.String())
			}

			c.imageRemove = nil
			if err := p.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := c.callsMatching("ImageRemove sha256:x"); len(got) != 2 {
				t.Fatalf("ImageRemove calls for x = %q, want a failed attempt and a retry", got)
			}
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	})

	t.Run("retained and used images stay", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			testOwner(t)
			c := newFakeClient()
			p := newFakePool(t, c)
			c.imageBuild = c.buildStream("sha256:retained")
			retained, err := p.BuildAndRun(t.Context(), "myapp:r", &BuildOptions{ContextDir: t.TempDir(), RetainImage: true}, WithoutReuse())
			if err != nil {
				t.Fatalf("BuildAndRun(retained) error = %v", err)
			}
			if err := retained.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			buildX(t, p, c)
			proceed := make(chan struct{})
			pending := startY(t, p, c, "", proceed)
			synctest.Wait()

			close(proceed)
			<-pending
			if got := c.callsMatching("ImageRemove"); len(got) != 0 {
				t.Fatalf("ImageRemove calls = %q, want retained and used images kept", got)
			}
		})
	})
}

func TestRecoveryRemovesIntermediateImage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	testOwner(t)
	ctx := t.Context()
	c, err := client.NewMobyClient(ctx)
	if err != nil {
		t.Fatalf("NewMobyClient() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	p, err := NewPool(ctx, "", WithMobyClient(c))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(ctx)) })
	build := func(dockerfile string) string {
		dir := t.TempDir()
		if writeErr := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o600); writeErr != nil {
			t.Fatalf("WriteFile() error = %v", writeErr)
		}
		r, buildErr := p.BuildAndRun(ctx, "dockertest-intermediate:test", &BuildOptions{ContextDir: dir}, WithoutReuse())
		if buildErr != nil {
			t.Fatalf("BuildAndRun() error = %v", buildErr)
		}
		return r.Container().Image
	}

	// The child takes over the parent's tag, so the owned parent becomes an
	// untagged intermediate image that a default image listing omits.
	parentID := build("FROM alpine:latest\nCMD [\"sleep\", \"300\"]\n")
	childID := build("FROM dockertest-intermediate:test\nRUN true\n")
	want := map[string]string{labelManaged: "true", labelHost: "host-test", labelRun: "run-test"}
	filters := mobyclient.Filters{}
	for k, v := range want {
		filters.Add("label", k+"="+v)
	}
	listed, err := c.ImageList(ctx, mobyclient.ImageListOptions{Filters: filters})
	if err != nil {
		t.Fatalf("ImageList() error = %v", err)
	}
	if slices.ContainsFunc(listed.Items, func(img image.Summary) bool { return img.ID == parentID }) {
		t.Skip("backend lists the parent image without All, so it has no intermediate images")
	}

	// The run is abandoned: recovery sweeps everything carrying its labels.
	if err := sweepRun(ctx, c, want); err != nil {
		t.Fatalf("sweepRun() error = %v", err)
	}
	for _, id := range []string{childID, parentID} {
		if _, err := c.ImageInspect(ctx, id); !errdefs.IsNotFound(err) {
			t.Errorf("ImageInspect(%s) error = %v, want not found", id, err)
		}
	}
}

func TestRecoveryRemovesImageDerivedFromRetainedImage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	testOwner(t)
	ctx := t.Context()
	c, err := client.NewMobyClient(ctx)
	if err != nil {
		t.Fatalf("NewMobyClient() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	const baseTag = "dockertest-retained-base:test"
	t.Cleanup(func() {
		_, _ = c.ImageRemove(context.WithoutCancel(ctx), baseTag, mobyclient.ImageRemoveOptions{})
	})
	p, err := NewPool(ctx, "", WithMobyClient(c))
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Close(context.WithoutCancel(ctx)) })
	writeDockerfile := func(content string) string {
		dir := t.TempDir()
		if writeErr := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o600); writeErr != nil {
			t.Fatalf("WriteFile() error = %v", writeErr)
		}
		return dir
	}

	base, err := p.BuildAndRun(ctx, baseTag, &BuildOptions{ContextDir: writeDockerfile("FROM alpine:latest\nCMD [\"sleep\", \"300\"]\n"), RetainImage: true}, WithoutReuse())
	if err != nil {
		t.Fatalf("BuildAndRun(base) error = %v", err)
	}
	baseID := base.Container().Image
	if err = base.Close(ctx); err != nil {
		t.Fatalf("base.Close() error = %v", err)
	}
	derived, err := p.BuildAndRun(ctx, "dockertest-derived:test", &BuildOptions{ContextDir: writeDockerfile("FROM " + baseTag + "\n")}, WithoutReuse())
	if err != nil {
		t.Fatalf("BuildAndRun(derived) error = %v", err)
	}
	derivedID := derived.Container().Image
	inspect, err := c.ImageInspect(ctx, derivedID)
	if err != nil {
		t.Fatalf("ImageInspect(derived) error = %v", err)
	}
	if got := inspect.Config.Labels[labelRetain]; got != "false" {
		t.Fatalf("derived image retain label = %q, want false despite the retained base", got)
	}

	// The run is abandoned: recovery sweeps everything carrying its labels.
	if err := sweepRun(ctx, c, map[string]string{labelManaged: "true", labelHost: "host-test", labelRun: "run-test"}); err != nil {
		t.Fatalf("sweepRun() error = %v", err)
	}
	if _, err := c.ImageInspect(ctx, derivedID); !errdefs.IsNotFound(err) {
		t.Fatalf("ImageInspect(derived) error = %v, want not found", err)
	}
	if _, err := c.ImageInspect(ctx, baseID); err != nil {
		t.Fatalf("retained base image removed: %v", err)
	}
}

// buildOwnedImage builds sha256:x as myapp:test through p and returns the
// container that uses it.
func buildOwnedImage(t *testing.T, p *pool, c *fakeClient) ClosableResource {
	t.Helper()
	c.imageBuild = c.buildStream("sha256:x")
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	return r
}

func TestRunProtectsResolvedImageUntilContainerAttached(t *testing.T) {
	inspectX := func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:x"}, nil
	}
	// pauseCreate blocks the creation of a container from the tag myapp:test
	// until proceed is closed; the build's own container is created by ID.
	pauseCreate := func(c *fakeClient, createErr error) (creating, proceed chan struct{}) {
		creating, proceed = make(chan struct{}), make(chan struct{})
		var n int
		c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
			n++
			if opts.Config.Image == "myapp:test" {
				close(creating)
				<-proceed
				if createErr != nil {
					return "", createErr
				}
			}
			return fmt.Sprintf("%s-container-%d", c.daemonID, n), nil
		}
		return creating, proceed
	}

	for _, samePool := range []bool{true, false} {
		t.Run(fmt.Sprintf("same pool %t", samePool), func(t *testing.T) {
			o, _ := testOwner(t)
			c := newFakeClient()
			creating, proceed := pauseCreate(c, nil)
			a := newFakePool(t, c)
			b := a
			if !samePool {
				other := newFakeClient()
				other.containerInspect = inspectX
				other.containerCreate = c.containerCreate
				b = newFakePool(t, other)
			}
			ra := buildOwnedImage(t, a, c)

			var rb ClosableResource
			var wg sync.WaitGroup
			wg.Go(func() {
				var runErr error
				if rb, runErr = b.Run(t.Context(), "myapp", WithTag("test"), WithoutReuse()); runErr != nil {
					t.Errorf("Run() error = %v", runErr)
				}
			})
			<-creating
			if err := ra.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := c.callsMatching("ImageRemove"); len(got) != 0 {
				t.Fatalf("image removed while Run was creating a container from it: %q", got)
			}
			close(proceed)
			wg.Wait()
			if users, ok := o.imageUsers("daemon-1", "sha256:x"); !ok || users != 1 {
				t.Fatalf("image users = %d, %t; want the new container", users, ok)
			}
			if err := rb.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			removals := c.callsMatching("ImageRemove")
			if !samePool {
				removals = append(removals, b.client.(*fakeClient).callsMatching("ImageRemove")...)
			}
			if len(removals) != 1 || removals[0] != "ImageRemove sha256:x force=false prune=false" {
				t.Fatalf("ImageRemove calls = %q, want one after the last container", removals)
			}
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	}

	failures := map[string]func(c *fakeClient, failure error) (creating, proceed chan struct{}){
		"create": func(c *fakeClient, failure error) (chan struct{}, chan struct{}) {
			return pauseCreate(c, failure)
		},
		"start": func(c *fakeClient, failure error) (chan struct{}, chan struct{}) {
			c.containerStart = func(_ context.Context, id string) error {
				if id == "daemon-1-container-2" {
					return failure
				}
				return nil
			}
			return pauseCreate(c, nil)
		},
		"inspect": func(c *fakeClient, failure error) (chan struct{}, chan struct{}) {
			c.containerInspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
				if id == "daemon-1-container-2" {
					return container.InspectResponse{}, failure
				}
				return inspectX(ctx, id)
			}
			return pauseCreate(c, nil)
		},
	}
	for _, step := range slices.Sorted(maps.Keys(failures)) {
		t.Run(step+" failure", func(t *testing.T) {
			o, _ := testOwner(t)
			c := newFakeClient()
			failure := errors.New(step + " failed")
			creating, proceed := failures[step](c, failure)
			p := newFakePool(t, c)
			ra := buildOwnedImage(t, p, c)

			var wg sync.WaitGroup
			wg.Go(func() {
				if _, runErr := p.Run(t.Context(), "myapp", WithTag("test"), WithoutReuse()); !errors.Is(runErr, failure) {
					t.Errorf("Run() error = %v, want %v", runErr, failure)
				}
			})
			<-creating
			if err := ra.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := c.callsMatching("ImageRemove"); len(got) != 0 {
				t.Fatalf("image removed while Run was creating a container from it: %q", got)
			}
			close(proceed)
			wg.Wait()
			// The failed Run released its protection and performed the
			// deferred deletion itself.
			if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:x force=false prune=false" {
				t.Fatalf("ImageRemove calls = %q, want the deferred deletion", got)
			}
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	}

	t.Run("unrelated daemon is not blocked", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		creating, proceed := pauseCreate(c, nil)
		p := newFakePool(t, c)
		other := newFakeClient()
		other.daemonID = "daemon-2"
		other.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
			return "daemon-2-container", nil
		}
		q := newFakePool(t, other)
		rq := buildOwnedImage(t, q, other)

		var rp ClosableResource
		var wg sync.WaitGroup
		wg.Go(func() {
			var runErr error
			if rp, runErr = p.Run(t.Context(), "myapp", WithTag("test"), WithoutReuse()); runErr != nil {
				t.Errorf("Run() error = %v", runErr)
			}
		})
		<-creating
		if err := rq.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := other.callsMatching("ImageRemove"); len(got) != 1 {
			t.Fatalf("daemon-2 ImageRemove calls = %q, want the image removed despite the pending Run on daemon-1", got)
		}
		close(proceed)
		wg.Wait()
		if err := rp.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("waits for in-flight deletion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o, _ := testOwner(t)
			c := newFakeClient()
			p := newFakePool(t, c)
			r := buildOwnedImage(t, p, c)

			proceed := make(chan struct{})
			c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error {
				<-proceed
				return nil
			}
			go func() {
				if closeErr := r.Close(t.Context()); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			}()
			synctest.Wait()

			ctx, cancel := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() {
				_, runErr := p.Run(ctx, "myapp", WithTag("test"), WithoutReuse())
				result <- runErr
			}()
			synctest.Wait()
			if got := c.callsMatching("ImageInspect myapp:test"); len(got) != 0 {
				t.Fatalf("image resolved during its deletion: %q", got)
			}
			cancel()
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want context.Canceled", err)
			}
			close(proceed)
			synctest.Wait()
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	})
}

func TestBuildKeepsUnverifiedImageForCleanup(t *testing.T) {
	// setup returns a pool whose build reports sha256:x and whose image
	// inspections are answered by inspect, numbered from 1.
	setup := func(t *testing.T, c *fakeClient, inspect func(ctx context.Context, n int, labels map[string]string) (image.InspectResponse, error)) *pool {
		t.Helper()
		c.imageBuild = c.buildStream("sha256:x")
		var n int
		c.imageInspect = func(ctx context.Context, id string) (image.InspectResponse, error) {
			c.mu.Lock()
			n++
			i, labels := n, c.images[id]
			c.mu.Unlock()
			resp, err := inspect(ctx, i, labels)
			resp.ID = id
			return resp, err
		}
		return newFakePool(t, c)
	}
	unavailable := errors.New("daemon unavailable")

	t.Run("canceled inspection is verified by rollback", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		ctx, cancel := context.WithCancel(t.Context())
		p := setup(t, c, func(inspectCtx context.Context, n int, labels map[string]string) (image.InspectResponse, error) {
			if n == 1 {
				cancel()
				return image.InspectResponse{}, inspectCtx.Err()
			}
			if inspectCtx.Err() != nil {
				t.Errorf("verification context error = %v, want a usable context", inspectCtx.Err())
			}
			return inspectWithLabels("", labels), nil
		})
		if _, err := p.BuildAndRun(ctx, "myapp:test", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, ErrImageBuildFailed) || !errors.Is(err, context.Canceled) {
			t.Fatalf("BuildAndRun() error = %v, want ErrImageBuildFailed wrapping context.Canceled", err)
		}
		if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:x force=false prune=false" {
			t.Fatalf("ImageRemove calls = %q, want the verified image removed", got)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("persistent failure is retried by Pool.Close", func(t *testing.T) {
		o, stderr := testOwner(t)
		c := newFakeClient()
		failing := true
		p := setup(t, c, func(_ context.Context, _ int, labels map[string]string) (image.InspectResponse, error) {
			if failing {
				return image.InspectResponse{}, unavailable
			}
			return inspectWithLabels("", labels), nil
		})
		p.ownedClient = true
		if _, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		if got := c.callsMatching("ImageInspect"); len(got) != 2 {
			t.Fatalf("ImageInspect calls = %q, want the failed inspection and one rollback verification", got)
		}
		if !strings.Contains(stderr.String(), unavailable.Error()) {
			t.Fatalf("stderr = %q, want the failed verification reported", stderr.String())
		}
		if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
			t.Fatalf("leftovers = %v, want the unverified image", left)
		}
		if err := p.Close(t.Context()); !errors.Is(err, unavailable) {
			t.Fatalf("Close() error = %v, want %v", err, unavailable)
		}
		if c.closed {
			t.Fatal("client closed after failed cleanup")
		}

		failing = false
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:x force=false prune=false" {
			t.Fatalf("ImageRemove calls = %q, want the verified image removed", got)
		}
		if left := o.leftovers(); len(left) != 0 || !c.closed {
			t.Fatalf("leftovers = %v, closed = %t; want none and a closed client", left, c.closed)
		}
	})

	t.Run("foreign or absent image is forgotten", func(t *testing.T) {
		for name, verify := range map[string]func(map[string]string) (image.InspectResponse, error){
			"foreign": func(map[string]string) (image.InspectResponse, error) {
				return inspectWithLabels("", map[string]string{labelManaged: labelTrue, labelRun: "other-run"}), nil
			},
			"absent": func(map[string]string) (image.InspectResponse, error) {
				return image.InspectResponse{}, fmt.Errorf("no such image: %w", errdefs.ErrNotFound)
			},
		} {
			t.Run(name, func(t *testing.T) {
				o, _ := testOwner(t)
				c := newFakeClient()
				p := setup(t, c, func(_ context.Context, n int, labels map[string]string) (image.InspectResponse, error) {
					if n == 1 {
						return image.InspectResponse{}, unavailable
					}
					return verify(labels)
				})
				if _, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, unavailable) {
					t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
				}
				if got := c.callsMatching("ImageRemove"); len(got) != 0 {
					t.Fatalf("ImageRemove calls = %q, want none", got)
				}
				if left := o.leftovers(); len(left) != 0 {
					t.Fatalf("leftovers = %v, want none", left)
				}
			})
		}
	})

	t.Run("retained image is never tracked", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := setup(t, c, func(context.Context, int, map[string]string) (image.InspectResponse, error) {
			return image.InspectResponse{}, unavailable
		})
		if _, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir(), RetainImage: true}); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c.callsMatching("ImageInspect"); len(got) != 1 {
			t.Fatalf("ImageInspect calls = %q, want no verification of a retained image", got)
		}
		if got := c.callsMatching("ImageRemove"); len(got) != 0 {
			t.Fatalf("ImageRemove calls = %q, want none", got)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("deletion conflict is kept for retry", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := setup(t, c, func(_ context.Context, n int, labels map[string]string) (image.InspectResponse, error) {
			if n == 1 {
				return image.InspectResponse{}, unavailable
			}
			return inspectWithLabels("", labels), nil
		})
		conflict := fmt.Errorf("%w: image is referenced in multiple repositories", errdefs.ErrConflict)
		c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error { return conflict }
		if _, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
			t.Fatalf("leftovers = %v, want the conflicting image", left)
		}
		c.imageRemove = nil
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c.callsMatching("ImageRemove"); len(got) != 2 {
			t.Fatalf("ImageRemove calls = %q, want the conflict and the retry", got)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("identical IDs on distinct daemons", func(t *testing.T) {
		o, _ := testOwner(t)
		c1 := newFakeClient()
		failing := true
		p1 := setup(t, c1, func(_ context.Context, _ int, labels map[string]string) (image.InspectResponse, error) {
			if failing {
				return image.InspectResponse{}, unavailable
			}
			return inspectWithLabels("", labels), nil
		})
		c2 := newFakeClient()
		c2.daemonID = "daemon-2"
		c2.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
			return "daemon-2-container", nil
		}
		p2 := newFakePool(t, c2)

		if _, err := p1.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()}); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		r2 := buildOwnedImage(t, p2, c2)
		if err := r2.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c2.callsMatching("ImageRemove"); len(got) != 1 {
			t.Fatalf("daemon-2 ImageRemove calls = %q, want one", got)
		}
		if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
			t.Fatalf("leftovers = %v, want daemon-1's unverified image", left)
		}
		failing = false
		if err := p1.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c1.callsMatching("ImageRemove"); len(got) != 1 {
			t.Fatalf("daemon-1 ImageRemove calls = %q, want one", got)
		}
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})
}

func TestContainerNotFoundOnRetryIsSuccess(t *testing.T) {
	notFound := fmt.Errorf("no such container: %w", errdefs.ErrNotFound)
	busy := errors.New("daemon busy")

	// unattached returns a pool with a tracked container that never got an
	// image attached because starting it failed and its rollback failed.
	unattached := func(t *testing.T) (*processOwner, *fakeClient, *pool) {
		t.Helper()
		o, _ := testOwner(t)
		c := newFakeClient()
		c.containerStart = func(context.Context, string) error { return errors.New("start failed") }
		c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return busy }
		p := newFakePool(t, c)
		p.ownedClient = true
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); !errors.Is(err, busy) {
			t.Fatalf("Run() error = %v, want %v", err, busy)
		}
		return o, c, p
	}

	t.Run("pool close", func(t *testing.T) {
		o, c, p := unattached(t)
		c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return notFound }
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v, want nil for an already removed container", err)
		}
		if left := o.leftovers(); len(left) != 0 || !c.closed {
			t.Fatalf("leftovers = %v, closed = %t; want none and a closed client", left, c.closed)
		}
	})

	t.Run("shared attempt", func(t *testing.T) {
		o, c, p := unattached(t)
		removing, proceed := make(chan struct{}), make(chan struct{})
		c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error {
			close(removing)
			<-proceed
			return notFound
		}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { errs[0] = o.removeContainer(t.Context(), p, "container-1") })
		<-removing
		wg.Go(func() {
			errs[1] = o.removeContainer(t.Context(), p, "container-1")
		})
		// The second remover either joins the attempt or finds the record
		// gone; both must report success.
		close(proceed)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("remover %d error = %v, want nil", i, err)
			}
		}
		if got := c.callsMatching("ContainerRemove"); len(got) != 2 {
			t.Fatalf("ContainerRemove calls = %q, want the failed rollback and one shared attempt", got)
		}
	})

	t.Run("real error keeps record", func(t *testing.T) {
		o, _, p := unattached(t)
		if err := p.Close(t.Context()); !errors.Is(err, busy) {
			t.Fatalf("Close() error = %v, want %v", err, busy)
		}
		if left := o.leftovers(); len(left) != 1 || left[0] != "container container-1" {
			t.Fatalf("leftovers = %v, want the container", left)
		}
	})

	t.Run("attached image error propagates", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		r := buildOwnedImage(t, p, c)
		c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return notFound }
		c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error { return busy }
		if err := r.Close(t.Context()); !errors.Is(err, busy) || errors.Is(err, errdefs.ErrNotFound) {
			t.Fatalf("Close() error = %v, want only the image removal error", err)
		}
		if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:x" {
			t.Fatalf("leftovers = %v, want the image", left)
		}
		c.imageRemove = nil
		if err := p.Close(t.Context()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if got := c.callsMatching("ImageRemove"); len(got) != 2 {
			t.Fatalf("ImageRemove calls = %q, want the failure and one retry", got)
		}
	})
}

func TestImageRegistrationCountsAttachedContainers(t *testing.T) {
	const removal = "ImageRemove sha256:x force=false prune=false"
	inspectX := func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:x"}, nil
	}
	// pauseBuild makes the build publish myapp:test as sha256:x and block in
	// its first inspection of sha256:x, before it registers the image, until
	// proceed is closed; that inspection then fails with failure, if set.
	pauseBuild := func(c *fakeClient, failure error) (inspecting, proceed chan struct{}) {
		inspecting, proceed = make(chan struct{}), make(chan struct{})
		c.imageBuild = c.buildStream("sha256:x")
		var paused bool
		c.imageInspect = func(_ context.Context, id string) (image.InspectResponse, error) {
			c.mu.Lock()
			labels, first := c.images[id], id == "sha256:x" && !paused
			paused = paused || first
			c.mu.Unlock()
			if first {
				close(inspecting)
				<-proceed
				if failure != nil {
					return image.InspectResponse{}, failure
				}
			}
			return inspectWithLabels(id, labels), nil
		}
		return inspecting, proceed
	}
	// buildWhile builds myapp:test on p and runs during while the build is
	// paused before registering the image.
	buildWhile := func(t *testing.T, p *pool, c *fakeClient, failure error, during func()) (ClosableResource, error) {
		t.Helper()
		inspecting, proceed := pauseBuild(c, failure)
		var r ClosableResource
		var err error
		var wg sync.WaitGroup
		wg.Go(func() {
			r, err = p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
		})
		<-inspecting
		during()
		close(proceed)
		wg.Wait()
		return r, err
	}
	run := func(t *testing.T, p *pool, opts ...RunOption) ClosableResource {
		t.Helper()
		r, err := p.Run(t.Context(), "myapp", append([]RunOption{WithTag("test")}, opts...)...)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		return r
	}
	closeAll := func(t *testing.T, rs ...ClosableResource) {
		t.Helper()
		for _, r := range rs {
			if err := r.Close(t.Context()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		}
	}
	wantUsers := func(t *testing.T, o *processOwner, daemonID string, want int) {
		t.Helper()
		if users, ok := o.imageUsers(daemonID, "sha256:x"); !ok || users != want {
			t.Fatalf("image users on %s = %d, %t; want %d", daemonID, users, ok, want)
		}
	}
	wantRemovals := func(t *testing.T, c *fakeClient, want ...string) {
		t.Helper()
		if got := c.callsMatching("ImageRemove"); !slices.Equal(got, want) {
			t.Fatalf("ImageRemove calls = %q, want %q", got, want)
		}
	}

	for _, buildFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("preattached containers, build closed first %t", buildFirst), func(t *testing.T) {
			o, _ := testOwner(t)
			c := newFakeClient()
			p := newFakePool(t, c)
			var runs []ClosableResource
			rb, err := buildWhile(t, p, c, nil, func() {
				runs = append(runs, run(t, p, WithoutReuse()), run(t, p, WithoutReuse()))
			})
			if err != nil {
				t.Fatalf("BuildAndRun() error = %v", err)
			}
			wantUsers(t, o, "daemon-1", 3)

			last := runs[1]
			closeAll(t, runs[0])
			if buildFirst {
				closeAll(t, rb)
			} else {
				closeAll(t, last)
				last = rb
			}
			wantRemovals(t, c)
			closeAll(t, last)
			wantRemovals(t, c, removal)
			if left := o.leftovers(); len(left) != 0 {
				t.Fatalf("leftovers = %v, want none", left)
			}
		})
	}

	t.Run("reuse handles of one container count once", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		var handles []ClosableResource
		rb, err := buildWhile(t, p, c, nil, func() {
			handles = append(handles, run(t, p), run(t, p))
		})
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		if got := c.callsMatching("ContainerCreate"); len(got) != 2 {
			t.Fatalf("ContainerCreate calls = %q, want one reused and one built container", got)
		}
		wantUsers(t, o, "daemon-1", 2)
		closeAll(t, rb, handles[0])
		wantRemovals(t, c)
		closeAll(t, handles[1])
		wantRemovals(t, c, removal)
	})

	t.Run("containers on another daemon are not counted", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		other := newFakeClient()
		other.daemonID = "daemon-2"
		other.containerInspect = inspectX
		q := newFakePool(t, other)
		var rq ClosableResource
		rb, err := buildWhile(t, p, c, nil, func() { rq = run(t, q, WithoutReuse()) })
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		wantUsers(t, o, "daemon-1", 1)
		closeAll(t, rb)
		wantRemovals(t, c, removal)
		closeAll(t, rq)
		wantRemovals(t, other)
	})

	t.Run("repeated registration does not recount", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		var rc ClosableResource
		rb, err := buildWhile(t, p, c, nil, func() { rc = run(t, p, WithoutReuse()) })
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		rb2, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()}, WithoutReuse())
		if err != nil {
			t.Fatalf("second BuildAndRun() error = %v", err)
		}
		wantUsers(t, o, "daemon-1", 3)
		closeAll(t, rb, rc)
		wantRemovals(t, c)
		closeAll(t, rb2)
		wantRemovals(t, c, removal)
	})

	t.Run("container removed before registration is not counted", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		rb, err := buildWhile(t, p, c, nil, func() { closeAll(t, run(t, p, WithoutReuse())) })
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		wantUsers(t, o, "daemon-1", 1)
		closeAll(t, rb)
		wantRemovals(t, c, removal)
	})

	t.Run("container removal in flight during registration", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		removing, proceed := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		rb, err := buildWhile(t, p, c, nil, func() {
			rc := run(t, p, WithoutReuse())
			c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error {
				close(removing)
				<-proceed
				return nil
			}
			wg.Go(func() {
				if closeErr := rc.Close(t.Context()); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			})
			<-removing
		})
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		c.containerRemove = nil
		close(proceed)
		wg.Wait()
		wantUsers(t, o, "daemon-1", 1)
		wantRemovals(t, c)
		closeAll(t, rb)
		wantRemovals(t, c, removal)
	})

	unavailable := errors.New("daemon unavailable")
	t.Run("unverified image is kept while a container uses it", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		var rc ClosableResource
		if _, err := buildWhile(t, p, c, unavailable, func() { rc = run(t, p, WithoutReuse()) }); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		wantUsers(t, o, "daemon-1", 1)
		wantRemovals(t, c)

		// The deletion after the last user verifies ownership first; a
		// failing verification keeps the image for a retry.
		c.imageInspect = func(context.Context, string) (image.InspectResponse, error) {
			return image.InspectResponse{}, unavailable
		}
		if err := rc.Close(t.Context()); !errors.Is(err, unavailable) {
			t.Fatalf("Close() error = %v, want %v", err, unavailable)
		}
		wantRemovals(t, c)
		c.imageInspect = nil
		if err := o.removeUnusedImages(t.Context()); err != nil {
			t.Fatalf("removeUnusedImages() error = %v", err)
		}
		wantRemovals(t, c, removal)
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})

	t.Run("unverified image promoted by a later build keeps its users", func(t *testing.T) {
		o, _ := testOwner(t)
		c := newFakeClient()
		p := newFakePool(t, c)
		var rc ClosableResource
		if _, err := buildWhile(t, p, c, unavailable, func() { rc = run(t, p, WithoutReuse()) }); !errors.Is(err, unavailable) {
			t.Fatalf("BuildAndRun() error = %v, want %v", err, unavailable)
		}
		rb, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
		if err != nil {
			t.Fatalf("BuildAndRun() error = %v", err)
		}
		wantUsers(t, o, "daemon-1", 2)
		closeAll(t, rb)
		wantRemovals(t, c)
		closeAll(t, rc)
		wantRemovals(t, c, removal)
		if left := o.leftovers(); len(left) != 0 {
			t.Fatalf("leftovers = %v, want none", left)
		}
	})
}
