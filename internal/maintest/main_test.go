// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package maintest exercises dockertest.Main and dockertest.Setup in child
// processes: the test binary re-executes itself with DOCKERTEST_MAIN_CASE set,
// and that child's TestMain hands control to dockertest.Main or, for cases
// prefixed with "setup-", runs a custom TestMain built on dockertest.Setup.
package maintest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
	"github.com/ory/dockertest/v4/internal/client"
	"go.uber.org/goleak"
)

const (
	envCase     = "DOCKERTEST_MAIN_CASE"
	envOutFile  = "DOCKERTEST_MAIN_OUT"
	readyMarker = "DOCKERTEST-READY"
	setupPrefix = "setup-"

	labelManaged = "io.ory.dockertest.managed"
	labelRun     = "io.ory.dockertest.run"
	labelExpires = "io.ory.dockertest.expires"
)

func TestMain(m *testing.M) {
	testCase := os.Getenv(envCase)
	if testCase == "" {
		os.Exit(m.Run())
	}
	opts := dockertest.MainOptions{CleanupTimeout: 20 * time.Second}
	switch strings.TrimPrefix(testCase, setupPrefix) {
	case "invalid-options":
		opts.CleanupTimeout = -time.Second
	case "cleanup-error":
		opts.Cleanup = func(context.Context) error { return errors.New("teardown failed") }
	case "stuck-cleanup":
		opts.CleanupTimeout = time.Minute
		opts.Cleanup = func(context.Context) error { select {} }
	case "signal-during-cleanup":
		opts.CleanupTimeout = time.Minute
		opts.Cleanup = func(ctx context.Context) error {
			ready()
			<-ctx.Done()
			return ctx.Err()
		}
	case "signal-race":
		opts.Cleanup = func(context.Context) error {
			fmt.Println(callbackMarker)
			close(released)
			<-cleanupCalled
			return nil
		}
	}
	if strings.HasPrefix(testCase, setupPrefix) {
		setupMain(m, opts, testCase)
		return
	}
	dockertest.Main(context.Background(), m, opts)
}

const (
	beforeRunMarker    = "DOCKERTEST-BEFORE-RUN"
	afterRunMarker     = "DOCKERTEST-AFTER-RUN"
	afterCleanupMarker = "DOCKERTEST-AFTER-CLEANUP"
	callbackMarker     = "DOCKERTEST-CLEANUP-CALLBACK"
)

var (
	// released is closed by the signal-race Cleanup callback to let the test
	// body return while the signal shutdown is still running.
	released = make(chan struct{})
	// cleanupCalled is closed right before the custom TestMain calls cleanup.
	cleanupCalled = make(chan struct{})
	// callerClients are Docker clients a test body created itself; like any
	// caller-owned client they are closed after cleanup, before the leak check.
	callerClients []io.Closer
)

