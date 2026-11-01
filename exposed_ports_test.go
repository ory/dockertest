// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

func TestRunWithExposedPorts(t *testing.T) {
	testOwner(t)
	c := newFakeClient()
	var got network.PortSet
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		got = opts.Config.ExposedPorts
		return "c1", nil
	}
	p := newFakePool(t, c)
	if _, err := p.Run(t.Context(), "alpine", WithExposedPorts("4566", "53/udp"), WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := network.PortSet{network.MustParsePort("4566/tcp"): {}, network.MustParsePort("53/udp"): {}}
	if !maps.Equal(got, want) {
		t.Fatalf("ExposedPorts = %v, want %v", got, want)
	}

	for _, port := range []string{"not-a-port", "5432/tcpp"} {
		if _, err := p.Run(t.Context(), "alpine", WithExposedPorts(port), WithoutReuse()); !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("Run() with port %q: error = %v, want ErrInvalidOption", port, err)
		}
	}
}
