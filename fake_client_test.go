// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/system"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

// fakeClient is a controllable DockerClient for daemon-free tests. Every
// method succeeds unless the matching hook is set. Calls are recorded in order.
// Containers report the image they were created from, with tags of fake builds
// resolved to the built image ID.
//
//nolint:govet // field alignment traded for readability
type fakeClient struct {
	client.DockerClient // nil: unhooked methods panic, which is intended

	mu         sync.Mutex
	calls      []string
	created    int
	images     map[string]map[string]string // image ID -> labels
	tags       map[string]string            // tag -> image ID, set by buildStream
	containers map[string]string            // container ID -> image reference it was created from
	closed     bool
	daemonID   string

	containerCreate  func(ctx context.Context, opts mobyclient.ContainerCreateOptions) (string, error)
	containerStart   func(ctx context.Context, id string) error
	containerInspect func(ctx context.Context, id string) (container.InspectResponse, error)
	containerRemove  func(ctx context.Context, id string, opts mobyclient.ContainerRemoveOptions) error
	containerList    func(ctx context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error)
	networkCreate    func(ctx context.Context, name string, opts mobyclient.NetworkCreateOptions) (string, error)
	networkInspect   func(ctx context.Context, id string) (network.Inspect, error)
	networkRemove    func(ctx context.Context, id string) error
	networkList      func(ctx context.Context, opts mobyclient.NetworkListOptions) ([]network.Summary, error)
	imageBuild       func(ctx context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error)
	imageInspect     func(ctx context.Context, id string) (image.InspectResponse, error)
	imageRemove      func(ctx context.Context, id string, opts mobyclient.ImageRemoveOptions) error
	info             func(ctx context.Context) (system.Info, error)
}

func newFakeClient() *fakeClient {
	return &fakeClient{images: map[string]map[string]string{}, tags: map[string]string{}, containers: map[string]string{}, daemonID: "daemon-1"}
}

func (f *fakeClient) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

// callsMatching returns the recorded calls that start with prefix.
func (f *fakeClient) callsMatching(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeClient) ContainerCreate(ctx context.Context, opts mobyclient.ContainerCreateOptions) (mobyclient.ContainerCreateResult, error) { //nolint:gocritic // mirrors the client interface
	var id string
	var err error
	if f.containerCreate != nil {
		id, err = f.containerCreate(ctx, opts)
	} else {
		f.mu.Lock()
		f.created++
		id = fmt.Sprintf("container-%d", f.created)
		f.mu.Unlock()
	}
	f.record("ContainerCreate %s", id)
	if err == nil {
		f.mu.Lock()
		f.containers[id] = opts.Config.Image
		f.mu.Unlock()
	}
	return mobyclient.ContainerCreateResult{ID: id}, err
}

func (f *fakeClient) ContainerStart(ctx context.Context, id string, _ mobyclient.ContainerStartOptions) (mobyclient.ContainerStartResult, error) {
	f.record("ContainerStart %s", id)
	if f.containerStart != nil {
		return mobyclient.ContainerStartResult{}, f.containerStart(ctx, id)
	}
	return mobyclient.ContainerStartResult{}, nil
}

