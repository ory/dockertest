// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
)

// runMainForTest runs runMain in-process with an injected exit and returns
// the exit code and stderr.
func runMainForTest(ctx context.Context, t *testing.T, run func() int, opts MainOptions) (code int, stderr string) {
	t.Helper()
	_, out := testOwner(t)
	code = -1
	runMain(ctx, run, opts, 0, out, func(c int) { code = c })
	return code, out.String()
}

func TestMainRejectsNegativeCleanupTimeout(t *testing.T) {
	ran := false
	code, stderr := runMainForTest(t.Context(), t, func() int { ran = true; return 0 }, MainOptions{CleanupTimeout: -time.Second})
	if want := "must not be negative"; code != 1 || !strings.Contains(stderr, want) {
		t.Fatalf("exit = %d, stderr = %q; want 1 and %q", code, stderr, want)
	}
	if ran {
		t.Fatal("tests ran despite invalid options")
	}
}

func TestMainResolvesDefaults(t *testing.T) {
	opts, err := MainOptions{}.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if opts.CleanupTimeout != defaultCleanupTimeout {
		t.Fatalf("resolved options = %+v", opts)
	}
}

func TestMainRejectsRepeatedInstallAndEarlyResources(t *testing.T) {
	t.Run("repeated", func(t *testing.T) {
		o, stderr := testOwner(t)
		code := -1
		runMain(t.Context(), func() int { return 0 }, MainOptions{}, 0, stderr, func(c int) { code = c })
		if code != 0 {
			t.Fatalf("first run exit = %d, stderr = %s", code, stderr.String())
		}
		// Main must refuse to run twice in the same process even though the
		// first run has shut down.
		runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, MainOptions{}, 0, stderr, func(c int) { code = c })
		if code != 1 || !strings.Contains(stderr.String(), "already installed") {
			t.Fatalf("second run exit = %d, stderr = %s", code, stderr.String())
		}
		_ = o
	})

	t.Run("resources before Main", func(t *testing.T) {
		_, stderr := testOwner(t)
		p := newFakePool(t, newFakeClient())
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		code := -1
		runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, MainOptions{}, 0, stderr, func(c int) { code = c })
		if code != 1 || !strings.Contains(stderr.String(), "created before Main") {
			t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
		}
	})
}

func TestMainRunsCleanupAndPreservesStatus(t *testing.T) {
	cleanupErr := errors.New("teardown failed")
	var order []string
	c := newFakeClient()
	var p *pool
	code, stderr := runMainForTest(t.Context(), t, func() int {
		p = newFakePool(t, c)
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		order = append(order, "tests")
		return 7
	}, MainOptions{Cleanup: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("cleanup callback context has no deadline")
		}
		order = append(order, "cleanup")
		if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
			t.Errorf("containers removed before the cleanup callback: %q", got)
		}
		return cleanupErr
	}})

	if code != 7 {
		t.Fatalf("exit = %d, want the test status 7", code)
	}
	if strings.Join(order, ",") != "tests,cleanup" {
		t.Fatalf("order = %v", order)
	}
	if !strings.Contains(stderr, "teardown failed") {
		t.Fatalf("stderr = %q, want the cleanup callback error reported", stderr)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
		t.Fatalf("ContainerRemove calls = %q, want one", got)
	}
	if len(c.callsMatching("ContainerList")) != 1 || len(c.callsMatching("ImageRemove")) != 0 {
		t.Fatalf("calls = %q, want one reconciliation sweep", c.calls)
	}
	if _, err := p.Run(t.Context(), "alpine"); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run() after Main error = %v, want ErrShuttingDown", err)
	}
}

func TestMainReportsLeftovers(t *testing.T) {
	c := newFakeClient()
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return errors.New("busy") }
	code, stderr := runMainForTest(t.Context(), t, func() int {
		p := newFakePool(t, c)
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		return 0
	}, MainOptions{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (cleanup failures are warnings)", code)
	}
	if !strings.Contains(stderr, "leftover container container-1") || !strings.Contains(stderr, "busy") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMainEnforcesDeadlineOnStuckCleanup(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	start := time.Now()
	code, stderr := runMainForTest(t.Context(), t, func() int { return 0 }, MainOptions{
		CleanupTimeout: 50 * time.Millisecond,
		Cleanup:        func(context.Context) error { <-block; return nil },
	})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "did not finish") {
		t.Fatalf("stderr = %q, want deadline report", stderr)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("shutdown took %v, want it bounded by the deadline", elapsed)
	}
}

func TestMainSweepsExpiredOnShutdown(t *testing.T) {
	c := newFakeClient()
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		return listedContainers(opts,
			container.Summary{ID: "expired", State: container.StateExited, Labels: map[string]string{labelManaged: "true", labelRun: "dead", labelExpires: "1"}},
			container.Summary{ID: "active", State: container.StateRunning, Labels: map[string]string{labelManaged: "true", labelRun: "alive", labelExpires: "99999999999"}},
		), nil
	}
	code, stderr := runMainForTest(t.Context(), t, func() int {
		p := newFakePool(t, c)
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
			t.Errorf("containers removed before shutdown: %q", got)
		}
		return 0
	}, MainOptions{})
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"ContainerRemove container-1 force=true volumes=true canceled=false",
		"ContainerRemove expired force=true volumes=true canceled=false",
	}
	if got := c.callsMatching("ContainerRemove"); !slices.Equal(got, want) {
		t.Fatalf("ContainerRemove calls = %q\nwant                    %q", got, want)
	}
}

func TestMainStampsExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	if got, want := expiry(now, 10*time.Minute, time.Minute), "1721"; got != want {
		t.Fatalf("expiry() = %q, want %q (now + test timeout + cleanup timeout + grace + margin)", got, want)
	}
	if got := expiry(now, 0, time.Minute); got != "" {
		t.Fatalf("expiry() with disabled test timeout = %q, want none", got)
	}

	for _, tc := range []struct {
		name        string
		testTimeout time.Duration
		want        bool
	}{
		{name: "timeout", testTimeout: 10 * time.Minute, want: true},
		{name: "no timeout", testTimeout: 0, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr := testOwner(t)
			c := newFakeClient()
			var labels map[string]string
			c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
				labels = opts.Config.Labels
				return "c1", nil
			}
			runMain(t.Context(), func() int {
				if _, err := newFakePool(t, c).Run(t.Context(), "alpine", WithoutReuse()); err != nil {
					t.Fatal(err)
				}
				return 0
			}, MainOptions{}, tc.testTimeout, stderr, func(int) {})
			if _, ok := labels[labelExpires]; ok != tc.want {
				t.Fatalf("labels = %v, want expires label: %t", labels, tc.want)
			}
		})
	}
}