// setupMain is a custom TestMain built on Setup, shaped like Keto's: it keeps
// control of m.Run, its own teardown, the goleak check, and the exit status.
func setupMain(m *testing.M, opts dockertest.MainOptions, testCase string) {
	cleanup, err := dockertest.Setup(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(beforeRunMarker)
	code := m.Run()
	fmt.Println(afterRunMarker)
	close(cleanupCalled)
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	for _, c := range callerClients {
		_ = c.Close() //nolint:errcheck // best effort
	}
	fmt.Println(afterCleanupMarker)
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	if testCase == setupPrefix+"exit-code" {
		code = 42
	}
	os.Exit(code)
}

// TestHelperProcess is the body of every child process.
func TestHelperProcess(t *testing.T) {
	testCase := os.Getenv(envCase)
	switch strings.TrimPrefix(testCase, setupPrefix) {
	case "":
		t.Skip("not a child process")
	case "pass", "cleanup-error", "exit-code", "signal-during-cleanup":
	case "fail":
		t.Fatal("intended failure")
	case "leak":
		go func() { select {} }()
	case "signal-race":
		ready()
		<-released
	case "stuck", "stuck-cleanup":
		ready()
		select {}
	case "container-stuck":
		runContainer(t)
		ready()
		select {}
	case "container-exit":
		runContainer(t)
		os.Exit(0) // simulates a run that dies without cleanup
	case "container-pass":
		runContainer(t)
	case "container-stopped-exit":
		id := runContainer(t)
		zero := 0
		if _, err := dockerClient(t).ContainerStop(t.Context(), id, mobyclient.ContainerStopOptions{Timeout: &zero}); err != nil {
			t.Fatal(err)
		}
		os.Exit(0) // simulates a run that dies without cleanup
	case "creating":
		pool := dockertest.NewPoolT(t, "", dockertest.WithMobyClient(lostCreateClient{DockerClient: callerClient(t), t: t}))
		_, _ = pool.Run(t.Context(), "alpine", dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse()) //nolint:errcheck // the interrupt fails it
		select {}
	case "lost-pass":
		pool := dockertest.NewPoolT(t, "", dockertest.WithMobyClient(lostCreateClient{DockerClient: callerClient(t), t: t, immediate: true}))
		if _, err := pool.Run(t.Context(), "alpine", dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse()); err == nil {
			t.Fatal("Run() succeeded despite the lost create response")
		}
	case "network-pass":
		pool := dockertest.NewPoolT(t, "")
		writeOut(t, pool.CreateNetworkT(t, fmt.Sprintf("dockertest-setup-%d", os.Getpid()), nil).ID())
	default:
		t.Fatalf("unknown case %q", testCase)
	}
}

// callerClient creates a Docker client owned by the test body.
func callerClient(t *testing.T) client.DockerClient {
	t.Helper()
	dc, err := client.NewMobyClient(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	callerClients = append(callerClients, dc)
	return dc
}

func ready() {
	fmt.Println(readyMarker)
}

func writeOut(t *testing.T, id string) {
	t.Helper()
	if err := os.WriteFile(os.Getenv(envOutFile), []byte(id), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runContainer(t *testing.T) string {
	t.Helper()
	pool := dockertest.NewPoolT(t, "")
	r := pool.RunT(t, "alpine", dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
	writeOut(t, r.ID())
	return r.ID()
}

// lostCreateClient is a Docker client whose ContainerCreate creates the
// container, reports it as ready, and then waits until its context is canceled
// and returns without the create response, as if the request was interrupted.
// With immediate set, it returns without the response right away.
//
//nolint:govet // field alignment traded for readability
type lostCreateClient struct {
	client.DockerClient
	t         *testing.T
	immediate bool
}

func (c lostCreateClient) ContainerCreate(ctx context.Context, opts mobyclient.ContainerCreateOptions) (mobyclient.ContainerCreateResult, error) { //nolint:gocritic // mirrors the client interface
	resp, err := c.DockerClient.ContainerCreate(ctx, opts)
	if err != nil {
		return resp, err
	}
	writeOut(c.t, resp.ID)
	if c.immediate {
		return mobyclient.ContainerCreateResult{}, errors.New("create response lost")
	}
	ready()
	<-ctx.Done()
	return mobyclient.ContainerCreateResult{}, ctx.Err()
}

// child is a running child process whose stdout and stderr are captured.
//
//nolint:govet // field alignment traded for readability
type child struct {
	cmd        *exec.Cmd
	stdout     <-chan string
	stdoutText *strings.Builder
	stderr     *strings.Builder
	errCh      <-chan string
	out        string
}

// startChild starts a child process. Like go test, it passes a test timeout,
// so the child's resources carry an expiry, here one hour in the future.
func startChild(t *testing.T, testCase string) *child {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1", "-test.timeout=1h") // #nosec G204 -- re-executes the test binary
	cmd.Env = append(os.Environ(), envCase+"="+testCase, envOutFile+"="+out)
	setupProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting child: %v", err)
	}
	c := &child{cmd: cmd, stdoutText: &strings.Builder{}, stderr: &strings.Builder{}, out: out}
	c.stdout = teeLines(stdout, c.stdoutText)
	c.errCh = teeLines(stderr, c.stderr)
	t.Cleanup(func() { _ = cmd.Process.Kill() }) //nolint:errcheck // Best effort cleanup if the test failed early
	return c
}

func teeLines(r io.Reader, sink *strings.Builder) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			sink.WriteString(scanner.Text() + "\n")
			ch <- scanner.Text()
		}
	}()
	return ch
}

// waitFor blocks until a line containing marker arrives on ch.
func waitFor(t *testing.T, ch <-chan string, marker string) {
	t.Helper()
	for line := range ch {
		if strings.Contains(line, marker) {
			return
		}
	}
	t.Fatalf("child ended before printing %q", marker)
}

// wait waits for the child to exit and returns its exit code, or -1 if it
// was killed by a signal.
func (c *child) wait(t *testing.T) int {
	t.Helper()
	for range c.stdout { // drain
	}
	for range c.errCh {
	}
	err := c.cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		t.Fatalf("waiting for child: %v", err)
	}
	return 0
}

