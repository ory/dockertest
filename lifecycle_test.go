// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

func TestRunStartFailureRollsBackWithCanceledContext(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	startErr := errors.New("boom")
	c.containerStart = func(context.Context, string) error { return startErr }
	p := newFakePool(t, c)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
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
	c.networkInspect = func(context.Context, string) (network.Inspect, error) { return network.Inspect{}, inspectErr }
	p := newFakePool(t, c)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
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

func TestCleanupPreservesOuterDeadline(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	var seen time.Time
	c.containerRemove = func(ctx context.Context, _ string, _ mobyclient.ContainerRemoveOptions) error {
		seen, _ = ctx.Deadline()
		return nil
	}
	p := newFakePool(t, c, WithCleanupTimeout(time.Hour))
	r, err := p.Run(t.Context(), "alpine", WithoutReuse())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	outer := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), outer)
	t.Cleanup(cancel)
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !seen.Equal(outer) {
		t.Fatalf("removal deadline = %v, want the outer deadline %v", seen, outer)
	}
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
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:img1"}, nil
	}
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
	if users, ok := o.imageUsers("sha256:img1"); !ok || users != 1 {
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
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:shared"}, nil
	}
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
	if users, _ := o.imageUsers("sha256:shared"); users != 1 {
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
	testOwner(t)
	c := newFakeClient()
	images := make([]string, 0, 2)
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: images[len(images)-1]}, nil
	}
	p := newFakePool(t, c)
	opts := &BuildOptions{ContextDir: t.TempDir()}

	images = append(images, "sha256:v1")
	c.imageBuild = c.buildStream("sha256:v1")
	r1, err := p.BuildAndRun(t.Context(), "myapp:test", opts)
	if err != nil {
		t.Fatalf("first BuildAndRun() error = %v", err)
	}
	images = append(images, "sha256:v2")
	c.imageBuild = c.buildStream("sha256:v2")
	r2, err := p.BuildAndRun(t.Context(), "myapp:test", opts, WithReuseID("custom"))
	if err != nil {
		t.Fatalf("second BuildAndRun() error = %v", err)
	}
	r3, err := p.BuildAndRun(t.Context(), "myapp:test", opts, WithReuseID("custom"))
	if err != nil {
		t.Fatalf("third BuildAndRun() error = %v", err)
	}
	if r1.ID() == r2.ID() {
		t.Fatal("changed build reused the container of the previous build")
	}
	if r2.ID() != r3.ID() {
		t.Fatal("unchanged build with the same custom reuse ID did not reuse its container")
	}
	if _, ok := get(p.registryKey("custom@sha256:v2")); !ok {
		t.Fatal("custom reuse key does not include the image ID")
	}
}

func TestBuildRetainImageIsNeverRemoved(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:cached")
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:cached"}, nil
	}
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
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:foreign"}, nil
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

func TestImageRemoveConflictIsRetainedForRetry(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:tagged-twice")
	c.containerInspect = func(_ context.Context, id string) (container.InspectResponse, error) {
		return container.InspectResponse{ID: id, Image: "sha256:tagged-twice"}, nil
	}
	conflict := errors.New("conflict: image is referenced in multiple repositories")
	c.imageRemove = func(context.Context, string, mobyclient.ImageRemoveOptions) error { return conflict }
	p := newFakePool(t, c)
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	if err := r.Close(t.Context()); !errors.Is(err, conflict) {
		t.Fatalf("Close() error = %v, want the conflict", err)
	}
	if left := o.leftovers(); len(left) != 1 || left[0] != "image sha256:tagged-twice" {
		t.Fatalf("leftovers = %v, want the conflicting image", left)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 || !strings.Contains(got[0], "force=false") {
		t.Fatalf("ImageRemove calls = %q, want exactly one non-forced attempt", got)
	}
}

func TestSweepRunOnlyRemovesFullyMatchingResources(t *testing.T) {
	c := newFakeClient()
	want := map[string]string{labelManaged: "true", labelRun: "r1", labelHost: "h"}
	full := map[string]string{labelManaged: "true", labelRun: "r1", labelHost: "h"}
	partial := map[string]string{labelManaged: "true", labelRun: "r1"}
	var filters mobyclient.Filters
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		filters = opts.Filters
		return []container.Summary{{ID: "ok", Labels: full}, {ID: "partial", Labels: partial}}, nil
	}
	c.networkList = func(context.Context, mobyclient.NetworkListOptions) ([]network.Summary, error) {
		return []network.Summary{{Network: network.Network{ID: "n-ok", Labels: full}}, {Network: network.Network{ID: "n-partial", Labels: partial}}}, nil
	}
	c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
		retained := map[string]string{labelManaged: "true", labelRun: "r1", labelHost: "h", labelRetain: "true"}
		return []image.Summary{{ID: "i-ok", Labels: full}, {ID: "i-retained", Labels: retained}, {ID: "i-partial", Labels: partial}}, nil
	}

	if err := sweepRun(t.Context(), c, want); err != nil {
		t.Fatalf("sweepRun() error = %v", err)
	}
	for _, k := range []string{labelManaged + "=true", labelRun + "=r1", labelHost + "=h"} {
		if !filters["label"][k] {
			t.Fatalf("list filters %v miss label %q", filters, k)
		}
	}
	got := strings.Join(c.callsMatching("ContainerRemove"), ";") + "|" + strings.Join(c.callsMatching("NetworkRemove"), ";") + "|" + strings.Join(c.callsMatching("ImageRemove"), ";")
	wantCalls := "ContainerRemove ok force=true volumes=true canceled=false|NetworkRemove n-ok canceled=false|ImageRemove i-ok force=false prune=false"
	if got != wantCalls {
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
