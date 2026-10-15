// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
)

func TestRunStartFailureRollsBackWithCanceledContext(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	startErr := errors.New("boom")
	ctx, cancel := context.WithCancel(t.Context())
	// The caller gives up after the container was created.
	c.containerStart = func(context.Context, string) error { cancel(); return startErr }
	p := newFakePool(t, c)

	r, err := p.Run(ctx, "alpine", WithoutReuse())
	if r != nil {
		t.Fatalf("resource = %#v, want nil on failure", r)
	}
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

	r, err := p.Run(t.Context(), "alpine")
	if r != nil {
		t.Fatalf("resource = %#v, want nil on failure", r)
	}
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
	user := map[string]string{"team": "a", labelExpires: "1"}
	labels := o.withOwnershipLabels(user)
	if len(user) != 2 {
		t.Fatalf("caller map modified: %v", user)
	}
	if labels["team"] != "a" || labels[labelManaged] != "true" || labels[labelRun] != "run-test" {
		t.Fatalf("labels = %v", labels)
	}
	if _, ok := labels[labelExpires]; ok {
		t.Fatal("caller-supplied expires label kept without Main")
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

func TestBuildRunFailurePreservesFreshImage(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	c.imageBuild = c.buildStream("sha256:fresh")
	c.containerCreate = func(context.Context, mobyclient.ContainerCreateOptions) (string, error) {
		return "", errors.New("no space")
	}
	p := newFakePool(t, c)
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir()})
	if r != nil {
		t.Fatalf("resource = %#v, want nil on failure", r)
	}
	if !errors.Is(err, ErrContainerCreateFailed) {
		t.Fatalf("BuildAndRun() error = %v, want ErrContainerCreateFailed", err)
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("ImageRemove calls = %q, want no image removal", got)
	}
}

func TestSweepRemovesExpiredAndOwnRun(t *testing.T) {
	now := time.Unix(1000, 0)
	variants := []struct {
		labels map[string]string
		name   string
	}{
		{labels: map[string]string{labelManaged: "true", labelRun: "r1"}, name: "own-run"},
		{labels: map[string]string{labelManaged: "true", labelRun: "r2", labelExpires: "999"}, name: "expired"},
		{labels: map[string]string{labelManaged: "true", labelRun: "r2", labelExpires: "1000"}, name: "expires-now"},
		{labels: map[string]string{labelManaged: "true", labelRun: "r2", labelExpires: "1001"}, name: "future"},
		{labels: map[string]string{labelManaged: "true", labelRun: "r2"}, name: "no-expiry"},
		{labels: map[string]string{labelManaged: "true", labelRun: "r2", labelExpires: "soon"}, name: "malformed"},
		{labels: map[string]string{labelRun: "r1"}, name: "unmanaged-own-run"},
		{labels: map[string]string{labelRun: "r2", labelExpires: "999"}, name: "unmanaged-expired"},
	}
	c := newFakeClient()
	// Another run's sweep is already removing a container: not an error.
	c.containerRemove = func(_ context.Context, id string, _ mobyclient.ContainerRemoveOptions) error {
		if id == "stopped-expired" {
			return errdefs.ErrConflict
		}
		return nil
	}
	// Another run's expired network cannot be removed: reported, not an error.
	c.networkRemove = func(_ context.Context, id string) error {
		if id == "network-expired" {
			return errors.New("in use")
		}
		return nil
	}
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

	var stderr bytes.Buffer
	o := newProcessOwner("r1", &stderr)
	if err := o.sweep(t.Context(), c, now); err != nil {
		t.Fatalf("sweep() error = %v", err)
	}
	if want := "dockertest: removing network network-expired: in use of expired run r2\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
	if !filters["label"][labelManaged+"=true"] {
		t.Fatalf("list filters %v miss the managed label", filters)
	}
	got := slices.Concat(c.callsMatching("ContainerRemove"), c.callsMatching("NetworkRemove"), c.callsMatching("ImageRemove"))
	wantCalls := []string{
		"ContainerRemove running-own-run force=true volumes=true canceled=false",
		"ContainerRemove stopped-own-run force=true volumes=true canceled=false",
		"ContainerRemove running-expired force=true volumes=true canceled=false",
		"ContainerRemove stopped-expired force=true volumes=true canceled=false",
		"NetworkRemove network-own-run canceled=false",
		"NetworkRemove network-expired canceled=false",
	}
	if !slices.Equal(got, wantCalls) {
		t.Fatalf("removals = %q\nwant       %q", got, wantCalls)
	}
}

