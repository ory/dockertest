// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gofrs/flock"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	mobyclient "github.com/moby/moby/client"
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
	held, err := tryLock(lockPath(dir, "active"))
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	t.Cleanup(func() { held.Unlock() })
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
	if _, lockErr := tryLock(lockPath(dir, "run1")); !errors.Is(lockErr, errLocked) {
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
	unlocked, err := tryLock(lockPath(dir, "run1"))
	if err != nil {
		t.Fatalf("lock still held after close: %v", err)
	}
	if closeErr := unlocked.Unlock(); closeErr != nil {
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

func TestTryLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	first, err := tryLock(path)
	if err != nil {
		t.Fatalf("tryLock() error = %v", err)
	}
	t.Cleanup(func() { first.Unlock() })

	if _, err := tryLock(path); !errors.Is(err, errLocked) {
		t.Fatalf("second tryLock() error = %v, want errLocked", err)
	}
}

func TestUnlockReleasesLockAndKeepsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	first, err := tryLock(path)
	if err != nil {
		t.Fatalf("tryLock() error = %v", err)
	}
	if unlockErr := first.Unlock(); unlockErr != nil {
		t.Fatalf("Unlock() error = %v", unlockErr)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("lock file removed after Unlock: %v", statErr)
	}

	second, err := tryLock(path)
	if err != nil {
		t.Fatalf("tryLock() after Unlock error = %v, want nil", err)
	}
	t.Cleanup(func() { second.Unlock() })
}

func TestTryLockMissingDirectory(t *testing.T) {
	_, err := tryLock(filepath.Join(t.TempDir(), "missing", "run.lock"))
	if err == nil {
		t.Fatal("tryLock() error = nil, want error for missing directory")
	}
	if errors.Is(err, errLocked) {
		t.Fatalf("tryLock() error = %v, want a non-errLocked error", err)
	}
}

func TestRunStateAddDaemonRetriesFailedWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := openRunState(dir, "s", "h", "run1")
	if err != nil {
		t.Fatalf("openRunState() error = %v", err)
	}
	t.Cleanup(func() { _ = s.close(true) }) //nolint:errcheck // best effort

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.addDaemon("d1"); err == nil {
		t.Fatal("addDaemon() succeeded without a state directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.addDaemon("d1"); err != nil {
		t.Fatalf("retried addDaemon() error = %v", err)
	}
	if m, ok := readTestManifest(t, dir, "run1"); !ok || len(m.Daemons) != 1 || m.Daemons[0] != "d1" {
		t.Fatalf("manifest = %+v, %t; want the daemon persisted by the retry", m, ok)
	}
}

func TestRecoverKeepsRecordWhileImageRemovalConflicts(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1"}})
	c := newFakeClient()
	c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
		return []image.Summary{{ID: "sha256:x", Labels: runLabels("s", "h", "dead"), RepoTags: []string{"myapp:a", "myapp:b"}}}, nil
	}
	// Tag myapp:b may meanwhile point to an unrelated image.
	c.imageInspect = func(_ context.Context, id string) (image.InspectResponse, error) {
		return image.InspectResponse{ID: id, RepoTags: []string{"myapp:a", "myapp:b"}}, nil
	}
	conflict := fmt.Errorf("%w: image is referenced in multiple repositories", errdefs.ErrConflict)
	c.imageRemove = func(_ context.Context, ref string, _ mobyclient.ImageRemoveOptions) error {
		if ref == "sha256:x" {
			return conflict
		}
		return nil
	}
	var warnings []string
	warn := func(format string, _ ...any) { warnings = append(warnings, format) }

	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)
	if _, ok := readTestManifest(t, dir, "dead"); !ok {
		t.Fatal("manifest removed although the image is still present")
	}
	if got := c.callsMatching("ImageRemove"); len(got) != 1 || got[0] != "ImageRemove sha256:x force=false prune=false" {
		t.Fatalf("ImageRemove calls = %q, want only the removal by image ID", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want one", warnings)
	}

	c.imageRemove = nil
	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, warn)
	if _, ok := readTestManifest(t, dir, "dead"); ok {
		t.Fatal("manifest still present after successful retry")
	}
	want := "ImageRemove sha256:x force=false prune=false"
	if got := c.callsMatching("ImageRemove"); len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("ImageRemove calls = %q, want only removals by image ID", got)
	}
}

// fakeImage is an image in the store modeled by imageGraph.
type fakeImage struct {
	labels map[string]string
	id     string
	parent string
	tagged bool
}

