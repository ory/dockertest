// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package maintest exercises dockertest.Main in child processes: the test
// binary re-executes itself with DOCKERTEST_MAIN_CASE set, and that child's
// TestMain hands control to dockertest.Main.
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
	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
	"github.com/ory/dockertest/v4/internal/client"
)

const (
	envCase     = "DOCKERTEST_MAIN_CASE"
	envStateDir = "DOCKERTEST_MAIN_STATE_DIR"
	envScope    = "DOCKERTEST_MAIN_SCOPE"
	envOutFile  = "DOCKERTEST_MAIN_OUT"
	readyMarker = "DOCKERTEST-READY"
)

func TestMain(m *testing.M) {
	testCase := os.Getenv(envCase)
	if testCase == "" {
		os.Exit(m.Run())
	}
	opts := dockertest.MainOptions{
		Scope:          os.Getenv(envScope),
		StateDir:       os.Getenv(envStateDir),
		CleanupTimeout: 20 * time.Second,
	}
	switch testCase {
	case "cleanup-error":
		opts.Cleanup = func(context.Context) error { return errors.New("teardown failed") }
	case "stuck-cleanup":
		opts.CleanupTimeout = time.Minute
		opts.Cleanup = func(context.Context) error { select {} }
	}
	dockertest.Main(context.Background(), m, opts)
}

// TestHelperProcess is the body of every child process.
func TestHelperProcess(t *testing.T) {
	testCase := os.Getenv(envCase)
	switch testCase {
	case "":
		t.Skip("not a child process")
	case "pass", "cleanup-error":
	case "fail":
		t.Fatal("intended failure")
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
		dc, err := client.NewMobyClient(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		pool := dockertest.NewPoolT(t, "", dockertest.WithMobyClient(lostCreateClient{DockerClient: dc, t: t}))
		_, _ = pool.Run(t.Context(), "alpine", dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse()) //nolint:errcheck // the interrupt fails it
		select {}
	default:
		t.Fatalf("unknown case %q", testCase)
	}
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
type lostCreateClient struct {
	client.DockerClient
	t *testing.T
}

func (c lostCreateClient) ContainerCreate(ctx context.Context, opts mobyclient.ContainerCreateOptions) (mobyclient.ContainerCreateResult, error) { //nolint:gocritic // mirrors the client interface
	resp, err := c.DockerClient.ContainerCreate(ctx, opts)
	if err != nil {
		return resp, err
	}
	writeOut(c.t, resp.ID)
	ready()
	<-ctx.Done()
	return mobyclient.ContainerCreateResult{}, ctx.Err()
}

// child is a running child process whose stdout and stderr are captured.
//
//nolint:govet // field alignment traded for readability
type child struct {
	cmd    *exec.Cmd
	stdout <-chan string
	stderr *strings.Builder
	errCh  <-chan string
	out    string
}

func startChild(t *testing.T, testCase, stateDir, scope string) *child {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1") // #nosec G204 -- re-executes the test binary
	cmd.Env = append(os.Environ(), envCase+"="+testCase, envStateDir+"="+stateDir, envScope+"="+scope, envOutFile+"="+out)
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
	c := &child{cmd: cmd, stdout: lines(stdout), stderr: &strings.Builder{}, out: out}
	c.errCh = teeLines(stderr, c.stderr)
	t.Cleanup(func() { _ = cmd.Process.Kill() }) //nolint:errcheck // Best effort cleanup if the test failed early
	return c
}

func lines(r io.Reader) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			ch <- scanner.Text()
		}
	}()
	return ch
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

func run(t *testing.T, testCase, stateDir, scope string) (code int, stderr string) {
	t.Helper()
	c := startChild(t, testCase, stateDir, scope)
	return c.wait(t), c.stderr.String()
}

func TestMainExitStatus(t *testing.T) {
	stateDir := t.TempDir()
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
			code, stderr := run(t, tc.testCase, stateDir, "maintest")
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; stderr:\n%s", code, tc.wantCode, stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr, tc.wantErr)
			}
		})
	}
	if entries, _ := os.ReadDir(stateDir); len(entries) != 3 {
		t.Fatalf("state dir after clean runs has %d entries, want only the 3 lock files", len(entries))
	}
}