func TestSweepFailsOnOwnRunRemovalFailure(t *testing.T) {
	removeErr := errors.New("busy")
	c := newFakeClient()
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return removeErr }
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		return listedContainers(opts, container.Summary{ID: "own", Labels: map[string]string{labelManaged: "true", labelRun: "r1"}}), nil
	}
	var stderr bytes.Buffer
	o := newProcessOwner("r1", &stderr)
	if err := o.sweep(t.Context(), c, time.Unix(1000, 0)); !errors.Is(err, removeErr) {
		t.Fatalf("sweep() error = %v, want %v", err, removeErr)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want the failure returned instead", stderr.String())
	}
}

func TestOwnerCleanupSweepsEachClientOnce(t *testing.T) {
	o, _ := testOwner(t)
	if err := o.install(""); err != nil {
		t.Fatalf("install() error = %v", err)
	}
	// Both clients report the same host, as clients with a custom dialer do,
	// but may talk to different daemons: each must be swept.
	shared, other := newFakeClient(), newFakeClient()
	newFakePool(t, shared)
	newFakePool(t, shared)
	newFakePool(t, other)
	if err := o.cleanup(t.Context(), time.Unix(1000, 0)); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	for name, c := range map[string]*fakeClient{"shared": shared, "other": other} {
		if got := c.callsMatching("ContainerList"); len(got) != 1 {
			t.Fatalf("%s client ContainerList calls = %q, want one sweep", name, got)
		}
	}
}

func TestNoSweepWithoutMain(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	p := newFakePool(t, c)
	if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatalf("Run without Main: %v", err)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatalf("Close without Main: %v", err)
	}
	if calls := slices.Concat(c.callsMatching("ContainerList"), c.callsMatching("NetworkList")); len(calls) != 0 {
		t.Fatalf("daemon swept without Main: %v", calls)
	}
}

func TestOwnerCleanupReconcilesLostCreations(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	if err := o.install(""); err != nil {
		t.Fatalf("install() error = %v", err)
	}
	// A container whose create response was lost: it exists on the daemon with
	// this run's labels but was never tracked.
	c.containerList = func(context.Context, mobyclient.ContainerListOptions) ([]container.Summary, error) {
		return []container.Summary{{ID: "lost", Labels: map[string]string{labelManaged: "true", labelRun: "run-test"}}}, nil
	}
	p := newFakePool(t, c)
	r, err := p.Run(t.Context(), "alpine", WithoutReuse())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := o.cleanup(t.Context(), time.Unix(1000, 0)); err != nil {
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
	if err := o.install(""); err != nil {
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

func TestSharedContainerRemovalRetriedByLastReleasingPool(t *testing.T) {
	o, _ := testOwner(t)
	// Two clients for the same daemon, and one for an unrelated daemon.
	newClient := func(host string) *fakeClient {
		c := newFakeClient()
		c.host = host
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
	if got := cb.callsMatching("ImageRemove"); len(got) != 0 {
		t.Fatalf("b's ImageRemove calls = %q, want no image removal", got)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
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

}

func TestBuildCleanupKeepsImagesAndCallerLabels(t *testing.T) {
	o, _ := testOwner(t)
	c := newFakeClient()
	labels := map[string]string{"team": "build", "io.ory.dockertest.retain": "custom"}
	var got map[string]string
	c.imageBuild = func(_ context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		got = opts.Labels
		return io.NopCloser(strings.NewReader("{\"aux\":{\"ID\":\"sha256:kept\"}}\n")), nil
	}
	p := newFakePool(t, c)
	r, err := p.BuildAndRun(t.Context(), "myapp:test", &BuildOptions{ContextDir: t.TempDir(), Labels: labels}, WithoutReuse())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(labels) || got["team"] != "build" || got["io.ory.dockertest.retain"] != "custom" {
		t.Fatalf("build labels = %v, want caller labels %v", got, labels)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := o.cleanup(t.Context(), time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if calls := c.callsMatching("ImageRemove"); len(calls) != 0 {
		t.Fatalf("cleanup removed images: %v", calls)
	}
	if calls := c.callsMatching("ContainerRemove"); len(calls) != 1 {
		t.Fatalf("container removals = %v, want one", calls)
	}
	if left := o.leftovers(); len(left) != 0 {
		t.Fatalf("leftovers = %v, want none", left)
	}
}
