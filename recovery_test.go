// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/filelock"
)

func writeTestManifest(t *testing.T, dir string, m runManifest) {
	t.Helper()
	if err := writeManifest(dir, m); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}
}

func readTestManifest(t *testing.T, dir, runID string) (runManifest, bool) {
	t.Helper()
	data, err := os.ReadFile(manifestPath(dir, runID))
	if errors.Is(err, os.ErrNotExist) {
		return runManifest{}, false
	}
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var m runManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	return m, true
}

// abandonedContainers makes the fake daemon report one container per run ID
// labeled with the full ownership tuple of that run.
func abandonedContainers(c *fakeClient, host string, runIDs ...string) {
	c.containerList = func(_ context.Context, opts mobyclient.ContainerListOptions) ([]container.Summary, error) {
		var items []container.Summary
		for _, id := range runIDs {
			labels := map[string]string{labelManaged: "true", labelScope: "s", labelHost: host, labelRun: id}
			if opts.Filters["label"][labelRun+"="+id] {
				items = append(items, container.Summary{ID: "container-of-" + id, Labels: labels})
			}
		}
		return items, nil
	}
}

func TestRecoverAbandonedRun(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1"}})
	c := newFakeClient()
	abandonedContainers(c, "h", "dead")
	var warnings bytes.Buffer
	warn := func(format string, _ ...any) { warnings.WriteString(strings.TrimSpace(format) + "\n") }

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)

	if got := c.callsMatching("ContainerRemove"); len(got) != 1 || got[0] != "ContainerRemove container-of-dead force=true volumes=true canceled=false" {
		t.Fatalf("ContainerRemove calls = %q", got)
	}
	if _, ok := readTestManifest(t, dir, "dead"); ok {
		t.Fatal("manifest of recovered run still exists")
	}
	if _, err := os.Stat(lockPath(dir, "dead")); err != nil {
		t.Fatalf("lock file of recovered run was removed: %v", err)
	}
	if warnings.Len() != 0 {
		t.Fatalf("unexpected warnings:\n%s", warnings.String())
	}
}

func TestRecoverSkipsActiveRun(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "active", Daemons: []string{"d1"}})
	held, err := filelock.TryLock(lockPath(dir, "active"))
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	t.Cleanup(func() { held.Close() })
	c := newFakeClient()
	abandonedContainers(c, "h", "active")

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(string, ...any) { t.Fatal("unexpected warning") })

	if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
		t.Fatalf("active run's container removed: %q", got)
	}
	if _, ok := readTestManifest(t, dir, "active"); !ok {
		t.Fatal("active run's manifest removed")
	}
}

func TestRecoverSkipsMismatchedRecords(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "other", Host: "h", RunID: "scope-mismatch", Daemons: []string{"d1"}})
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "elsewhere", RunID: "host-mismatch", Daemons: []string{"d1"}})
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "daemon-mismatch", Daemons: []string{"d2"}})
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "me", Daemons: []string{"d1"}})
	c := newFakeClient()
	abandonedContainers(c, "h", "scope-mismatch", "host-mismatch", "daemon-mismatch", "me")

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(string, ...any) { t.Fatal("unexpected warning") })

	if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
		t.Fatalf("mismatched run resources removed: %q", got)
	}
	for _, id := range []string{"scope-mismatch", "host-mismatch", "daemon-mismatch", "me"} {
		if _, ok := readTestManifest(t, dir, id); !ok {
			t.Fatalf("manifest %s removed", id)
		}
	}
}

func TestRecoverWarnsOnMalformedRecords(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "garbage.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Run ID inside the record must match the file name.
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "renamed", Daemons: []string{"d1"}})
	if err := os.Rename(manifestPath(dir, "renamed"), manifestPath(dir, "moved")); err != nil {
		t.Fatal(err)
	}
	c := newFakeClient()
	abandonedContainers(c, "h", "renamed", "moved")
	var warnings []string
	warn := func(format string, _ ...any) { warnings = append(warnings, format) }

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)

	if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
		t.Fatalf("resources removed for malformed records: %q", got)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %q, want one per malformed record", warnings)
	}
}

func TestRecoverKeepsRecordOnPartialFailureAndRetriesLater(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1"}})
	c := newFakeClient()
	abandonedContainers(c, "h", "dead")
	c.containerRemove = func(context.Context, string, mobyclient.ContainerRemoveOptions) error { return errors.New("busy") }
	var warnings []string
	warn := func(format string, _ ...any) { warnings = append(warnings, format) }

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)
	if _, ok := readTestManifest(t, dir, "dead"); !ok {
		t.Fatal("manifest removed although recovery failed")
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want one", warnings)
	}

	c.containerRemove = nil
	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)
	if _, ok := readTestManifest(t, dir, "dead"); ok {
		t.Fatal("manifest still present after successful retry")
	}
	if got := c.callsMatching("ContainerRemove"); len(got) != 2 {
		t.Fatalf("ContainerRemove calls = %q, want a failed and a successful attempt", got)
	}
}

func TestRecoverKeepsRecordForOtherDaemons(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1", "d2"}})
	c := newFakeClient()
	abandonedContainers(c, "h", "dead")

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(string, ...any) { t.Fatal("unexpected warning") })

	m, ok := readTestManifest(t, dir, "dead")
	if !ok {
		t.Fatal("manifest removed although daemon d2 is not reconciled")
	}
	if len(m.Daemons) != 1 || m.Daemons[0] != "d2" {
		t.Fatalf("manifest daemons = %v, want [d2]", m.Daemons)
	}
}

func TestRunStateLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	s, err := openRunState(dir, "s", "h", "run1")
	if err != nil {
		t.Fatalf("openRunState() error = %v", err)
	}
	if _, lockErr := filelock.TryLock(lockPath(dir, "run1")); !errors.Is(lockErr, filelock.ErrLocked) {
		t.Fatalf("run lock not held: %v", lockErr)
	}
	if _, openErr := openRunState(dir, "s", "h", "run1"); openErr == nil {
		t.Fatal("second openRunState() for the same run succeeded")
	}
	if addErr := s.addDaemon("d1"); addErr != nil {
		t.Fatalf("addDaemon() error = %v", addErr)
	}
	if addErr := s.addDaemon("d1"); addErr != nil {
		t.Fatalf("repeated addDaemon() error = %v", addErr)
	}
	m, ok := readTestManifest(t, dir, "run1")
	if !ok || m.Scope != "s" || m.Host != "h" || len(m.Daemons) != 1 || m.Daemons[0] != "d1" {
		t.Fatalf("manifest = %+v, %t", m, ok)
	}

	if closeErr := s.close(false); closeErr != nil {
		t.Fatalf("close(false) error = %v", closeErr)
	}
	if _, ok := readTestManifest(t, dir, "run1"); !ok {
		t.Fatal("manifest removed after unclean close, want it kept for recovery")
	}
	unlocked, err := filelock.TryLock(lockPath(dir, "run1"))
	if err != nil {
		t.Fatalf("lock still held after close: %v", err)
	}
	if closeErr := unlocked.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	s, err = openRunState(dir, "s", "h", "run1")
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if closeErr := s.close(true); closeErr != nil {
		t.Fatalf("close(true) error = %v", closeErr)
	}
	if _, ok := readTestManifest(t, dir, "run1"); ok {
		t.Fatal("manifest kept after clean close")
	}
	if _, err := os.Stat(lockPath(dir, "run1")); err != nil {
		t.Fatalf("lock file removed: %v", err)
	}
}
