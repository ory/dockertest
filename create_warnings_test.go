// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestRunReportsCreateWarnings(t *testing.T) {
	_, stderr := testOwner(t)
	c := newFakeClient()
	c.createWarnings = []string{"empty port bindings are deprecated", "second warning"}
	p := newFakePool(t, c)
	// WithContainerConfig may replace the image; the warning names the one used.
	replaceImage := WithContainerConfig(func(c *container.Config) { c.Image = "busybox:1" })
	if _, err := p.Run(t.Context(), "alpine", replaceImage, WithoutReuse()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := "dockertest: container container-1 from busybox:1: empty port bindings are deprecated\n" +
		"dockertest: container container-1 from busybox:1: second warning\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
