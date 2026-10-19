// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package maintest

import (
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	mobyclient "github.com/moby/moby/client"
)

// markers returns the TestMain markers the child printed, in order.
func (c *child) markers() []string {
	var got []string
	for line := range strings.Lines(c.stdoutText.String()) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DOCKERTEST-") && line != readyMarker {
			got = append(got, line)
		}
	}
	return got
}

func TestWrapExitStatus(t *testing.T) {
	cases := []struct {
		name     string
		testCase string
		wantErr  string
		wantCode int
	}{
		{name: "clean run exits 0", testCase: "wrap-pass", wantCode: 0},
		{name: "failing tests exit 1", testCase: "wrap-fail", wantCode: 1},
		{name: "leak after passing tests exits 1", testCase: "wrap-leak", wantCode: 1, wantErr: "found unexpected goroutines"},
		{name: "teardown failure is a warning", testCase: "wrap-cleanup-error", wantCode: 0, wantErr: "teardown failed"},
		{name: "caller chooses the exit code", testCase: "wrap-exit-code", wantCode: 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := startChild(t, tc.testCase)
			code := c.wait(t)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; stderr:\n%s", code, tc.wantCode, c.stderr.String())
			}
			if !strings.Contains(c.stderr.String(), tc.wantErr) {
				t.Fatalf("stderr = %q, want %q", c.stderr.String(), tc.wantErr)
			}
			// Caller code runs before and after m.Run and the shutdown.
			want := []string{beforeRunMarker, afterRunMarker, afterCleanupMarker}
			if got := c.markers(); !slices.Equal(got, want) {
				t.Fatalf("markers = %q, want %q", got, want)
			}
		})
	}
}

func TestWrapInvalidOptionsReturns1(t *testing.T) {
	c := startChild(t, "wrap-invalid-options")
	if code := c.wait(t); code != 1 || !strings.Contains(c.stderr.String(), "must not be negative") {
		t.Fatalf("exit = %d, stderr = %q", code, c.stderr.String())
	}
	if got, want := c.markers(), []string{afterCleanupMarker}; !slices.Equal(got, want) {
		t.Fatalf("markers = %q, want %q: run is not called", got, want)
	}
}

func TestWrapInterruptOfStuckTest(t *testing.T) {
	c := startChild(t, "wrap-stuck")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	if !strings.Contains(c.stderr.String(), "received interrupt") {
		t.Fatalf("stderr = %q", c.stderr.String())
	}
}

func TestWrapTerminateOfStuckTest(t *testing.T) {
	c := startChild(t, "wrap-stuck")
	waitFor(t, c.stdout, readyMarker)
	terminate(t, c.cmd)
	if code := c.wait(t); code != 143 {
		t.Fatalf("exit = %d, want 143; stderr:\n%s", code, c.stderr.String())
	}
}

func TestWrapSecondSignalTerminatesImmediately(t *testing.T) {
	c := startChild(t, "wrap-stuck-cleanup")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	waitFor(t, c.errCh, "cleaning up")
	interrupt(t, c.cmd)
	if code := c.wait(t); code == 130 || code == 0 {
		t.Fatalf("exit = %d, want termination by the second signal", code)
	}
}

// A signal during Wrap's shutdown terminates the process at once: the
// shutdown has already stopped signal handling, so it cannot start a second
// shutdown or deadlock with the running one.
func TestWrapSignalDuringCleanupTerminates(t *testing.T) {
	c := startChild(t, "wrap-signal-during-cleanup")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code == 130 || code == 0 {
		t.Fatalf("exit = %d, want termination by the signal; stderr:\n%s", code, c.stderr.String())
	}
	if strings.Contains(c.stderr.String(), "cleaning up") {
		t.Fatalf("stderr = %q, want no signal shutdown", c.stderr.String())
	}
}

// When run returns while a signal shutdown is running, the shutdown runs once
// and the process exits with the signal status.
func TestWrapCleanupDuringSignalShutdown(t *testing.T) {
	c := startChild(t, "wrap-signal-race")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	want := []string{beforeRunMarker, teardownMarker, afterRunMarker}
	if got := c.markers(); !slices.Equal(got, want) {
		t.Fatalf("markers = %q, want %q", got, want)
	}
}

func TestWrapRemovesContainer(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "wrap-container-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, c.containerID(t))
}

func TestWrapRemovesNetwork(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "wrap-network-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	id := c.containerID(t)
	if _, err := dc.NetworkInspect(t.Context(), id, mobyclient.NetworkInspectOptions{}); !errdefs.IsNotFound(err) {
		t.Fatalf("NetworkInspect(%s) error = %v, want not found", id, err)
	}
}

func TestWrapReconcilesLostCreateResponse(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "wrap-lost-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, c.containerID(t))
}

func TestWrapInterruptDuringCreation(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "wrap-creating")
	waitFor(t, c.stdout, readyMarker)
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, id)
}

// Wrap's resources carry an expiry as under TestMain, so another run's sweep
// leaves them alone while it lies ahead, and the Wrap run itself cleans them up.
func TestWrapResourcesAreNotSweptByOtherRuns(t *testing.T) {
	dc := dockerClient(t)
	wrapped := startChild(t, "wrap-container-stuck")
	waitFor(t, wrapped.stdout, readyMarker)
	id := wrapped.containerID(t)
	removeAfter(t, dc, id)
	inspect, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if labels := inspect.Container.Config.Labels; labels[labelExpires] == "" || labels[labelManaged] != "true" {
		t.Fatalf("labels = %v, want managed with an expiry under Wrap", labels)
	}

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerAlive(t, dc, id)

	interrupt(t, wrapped.cmd)
	if code := wrapped.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, wrapped.stderr.String())
	}
	assertContainerGone(t, dc, id)
}
