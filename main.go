// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

// MainOptions configures Main.
//
//nolint:govet // field alignment traded for readability
type MainOptions struct {
	// CleanupTimeout is the deadline of the whole shutdown, including the
	// Cleanup callback. Main exits shortly after it even if a callback or
	// client ignores its context; what remains is swept by a later run once
	// it expires. Defaults to 60 seconds; negative values are rejected.
	CleanupTimeout time.Duration

	// Cleanup is an optional package teardown that runs once, before Docker
	// resources are removed, under the same deadline. Its error is reported
	// but does not change the exit status.
	Cleanup func(context.Context) error
}

const (
	defaultCleanupTimeout = 60 * time.Second
	// windowsConsoleCloseTimeout caps cleanup when Windows closes the console:
	// the OS terminates the process shortly after, so a longer budget is moot.
	windowsConsoleCloseTimeout = 4 * time.Second
	// shutdownGrace is how much longer than the cleanup timeout Main waits for
	// a callback that ignores its context before exiting anyway.
	shutdownGrace = time.Second
	// expiryMargin absorbs the time between computing the expiry and m.Run
	// starting the test timer, and clock skew between host and daemon.
	expiryMargin = time.Minute
)

// report writes a dockertest-prefixed line to w; output is best effort.
func report(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, "dockertest: "+format+"\n", args...) //nolint:errcheck // stderr reporting is best effort
}

func (o MainOptions) resolve() (MainOptions, error) {
	if o.CleanupTimeout < 0 {
		return o, fmt.Errorf("%w: MainOptions.CleanupTimeout must not be negative, got %v", ErrInvalidOption, o.CleanupTimeout)
	}
	if o.CleanupTimeout == 0 {
		o.CleanupTimeout = defaultCleanupTimeout
	}
	return o, nil
}

// expiry returns the expiry label value for a run that starts at now: the
// moment by which go test has killed the process for exceeding testTimeout and
// its shutdown has ended. It is empty when the test timeout is disabled.
func expiry(now time.Time, testTimeout, cleanupTimeout time.Duration) string {
	if testTimeout <= 0 {
		return ""
	}
	return strconv.FormatInt(now.Add(testTimeout+cleanupTimeout+shutdownGrace+expiryMargin).Unix(), 10)
}

// Main runs the tests of a package and cleans up its owned Docker resources,
// even when a test hangs or the process is interrupted. Images, builder cache,
// named volumes, and pre-existing unlabeled resources are outside automatic
// cleanup. Use it from TestMain:
//
//	func TestMain(m *testing.M) {
//		dockertest.Main(context.Background(), m, dockertest.MainOptions{})
//	}
//
// Main owns process exit and never returns. It calls m.Run on the calling
// goroutine and exits with its status after cleanup; cleanup failures are
// printed to stderr but do not change the status. On SIGINT or SIGTERM,
// shutdown starts even if m.Run is stuck, and the process exits with 130 or
// 143 after cleanup. After the first signal the default signal behavior is
// restored, so a second signal terminates the process immediately.
//
// Pools may be constructed before Main as long as they have created no Docker
// resources. Invalid options or a repeated Main exit with status 1 before any
// test runs.
//
// Under Main, containers and networks are labeled with an expiry derived from
// -test.timeout: once it has passed, go test has killed the process. At
// shutdown, Main also removes expired resources that earlier runs left behind
// on the daemons it used, for example after a kill or a power loss. With
// -test.timeout=0 resources get no expiry and are never removed by other runs.
func Main(ctx context.Context, m *testing.M, opts MainOptions) {
	if !flag.Parsed() {
		flag.Parse()
	}
	var timeout time.Duration
	if f := flag.Lookup("test.timeout"); f != nil {
		if d, ok := f.Value.(flag.Getter).Get().(time.Duration); ok {
			timeout = d
		}
	}
	runMain(ctx, m.Run, opts, timeout, os.Stderr, os.Exit)
}

// runMain is Main with the test runner, test timeout, output, and exit
// injected for tests.
func runMain(ctx context.Context, run func() int, opts MainOptions, testTimeout time.Duration, stderr io.Writer, exit func(int)) {
	opts, err := opts.resolve()
	if err != nil {
		report(stderr, "%v", err)
		exit(1)
		return
	}
	o := owner
	if err := o.install(expiry(time.Now(), testTimeout, opts.CleanupTimeout)); err != nil {
		report(stderr, "%v", err)
		exit(1)
		return
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	var once sync.Once
	finish := func(code int, sig os.Signal) {
		once.Do(func() {
			signal.Stop(sigs)
			o.shutdownProcess(ctx, opts, sig, stderr)
			exit(code)
		})
	}
	go func() {
		sig := <-sigs
		code := 130
		if sig == syscall.SIGTERM {
			code = 143
		}
		finish(code, sig)
	}()

	finish(run(), nil)
}

// shutdownProcess runs the whole shutdown under the cleanup deadline: stop
// admitting resources, drain in-flight operations, run the package Cleanup,
// and remove Docker resources. The deadline is also enforced with a timer so
// that a callback ignoring its context cannot hold the process.
func (o *processOwner) shutdownProcess(ctx context.Context, opts MainOptions, sig os.Signal, stderr io.Writer) {
	timeout := opts.CleanupTimeout
	if sig == syscall.SIGTERM && runtime.GOOS == "windows" {
		timeout = min(timeout, windowsConsoleCloseTimeout)
	}
	if sig != nil {
		report(stderr, "received %v, cleaning up", sig)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		drain := o.beginShutdown()
		errs := []error{drain(cleanupCtx)}
		if opts.Cleanup != nil {
			if err := opts.Cleanup(cleanupCtx); err != nil {
				errs = append(errs, fmt.Errorf("cleanup callback: %w", err))
			}
		}
		errs = append(errs, o.cleanup(cleanupCtx, time.Now()))
		leftovers := o.leftovers()
		errs = append(errs, o.closeClients())
		if err := errors.Join(errs...); err != nil {
			report(stderr, "cleanup failed: %v", err)
		}
		for _, id := range leftovers {
			report(stderr, "leftover %s", id)
		}
	}()

	select {
	case <-done:
	case <-time.After(timeout + shutdownGrace):
		report(stderr, "cleanup did not finish within %v; remaining resources are removed by a later run once they expire", timeout)
	}
}
