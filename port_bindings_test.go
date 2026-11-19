// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

func TestRunFillsEmptyPortBindings(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	var got network.PortMap
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		got = opts.HostConfig.PortBindings
		return "c1", nil
	}
	p := newFakePool(t, c)
	random, fixed := network.MustParsePort("8080/tcp"), network.MustParsePort("9090/tcp")
	bindings := network.PortMap{random: {}, fixed: {{HostPort: "19090"}}}
	if _, err := p.Run(t.Context(), "alpine", WithPortBindings(bindings), WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := network.PortMap{random: {{}}, fixed: {{HostPort: "19090"}}}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("PortBindings = %v, want %v", got, want)
	}
	if len(bindings[random]) != 0 {
		t.Fatalf("caller bindings modified: %v", bindings)
	}
}
