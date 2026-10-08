// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ory/dockertest/v4/internal/client"
)

// MainOptions configures Main.
//
//nolint:govet // field alignment traded for readability
type MainOptions struct {
	// Scope identifies the project. It is required and must be identical
	// across all test packages of the project so that a later run can recover
	// resources that an earlier, abandoned run of the same project left
	// behind. Runs with different scopes never touch each other's resources.
	Scope string

	// StateDir is a local directory shared by all processes that should
	// recover each other's resources. It holds per-run lock and manifest
	// files but never credentials. Defaults to os.UserCacheDir()/dockertest/v4.
	StateDir string

	// CleanupTimeout bounds the whole shutdown: the optional Cleanup callback,
	// removal of containers, networks, and images, and recovery bookkeeping.
	// Defaults to 60 seconds; negative values are rejected.
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
)

// report writes a dockertest-prefixed line to w; output is best effort.
func report(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...) //nolint:errcheck // stderr reporting is best effort
}

func (o MainOptions) resolve() (MainOptions, error) {
	if o.Scope == "" {
		return o, fmt.Errorf("%w: MainOptions.Scope is required", ErrInvalidOption)
	}
	if o.CleanupTimeout < 0 {
		return o, fmt.Errorf("%w: MainOptions.CleanupTimeout must not be negative, got %v", ErrInvalidOption, o.CleanupTimeout)
	}
	if o.CleanupTimeout == 0 {
		o.CleanupTimeout = defaultCleanupTimeout
	}
	if o.StateDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return o, fmt.Errorf("%w: resolving default state directory: %w", ErrInvalidOption, err)
		}
		o.StateDir = filepath.Join(dir, "dockertest", "v4")
	}
	return o, nil
}

// Main runs the tests of a package and guarantees cleanup of every Docker
// resource the package created, even when a test hangs or the process is
// interrupted. Use it from TestMain:
//
//	func TestMain(m *testing.M) {
//		dockertest.Main(context.Background(), m, dockertest.MainOptions{
//			Scope: "ory/my-project",
//		})
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
// resources. Invalid options, a repeated Main, or a failure to establish the
// run lock exit with status 1 before any test runs.
//
// Main also recovers containers, networks, and non-retained build images left
// behind by earlier runs of the same Scope on this host that did not shut
// down, for example after a kill or a power loss. Recovery requires the same
// StateDir and only covers daemons the new run connects to.
func Main(ctx context.Context, m *testing.M, opts MainOptions) {
	runMain(ctx, m.Run, opts, os.Stderr, os.Exit)
}

// runMain is Main with the test runner, output, and exit injected for tests.
func runMain(ctx context.Context, run func() int, opts MainOptions, stderr io.Writer, exit func(int)) {
	opts, err := opts.resolve()
	if err != nil {
		report(stderr, "dockertest: %v\n", err)
		exit(1)
		return
	}
	o := owner
	state, err := o.attachMain(opts)
	if err != nil {
		report(stderr, "dockertest: %v\n", err)
		exit(1)
		return
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	var once sync.Once
	finish := func(code int, sig os.Signal) {
		once.Do(func() {
			signal.Stop(sigs)
			o.shutdownProcess(ctx, opts, state, sig, stderr)
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

// attachMain installs Main into the owner: it takes the run lock, publishes
// the manifest, and arranges recovery for every daemon the run connects to.
func (o *processOwner) attachMain(opts MainOptions) (*runState, error) {
	state, err := openRunState(opts.StateDir, opts.Scope, o.hostID, o.runID)
	if err != nil {
		return nil, err
	}
	err = o.install(opts.Scope, func(ctx context.Context, daemonID string, c client.DockerClient) error {
		recoverAbandoned(ctx, opts.StateDir, opts.Scope, o.hostID, daemonID, o.runID, c, o.warn)
		return state.addDaemon(daemonID)
	})
	if err != nil {
		return nil, errors.Join(err, state.close(true))
	}
	return state, nil
}

// shutdownProcess runs the whole shutdown under the cleanup deadline: stop
// admitting resources, drain in-flight operations, run the package Cleanup,
// remove Docker resources, and release the run record. The deadline is also
// enforced with a timer so that a callback ignoring its context cannot hold
// the process; in that case the ownership record is kept for recovery.
func (o *processOwner) shutdownProcess(ctx context.Context, opts MainOptions, state *runState, sig os.Signal, stderr io.Writer) {
	timeout := opts.CleanupTimeout
	if sig == syscall.SIGTERM && runtime.GOOS == "windows" {
		timeout = min(timeout, windowsConsoleCloseTimeout)
	}
	if sig != nil {
		report(stderr, "dockertest: received %v, cleaning up\n", sig)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		drain := o.beginShutdown()
		var errs []error
		drainErr := drain(cleanupCtx)
		errs = append(errs, drainErr)
		if opts.Cleanup != nil {
			if err := opts.Cleanup(cleanupCtx); err != nil {
				errs = append(errs, fmt.Errorf("cleanup callback: %w", err))
			}
		}
		dockerErr := o.cleanup(cleanupCtx)
		errs = append(errs, dockerErr)
		leftovers := o.leftovers()
		errs = append(errs, o.closeClients())
		// The record is only dropped when every Docker resource of this run
		// is known to be gone; a failing callback does not keep it.
		clean := len(leftovers) == 0 && drainErr == nil && dockerErr == nil
		errs = append(errs, state.close(clean))
		if err := errors.Join(errs...); err != nil {
			report(stderr, "dockertest: cleanup failed: %v\n", err)
		}
		for _, id := range leftovers {
			report(stderr, "dockertest: leftover %s\n", id)
		}
	}()

	select {
	case <-done:
	case <-time.After(timeout + shutdownGrace):
		report(stderr, "dockertest: cleanup did not finish within %v; remaining resources are recorded for recovery\n", timeout)
	}
}