func (f *fakeClient) ContainerInspect(ctx context.Context, id string, _ mobyclient.ContainerInspectOptions) (mobyclient.ContainerInspectResult, error) {
	f.record("ContainerInspect %s", id)
	if f.containerInspect != nil {
		resp, err := f.containerInspect(ctx, id)
		return mobyclient.ContainerInspectResult{Container: resp}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := f.containers[id]
	return mobyclient.ContainerInspectResult{Container: container.InspectResponse{ID: id, Image: cmp.Or(f.tags[ref], ref)}}, nil
}

func (f *fakeClient) ContainerRemove(ctx context.Context, id string, opts mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error) {
	f.record("ContainerRemove %s force=%t volumes=%t canceled=%t", id, opts.Force, opts.RemoveVolumes, ctx.Err() != nil)
	if f.containerRemove != nil {
		return mobyclient.ContainerRemoveResult{}, f.containerRemove(ctx, id, opts)
	}
	return mobyclient.ContainerRemoveResult{}, nil
}

func (f *fakeClient) ContainerList(ctx context.Context, opts mobyclient.ContainerListOptions) (mobyclient.ContainerListResult, error) {
	f.record("ContainerList")
	if f.containerList != nil {
		items, err := f.containerList(ctx, opts)
		return mobyclient.ContainerListResult{Items: items}, err
	}
	return mobyclient.ContainerListResult{}, nil
}

// listedContainers returns the containers a daemon lists for opts: stopped
// ones only with All. Label filters are ignored, so only dockertest's own
// ownership check protects foreign containers.
func listedContainers(opts mobyclient.ContainerListOptions, all ...container.Summary) []container.Summary {
	var items []container.Summary
	for i := range all {
		if opts.All || all[i].State == container.StateRunning {
			items = append(items, all[i])
		}
	}
	return items
}

func (f *fakeClient) NetworkCreate(ctx context.Context, name string, opts mobyclient.NetworkCreateOptions) (mobyclient.NetworkCreateResult, error) { //nolint:gocritic // mirrors the client interface
	if f.networkCreate != nil {
		id, err := f.networkCreate(ctx, name, opts)
		f.record("NetworkCreate %s", id)
		return mobyclient.NetworkCreateResult{ID: id}, err
	}
	f.record("NetworkCreate network-%s", name)
	return mobyclient.NetworkCreateResult{ID: "network-" + name}, nil
}

func (f *fakeClient) NetworkInspect(ctx context.Context, id string, _ mobyclient.NetworkInspectOptions) (mobyclient.NetworkInspectResult, error) {
	f.record("NetworkInspect %s", id)
	if f.networkInspect != nil {
		resp, err := f.networkInspect(ctx, id)
		return mobyclient.NetworkInspectResult{Network: resp}, err
	}
	return mobyclient.NetworkInspectResult{Network: network.Inspect{Network: network.Network{ID: id, Name: id}}}, nil
}

func (f *fakeClient) NetworkRemove(ctx context.Context, id string, _ mobyclient.NetworkRemoveOptions) (mobyclient.NetworkRemoveResult, error) {
	f.record("NetworkRemove %s canceled=%t", id, ctx.Err() != nil)
	if f.networkRemove != nil {
		return mobyclient.NetworkRemoveResult{}, f.networkRemove(ctx, id)
	}
	return mobyclient.NetworkRemoveResult{}, nil
}

func (f *fakeClient) NetworkList(ctx context.Context, opts mobyclient.NetworkListOptions) (mobyclient.NetworkListResult, error) {
	f.record("NetworkList")
	if f.networkList != nil {
		items, err := f.networkList(ctx, opts)
		return mobyclient.NetworkListResult{Items: items}, err
	}
	return mobyclient.NetworkListResult{}, nil
}

func (f *fakeClient) ImageBuild(ctx context.Context, buildContext io.Reader, opts mobyclient.ImageBuildOptions) (mobyclient.ImageBuildResult, error) { //nolint:gocritic // mirrors the client interface
	f.record("ImageBuild %s", strings.Join(opts.Tags, ","))
	_, _ = io.Copy(io.Discard, buildContext) //nolint:errcheck // drain the pipe like the daemon would
	if f.imageBuild != nil {
		body, err := f.imageBuild(ctx, opts)
		return mobyclient.ImageBuildResult{Body: body}, err
	}
	return mobyclient.ImageBuildResult{Body: io.NopCloser(strings.NewReader(""))}, nil
}

// buildStream returns a classic-builder stream that labels image id with the
// build's labels, points the build's tags at it, and reports id as the result.
func (f *fakeClient) buildStream(id string) func(context.Context, mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
	return func(_ context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		f.mu.Lock()
		f.images[id] = opts.Labels
		for _, tag := range opts.Tags {
			f.tags[tag] = id
		}
		f.mu.Unlock()
		return io.NopCloser(strings.NewReader(fmt.Sprintf(`{"stream":"Step 1/1 : FROM scratch"}
{"aux":{"ID":%q}}
{"stream":"Successfully built\n"}
`, id))), nil
	}
}

func (f *fakeClient) ImageInspect(ctx context.Context, id string, _ ...mobyclient.ImageInspectOption) (mobyclient.ImageInspectResult, error) {
	f.record("ImageInspect %s", id)
	if f.imageInspect != nil {
		resp, err := f.imageInspect(ctx, id)
		return mobyclient.ImageInspectResult{InspectResponse: resp}, err
	}
	// Unknown references count as present so that Run never pulls.
	f.mu.Lock()
	labels := f.images[id]
	f.mu.Unlock()
	return mobyclient.ImageInspectResult{InspectResponse: inspectWithLabels(id, labels)}, nil
}

func inspectWithLabels(id string, labels map[string]string) image.InspectResponse {
	resp := image.InspectResponse{ID: id}
	resp.Config = &dockerspec.DockerOCIImageConfig{}
	resp.Config.Labels = labels
	return resp
}

func (f *fakeClient) ImageRemove(ctx context.Context, id string, opts mobyclient.ImageRemoveOptions) (mobyclient.ImageRemoveResult, error) {
	f.record("ImageRemove %s force=%t prune=%t", id, opts.Force, opts.PruneChildren)
	if f.imageRemove != nil {
		return mobyclient.ImageRemoveResult{}, f.imageRemove(ctx, id, opts)
	}
	f.mu.Lock()
	delete(f.images, id)
	f.mu.Unlock()
	return mobyclient.ImageRemoveResult{}, nil
}

func (f *fakeClient) Info(ctx context.Context, _ mobyclient.InfoOptions) (mobyclient.SystemInfoResult, error) {
	f.record("Info")
	if f.info != nil {
		info, err := f.info(ctx)
		return mobyclient.SystemInfoResult{Info: info}, err
	}
	return mobyclient.SystemInfoResult{Info: system.Info{ID: f.daemonID}}, nil
}

func (f *fakeClient) Ping(context.Context, mobyclient.PingOptions) (mobyclient.PingResult, error) {
	return mobyclient.PingResult{}, nil
}

func (f *fakeClient) DaemonHost() string { return "fake://" + f.daemonID }

func (f *fakeClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// testOwner swaps the process owner for an isolated instance with injected
// identities and a captured stderr, and restores the original afterwards.
func testOwner(t *testing.T) (*processOwner, *bytes.Buffer) {
	t.Helper()
	ResetRegistry()
	var stderr bytes.Buffer
	o := newProcessOwner("run-test", "host-test", &stderr)
	previous := owner
	owner = o
	t.Cleanup(func() {
		owner = previous
		ResetRegistry()
	})
	return o, &stderr
}

// newFakePool creates a pool on a fake client without touching Docker.
func newFakePool(t *testing.T, c *fakeClient, opts ...PoolOption) *pool {
	t.Helper()
	p, err := NewPool(t.Context(), "", append([]PoolOption{WithMobyClient(c)}, opts...)...)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	return p.(*pool)
}
