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

func TestSetupExitStatus(t *testing.T) {
	cases := []struct {
		name     string
		testCase string
		wantErr  string
		wantCode int
	}{
		{name: "clean run exits 0", testCase: "setup-pass", wantCode: 0},
		{name: "failing tests exit 1", testCase: "setup-fail", wantCode: 1},
		{name: "leak after passing tests exits 1", testCase: "setup-leak", wantCode: 1, wantErr: "found unexpected goroutines"},
		{name: "cleanup error is the caller's to report", testCase: "setup-cleanup-error", wantCode: 0, wantErr: "teardown failed"},
		{name: "caller chooses the exit code", testCase: "setup-exit-code", wantCode: 42},
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
			// Caller code runs before and after m.Run and cleanup.
			want := []string{beforeRunMarker, afterRunMarker, afterCleanupMarker}
			if got := c.markers(); !slices.Equal(got, want) {
				t.Fatalf("markers = %q, want %q", got, want)
			}
		})
	}
}

func TestSetupInvalidOptionsReturnsError(t *testing.T) {
	c := startChild(t, "setup-invalid-options")
	if code := c.wait(t); code != 1 || !strings.Contains(c.stderr.String(), "must not be negative") {
		t.Fatalf("exit = %d, stderr = %q", code, c.stderr.String())
	}
	if got := c.markers(); len(got) != 0 {
		t.Fatalf("markers = %q, want TestMain to stop before m.Run", got)
	}
}

func TestSetupInterruptOfStuckTest(t *testing.T) {
	c := startChild(t, "setup-stuck")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	if !strings.Contains(c.stderr.String(), "received interrupt") {
		t.Fatalf("stderr = %q", c.stderr.String())
	}
}

func TestSetupTerminateOfStuckTest(t *testing.T) {
	c := startChild(t, "setup-stuck")
	waitFor(t, c.stdout, readyMarker)
	terminate(t, c.cmd)
	if code := c.wait(t); code != 143 {
		t.Fatalf("exit = %d, want 143; stderr:\n%s", code, c.stderr.String())
	}
}

func TestSetupSecondSignalTerminatesImmediately(t *testing.T) {
	c := startChild(t, "setup-stuck-cleanup")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	waitFor(t, c.errCh, "cleaning up")
	interrupt(t, c.cmd)
	if code := c.wait(t); code == 130 || code == 0 {
		t.Fatalf("exit = %d, want termination by the second signal", code)
	}
}

// A signal during the caller's cleanup terminates the process at once:
// cleanup has already stopped signal handling, so it cannot start a second
// shutdown or deadlock with the running one.
func TestSetupSignalDuringCleanupTerminates(t *testing.T) {
	c := startChild(t, "setup-signal-during-cleanup")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code == 130 || code == 0 {
		t.Fatalf("exit = %d, want termination by the signal; stderr:\n%s", code, c.stderr.String())
	}
	if strings.Contains(c.stderr.String(), "cleaning up") {
		t.Fatalf("stderr = %q, want no signal shutdown", c.stderr.String())
	}
}

// When the caller calls cleanup while a signal shutdown is running, the
// shutdown runs once and the process exits with the signal status.
func TestSetupCleanupDuringSignalShutdown(t *testing.T) {
	c := startChild(t, "setup-signal-race")
	waitFor(t, c.stdout, readyMarker)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	want := []string{beforeRunMarker, callbackMarker, afterRunMarker}
	if got := c.markers(); !slices.Equal(got, want) {
		t.Fatalf("markers = %q, want %q", got, want)
	}
}

func TestSetupRemovesContainer(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "setup-container-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, c.containerID(t))
}

func TestSetupRemovesNetwork(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "setup-network-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	id := c.containerID(t)
	if _, err := dc.NetworkInspect(t.Context(), id, mobyclient.NetworkInspectOptions{}); !errdefs.IsNotFound(err) {
		t.Fatalf("NetworkInspect(%s) error = %v, want not found", id, err)
	}
}

func TestSetupReconcilesLostCreateResponse(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "setup-lost-pass")
	if code := c.wait(t); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, c.containerID(t))
}

func TestSetupInterruptDuringCreation(t *testing.T) {
	dc := dockerClient(t)
	c := startChild(t, "setup-creating")
	waitFor(t, c.stdout, readyMarker)
	id := c.containerID(t)
	assertContainerAlive(t, dc, id)
	interrupt(t, c.cmd)
	if code := c.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, c.stderr.String())
	}
	assertContainerGone(t, dc, id)
}

// Setup's resources carry no expiry, so a Main run's sweep never removes them,
// while the Setup run itself still cleans them up.
func TestSetupResourcesAreNotSweptByOtherRuns(t *testing.T) {
	dc := dockerClient(t)
	setup := startChild(t, "setup-container-stuck")
	waitFor(t, setup.stdout, readyMarker)
	id := setup.containerID(t)
	removeAfter(t, dc, id)
	inspect, err := dc.ContainerInspect(t.Context(), id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if labels := inspect.Container.Config.Labels; labels[labelExpires] != "" || labels[labelManaged] != "true" {
		t.Fatalf("labels = %v, want managed without expiry under Setup", labels)
	}

	if code, stderr := run(t, "container-pass"); code != 0 || !clean(stderr) {
		t.Fatalf("exit = %d, stderr:\n%s", code, stderr)
	}
	assertContainerAlive(t, dc, id)

	interrupt(t, setup.cmd)
	if code := setup.wait(t); code != 130 {
		t.Fatalf("exit = %d, want 130; stderr:\n%s", code, setup.stderr.String())
	}
	assertContainerGone(t, dc, id)
}