func (c *child) containerID(t *testing.T) string {
	t.Helper()
	id, err := os.ReadFile(c.out)
	if err != nil {
		t.Fatalf("child did not report a container ID: %v", err)
	}
	return string(id)
}

func run(t *testing.T, testCase string) (code int, stderr string) {
	t.Helper()
	c := startChild(t, testCase)
	return c.wait(t), c.stderr.String()
}

func TestMainExitStatus(t *testing.T) {
	cases := []struct {
		name     string
		testCase string
		wantErr  string
		wantCode int
	}{
		{name: "passing tests exit 0", testCase: "pass", wantCode: 0},
		{name: "failing tests exit 1", testCase: "fail", wantCode: 1},
		{name: "cleanup failure is a warning", testCase: "cleanup-error", wantCode: 0, wantErr: "teardown failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := run(t, tc.testCase)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; stderr:\n%s", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.wantErr)
			}
		})
	}
}

func TestMainInvalidOptionsExits1(t *testing.T) {
	code, stderr := run(t, "invalid-options")
	if code != 1 || !strings.Contains(stderr, "must not be negative") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestMainInterruptOfStuckTest(t *testing.T) {
	c := startChild(t, "stuck")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	if !strings.Contains(c.stderr.String(), "received interrupt") {
		t.Fatalf("stderr = %q", c.stderr.String())
	}
}

func TestMainTerminateOfStuckTest(t *testing.T) {
	c := startChild(t, "stuck")
	waitFor(t, c.stdout, readyMarker)
	terminate(t, c.cmd)
	if code := c.wait(t); code != 143 {
		t.Fatalf("exit = %d, want 143; stderr:\n%s", code, c.stderr.String())
	}
}

func TestMainSecondSignalTerminatesImmediately(t *testing.T) {
	c := startChild(t, "stuck-cleanup")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	waitFor(t, c.errCh, "cleaning up")
	interrupt(t, c.cmd)
	if code := c.wait(t); code == 130 || code == 0 {
		t.Fatalf("exit = %d, want termination by the second signal", code)
	}
}

func dockerClient(t *testing.T) *mobyclient.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	dc, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("mobyclient.New() error = %v", err)
	}
	t.Cleanup(func() { dc.Close() })
	return dc
}

func assertContainerGone(t *testing.T, dc *mobyclient.Client, id string) {
	t.Helper()
	_, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
	if !errdefs.IsNotFound(err) {
		t.Fatalf("ContainerInspect(%s) error = %v, want not found", id, err)
	}
}

// removeAfter force-removes the container when the test ends; it belongs to a
// run that died, so nothing else removes it before it expires.
func removeAfter(t *testing.T, dc *mobyclient.Client, id string) {
	t.Cleanup(func() {
		_, _ = dc.ContainerRemove(context.WithoutCancel(t.Context()), id, mobyclient.ContainerRemoveOptions{Force: true}) //nolint:errcheck // Best effort cleanup
	})
}