func TestMainMissingScopeExits1(t *testing.T) {
	code, stderr := run(t, "pass", t.TempDir(), "")
	if code != 1 || !strings.Contains(stderr, "Scope is required") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestMainInterruptOfStuckTest(t *testing.T) {
	c := startChild(t, "stuck", t.TempDir(), "maintest")
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
	c := startChild(t, "stuck", t.TempDir(), "maintest")
	waitFor(t, c.stdout, readyMarker)
	terminate(t, c.cmd)
	if code := c.wait(t); code != 143 {
		t.Fatalf("exit = %d, want 143; stderr:\n%s", code, c.stderr.String())
	}
}

func TestMainSecondSignalTerminatesImmediately(t *testing.T) {
	c := startChild(t, "stuck-cleanup", t.TempDir(), "maintest")
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

func assertContainerAlive(t *testing.T, dc *mobyclient.Client, id string) {
	t.Helper()
	if _, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{}); err != nil {
		t.Fatalf("ContainerInspect(%s) error = %v, want container alive", id, err)
	}
}

func TestMainInterruptRemovesContainers(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "container-stuck", t.TempDir(), "maintest")
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
	// A scope of its own: other tests leave containers in "maintest" on
	// purpose, and the child's run ID is unknown here.
	const scope = "maintest-creating"
	c := startChild(t, "creating", t.TempDir(), scope)
	waitFor(t, c.stdout, readyMarker)
	// The container exists, but the child has not received its ID.
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, id)
	filters := mobyclient.Filters{}
	filters.Add("label", "io.ory.dockertest.scope="+scope)
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
	stateDir := t.TempDir()
	code, stderr := run(t, "container-exit", stateDir, "maintest")
	if code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	c := startChild(t, "container-exit", stateDir, "maintest")
	_ = c.wait(t)
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)

	t.Run("different scope leaves it alone", func(t *testing.T) {
		if code, stderr := run(t, "container-pass", stateDir, "other-scope"); code != 0 {
			t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
		}
		assertContainerAlive(t, dc, id)
	})

	t.Run("same scope recovers", func(t *testing.T) {
		if code, stderr := run(t, "container-pass", stateDir, "maintest"); code != 0 || stderr != "" {
			t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
		}
		assertContainerGone(t, dc, id)
	})

	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("manifest %s left after recovery", e.Name())
		}
	}
}

func TestMainRecoversStoppedContainer(t *testing.T) {
	dc := dockerClient(t)
	stateDir := t.TempDir()
	c := startChild(t, "container-stopped-exit", stateDir, "maintest")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	id := c.containerID(t)
	inspect, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect(%s) error = %v", id, err)
	}
	if inspect.Container.State.Running {
		t.Fatal("container still running, want it stopped")
	}

	if code, stderr := run(t, "container-pass", stateDir, "maintest"); code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerGone(t, dc, id)
}

func TestMainRecoversAfterKill(t *testing.T) {
	dc := dockerClient(t)
	stateDir := t.TempDir()
	c := startChild(t, "container-stuck", stateDir, "maintest")
	waitFor(t, c.stdout, readyMarker)
	id := c.containerID(t)
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.wait(t)
	assertContainerAlive(t, dc, id)

	if code, stderr := run(t, "pass", stateDir, "maintest"); code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	// Recovery only runs once a pool talks to the daemon; "pass" creates none.
	assertContainerAlive(t, dc, id)

	if code, stderr := run(t, "container-pass", stateDir, "maintest"); code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerGone(t, dc, id)
}

func TestMainActiveRunIsProtected(t *testing.T) {
	dc := dockerClient(t)
	stateDir := t.TempDir()
	active := startChild(t, "container-stuck", stateDir, "maintest")
	waitFor(t, active.stdout, readyMarker)
	id := active.containerID(t)

	if code, stderr := run(t, "container-pass", stateDir, "maintest"); code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerAlive(t, dc, id)

	interrupt(t, active.cmd)
	if code := active.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, active.stderr.String())
	}
	assertContainerGone(t, dc, id)
}
