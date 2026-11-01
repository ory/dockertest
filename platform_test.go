// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestRunWithPlatform(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	var got []*ocispec.Platform
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		got = append(got, opts.Platform)
		return fmt.Sprintf("c%d", len(got)), nil
	}
	p := newFakePool(t, c)
	amd64, err := p.Run(t.Context(), "alpine", WithPlatform("linux/amd64"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	arm64, err := p.Run(t.Context(), "alpine", WithPlatform("linux/arm64/v8"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if amd64.ID() == arm64.ID() {
		t.Fatal("containers for different platforms were reused for each other")
	}
	want := []*ocispec.Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64", Variant: "v8"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create platforms = %v, want %v", got, want)
	}

	for _, bad := range []string{"linux", "linux/", "a/b/c/d"} {
		if _, err := p.Run(t.Context(), "alpine", WithPlatform(bad)); !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("WithPlatform(%q) error = %v, want ErrInvalidOption", bad, err)
		}
	}
}

func TestBuildAndRunWithPlatform(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	built := c.buildStream("sha256:img")
	var buildPlatforms []ocispec.Platform
	c.imageBuild = func(ctx context.Context, opts mobyclient.ImageBuildOptions) (io.ReadCloser, error) {
		buildPlatforms = opts.Platforms
		return built(ctx, opts)
	}
	var createPlatform *ocispec.Platform
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		createPlatform = opts.Platform
		return "c1", nil
	}
	p := newFakePool(t, c)
	if _, err := p.BuildAndRun(t.Context(), "img", &BuildOptions{ContextDir: t.TempDir(), Platform: "linux/amd64"}); err != nil {
		t.Fatalf("BuildAndRun() error = %v", err)
	}
	want := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	if !reflect.DeepEqual(buildPlatforms, []ocispec.Platform{want}) {
		t.Fatalf("build platforms = %v, want [%v]", buildPlatforms, want)
	}
	if !reflect.DeepEqual(createPlatform, &want) {
		t.Fatalf("create platform = %v, want %v", createPlatform, want)
	}
}