// plant creates a container as a dead run would have left it: managed by
// dockertest and labeled with the given expiry. It is started if running is
// set. The alpine image must already be present.
func plant(t *testing.T, dc *mobyclient.Client, expires string, running bool) string {
	t.Helper()
	resp, err := dc.ContainerCreate(t.Context(), mobyclient.ContainerCreateOptions{Config: &container.Config{
		Image:  "alpine",
		Cmd:    []string{"sleep", "300"},
		Labels: map[string]string{labelManaged: "true", labelRun: "planted", labelExpires: expires},
	}})
	if err != nil {
		t.Fatalf("ContainerCreate() error = %v", err)
	}
	removeAfter(t, dc, resp.ID)
	if running {
		if _, err := dc.ContainerStart(t.Context(), resp.ID, mobyclient.ContainerStartOptions{}); err != nil {
			t.Fatalf("ContainerStart() error = %v", err)
		}
	}
	return resp.ID
}

// clean reports whether a child's stderr shows a clean shutdown. Reports about
// expired runs are ignored: they concern resources that other processes on the
// shared daemon left behind.
func clean(stderr string) bool {
	for line := range strings.Lines(stderr) {
		if !strings.Contains(line, " of expired run ") {
			return false
		}
	}
	return true
}

// farFuture is an expiry no run reaches.
const farFuture = "99999999999"

func assertContainerAlive(t *testing.T, dc *mobyclient.Client, id string) {
	t.Helper()
	if _, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{}); err != nil {
		t.Fatalf("ContainerInspect(%s) error = %v, want container alive", id, err)
	}
}

func TestMainInterruptRemovesContainers(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "container-stuck")
	waitFor(t, c.stdout, readyMarker)
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, id)
}

func TestMainInterruptDuringCreation(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "creating")
	waitFor(t, c.stdout, readyMarker)
	// The container exists, but the child has not received its ID.
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)
	inspect, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	labels := inspect.Container.Config.Labels
	if labels[labelExpires] == "" {
		t.Fatalf("labels = %v, want an expiry under Main", labels)
	}
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, id)
	filters := mobyclient.Filters{}
	filters.Add("label", labelRun+"="+labels[labelRun])
	list, err := dc.ContainerList(t.Context(), mobyclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("%d containers of the interrupted run survived; stderr:\n%s", len(list.Items), c.stderr.String())
	}
}

func TestMainRecoversAfterExit(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "container-exit")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	dead := c.containerID(t)
	removeAfter(t, dc, dead)
	expired := plant(t, dc, "1", true)
	active := plant(t, dc, farFuture, true)

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerGone(t, dc, expired)
	assertContainerAlive(t, dc, active)
	// The exited run's expiry has not passed yet, so it is not provably dead.
	assertContainerAlive(t, dc, dead)
}

func TestMainRecoversStoppedContainer(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "container-stopped-exit")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	stopped := c.containerID(t)
	removeAfter(t, dc, stopped)
	expired := plant(t, dc, "1", false)

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerGone(t, dc, expired)
	assertContainerAlive(t, dc, stopped)
}

func TestMainRecoversAfterKill(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "container-stuck")
	waitFor(t, c.stdout, readyMarker)
	killed := c.containerID(t)
	removeAfter(t, dc, killed)
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.wait(t)
	assertContainerAlive(t, dc, killed)
	expired := plant(t, dc, "1", true)

	if code, stderr := run(t, "pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	// Whether "pass", which creates no pool, leaves the expired container alone
	// is not asserted here: any other run on the daemon may sweep it. The unit
	// tests cover which daemons are swept.

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerGone(t, dc, expired)
	// The killed run's expiry has not passed yet, so it is not provably dead.
	assertContainerAlive(t, dc, killed)
}

func TestMainActiveRunIsProtected(t *testing.T) {
	dc := dockerClient(t)
	active := startChild(t, "container-stuck")
	waitFor(t, active.stdout, readyMarker)
	id := active.containerID(t)

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerAlive(t, dc, id)

	interrupt(t, active.cmd)
	if code := active.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, active.stderr.String())
	}
	assertContainerGone(t, dc, id)
}
