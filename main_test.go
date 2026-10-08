// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	mobyclient "github.com/moby/moby/client"
)

// runMainForTest runs runMain in-process with an injected exit and returns
// the exit code and stderr.
func runMainForTest(ctx context.Context, t *testing.T, run func() int, opts MainOptions) (code int, stderr string) {
	t.Helper()
	if opts.StateDir == "" {
		opts.StateDir = t.TempDir()
	}
	_, out := testOwner(t)
	code = -1
	runMain(ctx, run, opts, out, func(c int) { code = c })
	return code, out.String()
}

func TestMainRejectsInvalidOptions(t *testing.T) {
	cases := []struct {
		name string
		opts MainOptions
		want string
	}{
		{name: "missing scope", opts: MainOptions{}, want: "Scope is required"},
		{name: "negative timeout", opts: MainOptions{Scope: "s", CleanupTimeout: -time.Second}, want: "must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			code, stderr := runMainForTest(t.Context(), t, func() int { ran = true; return 0 }, tc.opts)
			if code != 1 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit = %d, stderr = %q; want 1 and %q", code, stderr, tc.want)
			}
			if ran {
				t.Fatal("tests ran despite invalid options")
			}
		})
	}
}

func TestMainResolvesDefaults(t *testing.T) {
	opts, err := MainOptions{Scope: "s"}.resolve()
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no user cache dir")
	}
	if opts.CleanupTimeout != defaultCleanupTimeout || !strings.HasPrefix(opts.StateDir, cache) || !strings.HasSuffix(opts.StateDir, "v4") {
		t.Fatalf("resolved options = %+v", opts)
	}
}

func TestMainRejectsRepeatedInstallAndEarlyResources(t *testing.T) {
	t.Run("repeated", func(t *testing.T) {
		o, stderr := testOwner(t)
		dir := t.TempDir()
		code := -1
		runMain(t.Context(), func() int { return 0 }, MainOptions{Scope: "s", StateDir: dir}, stderr, func(c int) { code = c })
		if code != 0 {
			t.Fatalf("first run exit = %d, stderr = %s", code, stderr.String())
		}
		// Main must refuse to run twice in the same process even though the
		// first run's lock was released.
		runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, MainOptions{Scope: "s", StateDir: dir}, stderr, func(c int) { code = c })
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
		runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, MainOptions{Scope: "s", StateDir: t.TempDir()}, stderr, func(c int) { code = c })
		if code != 1 || !strings.Contains(stderr.String(), "created before Main") {
			t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
		}
	})

	t.Run("unlockable state directory", func(t *testing.T) {
		file := t.TempDir() + "/file"
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stderr := runMainForTest(t.Context(), t, func() int { t.Fatal("tests ran"); return 0 }, MainOptions{Scope: "s", StateDir: file + "/dir"})
		if code != 1 || !strings.Contains(stderr, "state directory") {
			t.Fatalf("exit = %d, stderr = %q", code, stderr)
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
	}, MainOptions{Scope: "s", Cleanup: func(ctx context.Context) error {
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
	if len(c.callsMatching("ContainerList")) != 1 || len(c.callsMatching("ImageList")) != 1 {
		t.Fatalf("calls = %q, want one reconciliation sweep", c.calls)
	}
	if _, err := p.Run(t.Context(), "alpine"); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run() after Main error = %v, want ErrShuttingDown", err)
	}
}

func TestMainReportsLeftoversAndKeepsRecord(t *testing.T) {
	c := newFakeClient()
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return errors.New("busy") }
	dir := t.TempDir()
	var runID string
	code, stderr := runMainForTest(t.Context(), t, func() int {
		runID = owner.runID
		p := newFakePool(t, c)
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		return 0
	}, MainOptions{Scope: "s", StateDir: dir})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (cleanup failures are warnings)", code)
	}
	if !strings.Contains(stderr, "leftover container container-1") || !strings.Contains(stderr, "busy") {
		t.Fatalf("stderr = %q", stderr)
	}
	m, ok := readTestManifest(t, dir, runID)
	if !ok || len(m.Daemons) != 1 || m.Daemons[0] != "daemon-1" {
		t.Fatalf("manifest = %+v, %t; want it kept with the daemon for recovery", m, ok)
	}
}

func TestMainEnforcesDeadlineOnStuckCleanup(t *testing.T) {
	dir := t.TempDir()
	var runID string
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	start := time.Now()
	code, stderr := runMainForTest(t.Context(), t, func() int { runID = owner.runID; return 0 }, MainOptions{
		Scope: "s", StateDir: dir, CleanupTimeout: 50 * time.Millisecond,
		Cleanup: func(context.Context) error { <-block; return nil },
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
	if _, ok := readTestManifest(t, dir, runID); !ok {
		t.Fatal("manifest removed although cleanup did not finish")
	}
}

func TestMainRecoversAbandonedRunOnFirstDaemonUse(t *testing.T) {
	dir := t.TempDir()
	c := newFakeClient()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "host-test", RunID: "dead", Daemons: []string{"daemon-1"}})
	abandonedContainers(c, "host-test", "dead")

	code, stderr := runMainForTest(t.Context(), t, func() int {
		p := newFakePool(t, c)
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		calls := c.callsMatching("ContainerRemove")
		if len(calls) != 1 || !strings.HasPrefix(calls[0], "ContainerRemove container-of-dead") {
			t.Errorf("recovery did not run before the first creation: %q", c.calls)
		}
		return 0
	}, MainOptions{Scope: "s", StateDir: dir})
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if _, ok := readTestManifest(t, dir, "dead"); ok {
		t.Fatal("recovered manifest still present")
	}
}
