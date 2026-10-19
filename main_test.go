// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"go.uber.org/goleak"
)

// runMainForTest runs runMain in-process, fails the test if it exits, and
// returns the exit code and stderr.
func runMainForTest(ctx context.Context, t *testing.T, run func() int, opts ...TestMainOption) (code int, stderr string) {
	t.Helper()
	_, out := testOwner(t)
	code = runMain(ctx, run, opts, 0, out, func(c int) { t.Errorf("exited with %d", c) })
	return code, out.String()
}

func TestMainRejectsNegativeShutdownTimeout(t *testing.T) {
	ran := false
	code, stderr := runMainForTest(t.Context(), t, func() int { ran = true; return 0 }, WithShutdownTimeout(-time.Second))
	if want := "must not be negative"; code != 1 || !strings.Contains(stderr, want) {
		t.Fatalf("exit = %d, stderr = %q; want 1 and %q", code, stderr, want)
	}
	if ran {
		t.Fatal("tests ran despite invalid options")
	}
}

func TestMainResolvesDefaults(t *testing.T) {
	opts, err := resolve(nil)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if opts.timeout != defaultShutdownTimeout {
		t.Fatalf("resolved options = %+v", opts)
	}
}

func TestMainRejectsRepeatedInstallAndEarlyResources(t *testing.T) {
	t.Run("repeated", func(t *testing.T) {
		o, stderr := testOwner(t)
		code := runMain(t.Context(), func() int { return 0 }, nil, 0, stderr, func(int) {})
		if code != 0 {
			t.Fatalf("first run exit = %d, stderr = %s", code, stderr.String())
		}
		// runMain must refuse to run twice in the same process even though the
		// first run has shut down.
		code = runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, nil, 0, stderr, func(int) {})
		if code != 1 || !strings.Contains(stderr.String(), "already installed") {
			t.Fatalf("second run exit = %d, stderr = %s", code, stderr.String())
		}
		_ = o
	})

	t.Run("resources before TestMain", func(t *testing.T) {
		_, stderr := testOwner(t)
		p := newFakePool(t, newFakeClient())
		if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		code := runMain(t.Context(), func() int { t.Fatal("tests ran"); return 0 }, nil, 0, stderr, func(int) {})
		if code != 1 || !strings.Contains(stderr.String(), "created before TestMain") {
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
	}, WithTeardown(func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("teardown context has no deadline")
		}
		order = append(order, "cleanup")
		if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
			t.Errorf("containers removed before the teardown: %q", got)
		}
		return cleanupErr
	}))

	if code != 7 {
		t.Fatalf("exit = %d, want the test status 7", code)
	}
	if strings.Join(order, ",") != "tests,cleanup" {
		t.Fatalf("order = %v", order)
	}
	if !strings.Contains(stderr, "teardown failed") {
		t.Fatalf("stderr = %q, want the teardown error reported", stderr)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
		t.Fatalf("ContainerRemove calls = %q, want one", got)
	}
	if len(c.callsMatching("ContainerList")) != 1 || len(c.callsMatching("ImageRemove")) != 0 {
		t.Fatalf("calls = %q, want one reconciliation sweep", c.calls)
	}
	if _, err := p.Run(t.Context(), "alpine"); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run() after TestMain error = %v, want ErrShuttingDown", err)
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
	})
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
	code, stderr := runMainForTest(t.Context(), t, func() int { return 0 },
		WithShutdownTimeout(50*time.Millisecond),
		WithTeardown(func(context.Context) error { <-block; return nil }),
	)
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
	})
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
			}, nil, tc.testTimeout, stderr, func(int) {})
			if _, ok := labels[labelExpires]; ok != tc.want {
				t.Fatalf("labels = %v, want expires label: %t", labels, tc.want)
			}
		})
	}
}

// setupForTest installs setup on an isolated owner and fails the test if
// setup tries to exit the process.
func setupForTest(t *testing.T, opts ...TestMainOption) (cleanup func() error, stderr *bytes.Buffer) {
	t.Helper()
	_, stderr = testOwner(t)
	cleanup, err := setup(t.Context(), opts, 0, stderr, func(code int) { t.Errorf("setup exited with %d", code) })
	if err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	return cleanup, stderr
}

func TestSetupRejectsInvalidInstall(t *testing.T) {
	noExit := func(code int) { t.Errorf("setup exited with %d", code) }

	t.Run("invalid options", func(t *testing.T) {
		_, stderr := testOwner(t)
		_, err := setup(t.Context(), []TestMainOption{WithShutdownTimeout(-time.Second)}, 0, stderr, noExit)
		if !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "must not be negative") {
			t.Fatalf("setup() error = %v, want ErrInvalidOption", err)
		}
	})

	t.Run("repeated setup", func(t *testing.T) {
		cleanup, stderr := setupForTest(t)
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
		_, err := setup(t.Context(), nil, 0, stderr, noExit)
		if !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "already installed") {
			t.Fatalf("setup() error = %v, want already installed", err)
		}
	})

	t.Run("setup after TestMain", func(t *testing.T) {
		_, stderr := testOwner(t)
		runMain(t.Context(), func() int { return 0 }, nil, 0, stderr, func(int) {})
		_, err := setup(t.Context(), nil, 0, stderr, noExit)
		if !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "already installed") {
			t.Fatalf("setup() error = %v, want already installed", err)
		}
	})

	t.Run("resources before setup", func(t *testing.T) {
		_, stderr := testOwner(t)
		if _, err := newFakePool(t, newFakeClient()).Run(t.Context(), "alpine", WithoutReuse()); err != nil {
			t.Fatal(err)
		}
		_, err := setup(t.Context(), nil, 0, stderr, noExit)
		if !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "created before TestMain or Wrap") {
			t.Fatalf("setup() error = %v, want created before", err)
		}
	})
}

