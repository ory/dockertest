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

// MainOptions configures Main and Setup.
//
//nolint:govet // field alignment traded for readability
type MainOptions struct {
	// CleanupTimeout is the deadline of the whole shutdown, including the
	// Cleanup callback. Shutdown ends shortly after it even if a callback or
	// client ignores its context. Defaults to 60 seconds; negative values are
	// rejected.
	CleanupTimeout time.Duration

	// Cleanup is an optional package teardown that runs once, before Docker
	// resources are removed, under the same deadline. Its error is part of
	// the cleanup error; under Main it does not change the exit status.
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
	// starting the test timer, and clock skew between the hosts of runs that
	// share a daemon: the expiry is compared against the sweeping host's clock.
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
// -test.timeout=0, -test.bench, or -test.fuzz resources get no expiry and are
// never removed by other runs.
//
// Use Setup instead to keep control of m.Run and the exit status.
func Main(ctx context.Context, m *testing.M, opts MainOptions) {
	if !flag.Parsed() {
		flag.Parse()
	}
	value := func(name string) string {
		if f := flag.Lookup(name); f != nil {
			return f.Value.String()
		}
		return ""
	}
	// Benchmarks and fuzzing run after the test alarm has stopped, so
	// -test.timeout only bounds the lifetime of a process that runs neither.
	var timeout time.Duration
	if value("test.bench") == "" && value("test.fuzz") == "" {
		timeout, _ = time.ParseDuration(value("test.timeout")) //nolint:errcheck // an unparsable timeout means no expiry
	}
	runMain(ctx, m.Run, opts, timeout, os.Stderr, os.Exit)
}

// runMain is Main with the test runner, test timeout, output, and exit
// injected for tests.
func runMain(ctx context.Context, run func() int, opts MainOptions, testTimeout time.Duration, stderr io.Writer, exit func(int)) {
	cleanup, err := setup(ctx, opts, testTimeout, stderr, exit)
	if err != nil {
		report(stderr, "%v", err)
		exit(1)
		return
	}
	code := run()
	if err := cleanup(); err != nil {
		report(stderr, "cleanup failed: %v", err)
	}
	exit(code)
}

// Setup installs dockertest's process cleanup for a custom TestMain that keeps
// control of m.Run, its own teardown, leak checks, and the exit status:
//
//	func TestMain(m *testing.M) {
//		cleanup, err := dockertest.Setup(context.Background(), dockertest.MainOptions{})
//		if err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			os.Exit(1)
//		}
//		code := m.Run()
//		if err := cleanup(); err != nil {
//			fmt.Fprintln(os.Stderr, err)
//		}
//		os.Exit(code)
//	}
//
// Call Setup before any Docker resource is created. Invalid options, a
// repeated Setup or Main, or resources created earlier return an error
// wrapping ErrInvalidOption.
//
// The returned cleanup runs the same shutdown as Main under the same deadline:
// it rejects new operations, drains in-flight ones, runs MainOptions.Cleanup,
// removes the owned containers and networks, and closes the Docker clients that
// pools created. It returns all failures, including a missed deadline, and
// never exits. Before it returns, Setup's signal handling has stopped, so leak
// checks such as goleak.Find run after it see no dockertest goroutines. It is
// safe to call repeatedly and concurrently; the shutdown runs once and every
// caller receives its result. Call it before os.Exit.
//
// On SIGINT or SIGTERM before cleanup is called, Setup shuts down like Main,
// prints failures to stderr, and exits with 130 or 143; a second signal
// terminates immediately.
//
// Unlike Main, Setup does not read testing flags, so resources get no expiry
// label and are never removed by other runs: leftovers of a crash or SIGKILL
// remain. At cleanup, Setup removes resources of this run whose creation
// response was lost and expired resources that earlier Main runs left behind.
func Setup(ctx context.Context, opts MainOptions) (cleanup func() error, err error) {
	return setup(ctx, opts, 0, os.Stderr, os.Exit)
}

// setup is Setup with the test timeout, which determines the expiry label,
// output, and exit injected.
func setup(ctx context.Context, opts MainOptions, testTimeout time.Duration, stderr io.Writer, exit func(int)) (func() error, error) {
	opts, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	o := owner
	if err := o.install(expiry(time.Now(), testTimeout, opts.CleanupTimeout)); err != nil {
		return nil, err
	}

	var (
		once   sync.Once
		result error
	)
	shutdown := func(sig os.Signal) error {
		once.Do(func() { result = o.shutdownProcess(ctx, opts, sig) })
		return result
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	stop := make(chan struct{})
	watcher := make(chan struct{})
	go func() {
		defer close(watcher)
		select {
		case sig := <-sigs:
			// Restore the default behavior so that a second signal terminates
			// the process immediately.
			signal.Stop(sigs)
			report(stderr, "received %v, cleaning up", sig)
			if err := shutdown(sig); err != nil {
				report(stderr, "cleanup failed: %v", err)
			}
			code := 130
			if sig == syscall.SIGTERM {
				code = 143
			}
			exit(code)
		case <-stop:
		}
	}()

	return sync.OnceValue(func() error {
		signal.Stop(sigs)
		close(stop)
		// A signal shutdown that won the race exits the process here.
		<-watcher
		return shutdown(nil)
	}), nil
}

// shutdownProcess runs the whole shutdown under the cleanup deadline: stop
// admitting resources, drain in-flight operations, run the package Cleanup,
// and remove Docker resources. The deadline is also enforced with a timer so
// that a callback ignoring its context cannot hold the process.
func (o *processOwner) shutdownProcess(ctx context.Context, opts MainOptions, sig os.Signal) error {
	timeout := opts.CleanupTimeout
	if sig == syscall.SIGTERM && runtime.GOOS == "windows" {
		timeout = min(timeout, windowsConsoleCloseTimeout)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		drain := o.beginShutdown()
		errs := []error{drain(cleanupCtx)}
		if opts.Cleanup != nil {
			if err := opts.Cleanup(cleanupCtx); err != nil {
				errs = append(errs, fmt.Errorf("cleanup callback: %w", err))
			}
		}
		errs = append(errs, o.cleanup(cleanupCtx, time.Now()))
		for _, id := range o.leftovers() {
			errs = append(errs, fmt.Errorf("leftover %s", id))
		}
		errs = append(errs, o.closeClients())
		result <- errors.Join(errs...)
	}()

	select {
	case err := <-result:
		return err
	case <-time.After(timeout + shutdownGrace):
		return fmt.Errorf("cleanup did not finish within %v: %w", timeout, context.DeadlineExceeded)
	}
}
