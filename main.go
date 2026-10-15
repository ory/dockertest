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

// TestMainOption configures TestMain and Wrap.
type TestMainOption func(*mainOptions)

// WithShutdownTimeout sets the deadline of the whole shutdown, including the
// teardown set with WithTeardown. Shutdown ends shortly after it even if a
// teardown or client ignores its context. Defaults to 60 seconds; negative
// values are rejected.
func WithShutdownTimeout(d time.Duration) TestMainOption {
	return func(o *mainOptions) { o.timeout = d }
}

// WithTeardown sets a package teardown that runs once, before Docker resources
// are removed, under the shutdown deadline. Its error is reported but does not
// change the exit status.
//
// It is not needed for pools, containers, and networks created through
// dockertest: shutdown removes them and closes the Docker clients pools
// created. Use it only for state dockertest does not own.
func WithTeardown(fn func(context.Context) error) TestMainOption {
	return func(o *mainOptions) { o.teardown = fn }
}

//nolint:govet // field alignment traded for readability
type mainOptions struct {
	timeout  time.Duration
	teardown func(context.Context) error
}

const (
	defaultCleanupTimeout = 60 * time.Second
	// windowsConsoleCloseTimeout caps cleanup when Windows closes the console:
	// the OS terminates the process shortly after, so a longer budget is moot.
	windowsConsoleCloseTimeout = 4 * time.Second
	// shutdownGrace is how much longer than the shutdown timeout shutdown
	// waits for a teardown that ignores its context before exiting anyway.
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

func resolve(opts []TestMainOption) (mainOptions, error) {
	o := mainOptions{timeout: defaultCleanupTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	if o.timeout < 0 {
		return o, fmt.Errorf("%w: WithShutdownTimeout must not be negative, got %v", ErrInvalidOption, o.timeout)
	}
	if o.timeout == 0 {
		o.timeout = defaultCleanupTimeout
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

// TestMain runs the tests of a package and cleans up its owned Docker
// resources, even when a test hangs or the process is interrupted. Images,
// builder cache, named volumes, and pre-existing unlabeled resources are
// outside automatic cleanup. Use it from the package's TestMain:
//
//	func TestMain(m *testing.M) {
//		dockertest.TestMain(context.Background(), m)
//	}
//
// TestMain owns process exit and never returns. It calls m.Run on the calling
// goroutine and exits with its status after cleanup; cleanup failures are
// printed to stderr but do not change the status. On SIGINT or SIGTERM,
// shutdown starts even if m.Run is stuck, and the process exits with 130 or
// 143 after cleanup. After the first signal the default signal behavior is
// restored, so a second signal terminates the process immediately.
//
// Pools may be constructed before TestMain as long as they have created no
// Docker resources. Invalid options or a repeated TestMain or Wrap exit with
// status 1 before any test runs.
//
// Containers and networks are labeled with an expiry derived from
// -test.timeout: once it has passed, go test has killed the process. At
// shutdown, TestMain also removes expired resources that earlier runs left
// behind on the daemons it used, for example after a kill or a power loss.
// With -test.timeout=0, -test.bench, or -test.fuzz resources get no expiry and
// are never removed by other runs.
//
// Use Wrap to compose with other TestMain helpers or to keep control of the
// exit status.
func TestMain(ctx context.Context, m *testing.M, opts ...TestMainOption) {
	os.Exit(Wrap(ctx, m.Run, opts...).Run())
}

// RunFunc runs tests and returns their exit code. It satisfies
// goleak.TestingM.
type RunFunc func() int

// Run calls f.
func (f RunFunc) Run() int { return f() }

// Wrap returns run wrapped in the cleanup of TestMain, for a TestMain that
// composes with other helpers or keeps control of its exit status:
//
//	func TestMain(m *testing.M) {
//		goleak.VerifyTestMain(dockertest.Wrap(context.Background(), m.Run))
//	}
//
// The returned Run installs the cleanup, calls run, shuts down as TestMain
// does, and returns the status of run. Invalid options, a repeated TestMain or
// Wrap, or resources created earlier are printed to stderr and return 1
// without calling run. Cleanup failures are printed but do not change the
// status. Before Run returns, signal handling has stopped and the Docker
// clients pools created are closed, so leak checks after it see no dockertest
// goroutines. Signals and expiry labels behave as under TestMain.
func Wrap(ctx context.Context, run func() int, opts ...TestMainOption) RunFunc {
	return func() int {
		return runMain(ctx, run, opts, testTimeout(), os.Stderr, os.Exit)
	}
}

// testTimeout returns -test.timeout, or zero when it does not bound the
// lifetime of the process.
func testTimeout() time.Duration {
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
	if value("test.bench") != "" || value("test.fuzz") != "" {
		return 0
	}
	timeout, _ := time.ParseDuration(value("test.timeout")) //nolint:errcheck // an unparsable timeout means no expiry
	return timeout
}

// runMain is Wrap with the test timeout, output, and signal exit injected for
// tests.
func runMain(ctx context.Context, run func() int, opts []TestMainOption, testTimeout time.Duration, stderr io.Writer, exit func(int)) int {
	cleanup, err := setup(ctx, opts, testTimeout, stderr, exit)
	if err != nil {
		report(stderr, "%v", err)
		return 1
	}
	code := run()
	if err := cleanup(); err != nil {
		report(stderr, "cleanup failed: %v", err)
	}
	return code
}

// setup installs the process cleanup and returns the shutdown. The shutdown
// rejects new operations, drains in-flight ones, runs the teardown, removes the
// owned containers and networks, closes the Docker clients that pools created,
// and returns all failures, including a missed deadline. It runs once however
// often it is called, and stops signal handling before it starts. On SIGINT or
// SIGTERM before it is called, the shutdown runs, failures are printed, and
// exit is called with 130 or 143. The test timeout determines the expiry label.
func setup(ctx context.Context, opts []TestMainOption, testTimeout time.Duration, stderr io.Writer, exit func(int)) (func() error, error) {
	cfg, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	o := owner
	if err := o.install(expiry(time.Now(), testTimeout, cfg.timeout)); err != nil {
		return nil, err
	}

	var (
		once   sync.Once
		result error
	)
	shutdown := func(sig os.Signal) error {
		once.Do(func() { result = o.shutdownProcess(ctx, cfg, sig) })
		return result
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	stop := make(chan struct{})
	watcher := make(chan struct{})
	go func() {
		defer close(watcher)
		var sig os.Signal
		select {
		case sig = <-sigs:
		case <-stop:
			// cleanup stops notifications before closing stop, so a signal
			// that arrived earlier is buffered and still wins.
			select {
			case sig = <-sigs:
			default:
				return
			}
		}
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
// admitting resources, drain in-flight operations, run the teardown, and
// remove Docker resources. The deadline is also enforced with a timer so that
// a teardown ignoring its context cannot hold the process.
func (o *processOwner) shutdownProcess(ctx context.Context, cfg mainOptions, sig os.Signal) error {
	timeout := cfg.timeout
	if sig == syscall.SIGTERM && runtime.GOOS == "windows" {
		timeout = min(timeout, windowsConsoleCloseTimeout)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		drain := o.beginShutdown()
		errs := []error{drain(cleanupCtx)}
		if cfg.teardown != nil {
			if err := cfg.teardown(cleanupCtx); err != nil {
				errs = append(errs, fmt.Errorf("teardown: %w", err))
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