func TestSetupCleanupRunsOnce(t *testing.T) {
	cleanupErr := errors.New("teardown failed")
	var calls atomic.Int32
	cleanup, stderr := setupForTest(t, WithTeardown(func(context.Context) error {
		calls.Add(1)
		return cleanupErr
	}))
	c := newFakeClient()
	p := newFakePool(t, c)
	if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatal(err)
	}

	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { errs[i] = cleanup() })
	}
	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("cleanup() #%d error = %v, want %v", i, err, cleanupErr)
		}
	}
	if err := cleanup(); !errors.Is(err, cleanupErr) {
		t.Fatalf("repeated cleanup() error = %v, want %v", err, cleanupErr)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("teardown ran %d times, want once", got)
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
		t.Fatalf("ContainerRemove calls = %q, want one", got)
	}
	if got := c.callsMatching("ContainerList"); len(got) != 1 {
		t.Fatalf("ContainerList calls = %q, want one reconciliation sweep", got)
	}
	if _, err := p.Run(t.Context(), "alpine"); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run() after cleanup error = %v, want ErrShuttingDown", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want errors returned rather than printed", stderr)
	}
}

func TestSetupCleanupReturnsRemovalErrors(t *testing.T) {
	cleanup, _ := setupForTest(t)
	busy := errors.New("busy")
	c := newFakeClient()
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return busy }
	if _, err := newFakePool(t, c).Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatal(err)
	}
	err := cleanup()
	if !errors.Is(err, busy) || !strings.Contains(err.Error(), "leftover container container-1") {
		t.Fatalf("cleanup() error = %v, want the removal failure and the leftover", err)
	}
}

func TestSetupCleanupReturnsDeadline(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	cleanup, _ := setupForTest(t,
		WithShutdownTimeout(50*time.Millisecond),
		WithTeardown(func(context.Context) error { <-block; return nil }),
	)
	start := time.Now()
	if err := cleanup(); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("cleanup() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cleanup took %v, want it bounded by the deadline", elapsed)
	}
}

func TestSetupCleanupStopsWatcher(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	cleanup, _ := setupForTest(t)
	if _, err := newFakePool(t, newFakeClient()).Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	goleak.VerifyNone(t, ignore)
}

func TestSetupRemovesOwnedResourcesWithoutExpiry(t *testing.T) {
	cleanup, _ := setupForTest(t)
	c := newFakeClient()
	var labels []map[string]string
	c.containerCreate = func(_ context.Context, opts mobyclient.ContainerCreateOptions) (string, error) {
		labels = append(labels, opts.Config.Labels)
		return "c1", nil
	}
	c.networkCreate = func(_ context.Context, _ string, opts mobyclient.NetworkCreateOptions) (string, error) {
		labels = append(labels, opts.Labels)
		return "n1", nil
	}
	// A container whose create response was lost: it exists on the daemon
	// with this run's label but was never tracked.
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		return listedContainers(opts, container.Summary{ID: "lost", State: container.StateRunning, Labels: map[string]string{labelManaged: "true", labelRun: "run-test"}}), nil
	}
	p := newFakePool(t, c)
	if _, err := p.Run(t.Context(), "alpine", WithoutReuse()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateNetwork(t.Context(), "net", nil); err != nil {
		t.Fatal(err)
	}

	for _, l := range labels {
		if _, ok := l[labelExpires]; ok {
			t.Fatalf("labels = %v, want no expiry without a test timeout", l)
		}
		// Another run's sweep, even far in the future, must leave it alone.
		if orphaned(l, "other-run", time.Unix(1<<40, 0)) {
			t.Fatalf("labels = %v are sweepable by another run", l)
		}
	}
	if len(labels) != 2 {
		t.Fatalf("created %d resources, want a container and a network", len(labels))
	}

	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ContainerRemove c1 force=true volumes=true canceled=false",
		"ContainerRemove lost force=true volumes=true canceled=false",
	}
	if got := c.callsMatching("ContainerRemove"); !slices.Equal(got, want) {
		t.Fatalf("ContainerRemove calls = %q\nwant                    %q", got, want)
	}
	if got := c.callsMatching("NetworkRemove"); !slices.Equal(got, []string{"NetworkRemove n1 canceled=false"}) {
		t.Fatalf("NetworkRemove calls = %q", got)
	}
}

func TestSetupClosesOwnedClients(t *testing.T) {
	cleanup, _ := setupForTest(t)
	c := newFakeClient()
	p := newFakePool(t, c)
	p.ownedClient = true
	if err := p.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.closed {
		t.Fatal("pool.Close closed the client under setup, want it kept for shutdown")
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if !c.closed {
		t.Fatal("cleanup did not close the pool's own client")
	}
}