// imageGraph makes the fake daemon model a classic image store that lists
// images in the given order. As on the daemon, an untagged image with children
// is an intermediate image that is only listed with All, removing an image
// with children conflicts, and removing a child without pruning keeps its
// parent. It returns the IDs of the images that remain.
func imageGraph(c *fakeClient, images ...fakeImage) (remaining func() []string) {
	hasChildren := func(id string) bool {
		return slices.ContainsFunc(images, func(img fakeImage) bool { return img.parent == id })
	}
	c.imageList = func(_ context.Context, opts mobyclient.ImageListOptions) ([]image.Summary, error) {
		var items []image.Summary
	next:
		for _, img := range images {
			if !opts.All && !img.tagged && hasChildren(img.id) {
				continue
			}
			for f := range opts.Filters["label"] {
				if k, v, _ := strings.Cut(f, "="); img.labels[k] != v {
					continue next
				}
			}
			items = append(items, image.Summary{ID: img.id, ParentID: img.parent, Labels: img.labels})
		}
		return items, nil
	}
	c.imageRemove = func(_ context.Context, id string, opts mobyclient.ImageRemoveOptions) error {
		i := slices.IndexFunc(images, func(img fakeImage) bool { return img.id == id })
		switch {
		case opts.Force || opts.PruneChildren:
			return errors.New("unexpected forced or pruning removal")
		case i < 0:
			return errdefs.ErrNotFound
		case hasChildren(id):
			return fmt.Errorf("%w: image has dependent child images", errdefs.ErrConflict)
		}
		images = slices.Delete(images, i, i+1)
		return nil
	}
	return func() []string {
		ids := make([]string, 0, len(images))
		for _, img := range images {
			ids = append(ids, img.id)
		}
		return ids
	}
}

func TestRecoverRemovesIntermediateImages(t *testing.T) {
	owned := runLabels("s", "h", "dead")
	retained := maps.Clone(owned)
	retained[labelRetain] = labelTrue
	parent := fakeImage{id: "sha256:a", labels: owned}
	child := fakeImage{id: "sha256:b", parent: "sha256:a", labels: owned, tagged: true}

	type step struct {
		removals []string // image IDs whose removal is attempted
		remain   []string // image IDs left afterwards
		record   bool     // whether the manifest is kept for a later retry
	}
	for _, tc := range []struct {
		name   string
		images []fakeImage // in listing order
		steps  []step      // consecutive recoveries
	}{{
		name:   "child first",
		images: []fakeImage{child, parent},
		steps:  []step{{removals: []string{"sha256:b", "sha256:a"}}},
	}, {
		name:   "parent first",
		images: []fakeImage{parent, child},
		steps: []step{
			{removals: []string{"sha256:a", "sha256:b"}, remain: []string{"sha256:a"}, record: true},
			{removals: []string{"sha256:a"}},
		},
	}, {
		name: "foreign and retained intermediates",
		images: []fakeImage{
			{id: "sha256:b", parent: "sha256:r", labels: owned, tagged: true},
			{id: "sha256:r", parent: "sha256:f", labels: retained},
			{id: "sha256:f", labels: runLabels("s", "h", "other")},
		},
		steps: []step{{removals: []string{"sha256:b"}, remain: []string{"sha256:r", "sha256:f"}}},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1"}})
			c := newFakeClient()
			remaining := imageGraph(c, tc.images...)

			var want []string
			for i, s := range tc.steps {
				var warnings []string
				recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(format string, _ ...any) { warnings = append(warnings, format) })

				for _, id := range s.removals {
					want = append(want, "ImageRemove "+id+" force=false prune=false")
				}
				if got := c.callsMatching("ImageRemove"); !slices.Equal(got, want) {
					t.Fatalf("recovery %d: ImageRemove calls = %q, want %q", i, got, want)
				}
				if got := remaining(); !slices.Equal(got, s.remain) {
					t.Fatalf("recovery %d: remaining images = %q, want %q", i, got, s.remain)
				}
				if _, ok := readTestManifest(t, dir, "dead"); ok != s.record {
					t.Fatalf("recovery %d: manifest kept = %t, want %t", i, ok, s.record)
				}
				if s.record && len(warnings) != 1 || !s.record && len(warnings) != 0 {
					t.Fatalf("recovery %d: warnings = %q, want one only while the manifest is kept", i, warnings)
				}
			}
		})
	}
}

func TestRecoverKeepsRecordWhenImageListFails(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "dead", Daemons: []string{"d1"}})
	c := newFakeClient()
	c.imageList = func(context.Context, mobyclient.ImageListOptions) ([]image.Summary, error) {
		return nil, errors.New("daemon unavailable")
	}
	var warnings []string
	recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(format string, _ ...any) { warnings = append(warnings, format) })

	if _, ok := readTestManifest(t, dir, "dead"); !ok {
		t.Fatal("manifest removed although listing images failed")
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want one", warnings)
	}
}

// interleaveBeforeLock runs change once, right before recovery takes the lock
// of run runID, and restores the real lock afterwards.
func interleaveBeforeLock(t *testing.T, dir, runID string, change func()) {
	t.Helper()
	previous := lockRecoveredRun
	t.Cleanup(func() { lockRecoveredRun = previous })
	var done bool
	lockRecoveredRun = func(path string) (*flock.Flock, error) {
		if path == lockPath(dir, runID) && !done {
			done = true
			change()
		}
		return previous(path)
	}
}

func TestRecoverReadsRecordUnderRunLock(t *testing.T) {
	t.Run("daemon added before the owner exits", func(t *testing.T) {
		dir := t.TempDir()
		s, err := openRunState(dir, "s", "h", "racer")
		if err != nil {
			t.Fatalf("openRunState() error = %v", err)
		}
		if err := s.addDaemon("d1"); err != nil {
			t.Fatalf("addDaemon() error = %v", err)
		}
		// The owner records d2 after recovery decided to recover the run,
		// then exits and releases its lock.
		interleaveBeforeLock(t, dir, "racer", func() {
			if err := s.addDaemon("d2"); err != nil {
				t.Fatalf("addDaemon() error = %v", err)
			}
			if err := s.close(false); err != nil {
				t.Fatalf("close() error = %v", err)
			}
		})
		c := newFakeClient()
		abandonedContainers(c, "h", "racer")

		recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(string, ...any) { t.Fatal("unexpected warning") })

		if got := c.callsMatching("ContainerRemove"); len(got) != 1 {
			t.Fatalf("ContainerRemove calls = %q, want the run swept on d1", got)
		}
		m, ok := readTestManifest(t, dir, "racer")
		if !ok || !slices.Equal(m.Daemons, []string{"d2"}) {
			t.Fatalf("manifest = %+v, %t; want daemon d2 kept for recovery", m, ok)
		}

		recoverAbandoned(t.Context(), dir, "s", "h", "d2", "me", c, func(string, ...any) { t.Fatal("unexpected warning") })
		if _, ok := readTestManifest(t, dir, "racer"); ok {
			t.Fatal("manifest still present after recovery on d2")
		}
	})

	writeRaw := func(t *testing.T, dir string, data string) {
		t.Helper()
		if err := os.WriteFile(manifestPath(dir, "racer"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	marshal := func(t *testing.T, m runManifest) string {
		t.Helper()
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string) // replaces or removes the record
		record string                         // the record that must remain; empty if none
		warns  int
	}{{
		name: "record removed",
		change: func(t *testing.T, dir string) {
			if err := os.Remove(manifestPath(dir, "racer")); err != nil {
				t.Fatal(err)
			}
		},
		warns: 1,
	}, {
		name:   "record malformed",
		change: func(t *testing.T, dir string) { writeRaw(t, dir, "{not json") },
		record: "{not json",
		warns:  1,
	}, {
		name: "record identity replaced",
		change: func(t *testing.T, dir string) {
			writeRaw(t, dir, marshal(t, runManifest{Scope: "s", Host: "h", RunID: "other", Daemons: []string{"d1"}}))
		},
		record: marshal(t, runManifest{Scope: "s", Host: "h", RunID: "other", Daemons: []string{"d1"}}),
		warns:  1,
	}, {
		name: "scope changed",
		change: func(t *testing.T, dir string) {
			writeRaw(t, dir, marshal(t, runManifest{Scope: "other", Host: "h", RunID: "racer", Daemons: []string{"d1"}}))
		},
		record: marshal(t, runManifest{Scope: "other", Host: "h", RunID: "racer", Daemons: []string{"d1"}}),
	}, {
		name: "host changed",
		change: func(t *testing.T, dir string) {
			writeRaw(t, dir, marshal(t, runManifest{Scope: "s", Host: "elsewhere", RunID: "racer", Daemons: []string{"d1"}}))
		},
		record: marshal(t, runManifest{Scope: "s", Host: "elsewhere", RunID: "racer", Daemons: []string{"d1"}}),
	}, {
		name: "daemon no longer recorded",
		change: func(t *testing.T, dir string) {
			writeRaw(t, dir, marshal(t, runManifest{Scope: "s", Host: "h", RunID: "racer", Daemons: []string{"d2"}}))
		},
		record: marshal(t, runManifest{Scope: "s", Host: "h", RunID: "racer", Daemons: []string{"d2"}}),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestManifest(t, dir, runManifest{Scope: "s", Host: "h", RunID: "racer", Daemons: []string{"d1"}})
			interleaveBeforeLock(t, dir, "racer", func() { tc.change(t, dir) })
			c := newFakeClient()
			abandonedContainers(c, "h", "racer")
			var warnings []string

			recoverAbandoned(t.Context(), dir, "s", "h", "d1", "me", c, func(format string, _ ...any) { warnings = append(warnings, format) })

			if got := c.callsMatching("ContainerRemove"); len(got) != 0 {
				t.Fatalf("ContainerRemove calls = %q, want none for a stale record", got)
			}
			data, err := os.ReadFile(manifestPath(dir, "racer"))
			switch {
			case tc.record == "" && !errors.Is(err, os.ErrNotExist):
				t.Fatalf("record = %q, %v; want none", data, err)
			case tc.record != "" && (err != nil || string(data) != tc.record):
				t.Fatalf("record = %q, %v; want %q untouched", data, err, tc.record)
			}
			if len(warnings) != tc.warns {
				t.Fatalf("warnings = %q, want %d", warnings, tc.warns)
			}
			lock, err := tryLock(lockPath(dir, "racer"))
			if err != nil {
				t.Fatalf("run lock not released: %v", err)
			}
			if err := lock.Unlock(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
