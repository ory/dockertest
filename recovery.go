// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ory/dockertest/v4/internal/client"
	"github.com/ory/dockertest/v4/internal/filelock"
)

// runManifest is the on-disk ownership record of one Main-managed run. It
// holds no credentials: only the identity needed to attribute resources on a
// daemon to the run that created them.
type runManifest struct {
	Scope   string   `json:"scope"`
	Host    string   `json:"host"`
	RunID   string   `json:"run_id"`
	Daemons []string `json:"daemons"`
}

// runState is this run's manifest together with the lock that proves the run
// is alive. The lock is held for the lifetime of the process; the lock file is
// never removed so that lockers can never race on unlink and re-creation.
//
//nolint:govet // field alignment traded for readability
type runState struct {
	mu       sync.Mutex
	dir      string
	lock     *filelock.Lock
	manifest runManifest
}

func manifestPath(dir, runID string) string { return filepath.Join(dir, runID+".json") }
func lockPath(dir, runID string) string     { return filepath.Join(dir, runID+".lock") }

// openRunState takes this run's lock and publishes its manifest.
func openRunState(dir, scope, host, runID string) (*runState, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}
	lock, err := filelock.TryLock(lockPath(dir, runID))
	if err != nil {
		return nil, fmt.Errorf("locking run: %w", err)
	}
	s := &runState{dir: dir, lock: lock, manifest: runManifest{Scope: scope, Host: host, RunID: runID}}
	if err := writeManifest(dir, s.manifest); err != nil {
		_ = lock.Close() //nolint:errcheck // Prioritize returning the write error
		return nil, err
	}
	return s, nil
}

// addDaemon records that this run creates resources on the given daemon.
func (s *runState) addDaemon(daemonID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.manifest.Daemons, daemonID) {
		return nil
	}
	s.manifest.Daemons = append(s.manifest.Daemons, daemonID)
	return writeManifest(s.dir, s.manifest)
}

// close releases the run lock. The manifest is removed only when every
// resource of the run is known to be gone; otherwise it stays for recovery.
func (s *runState) close(clean bool) error {
	var errs []error
	if clean {
		if err := os.Remove(manifestPath(s.dir, s.manifest.RunID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("removing manifest: %w", err))
		}
	}
	return errors.Join(append(errs, s.lock.Close())...)
}

// writeManifest atomically replaces the manifest file.
func writeManifest(dir string, m runManifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, m.RunID+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()           //nolint:errcheck // Prioritize returning the write error
		_ = os.Remove(tmp.Name()) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := os.Rename(tmp.Name(), manifestPath(dir, m.RunID)); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// recoverAbandoned removes resources on the daemon behind c that were created
// by earlier runs of the same scope on this host whose process is gone.
//
// A run is only considered abandoned when its lock can be taken; a held lock
// means the run is still active. Missing or malformed records and lock errors
// mean ownership cannot be proven, so those runs are reported and skipped.
// Every resource is verified against the full ownership tuple before removal.
// Records of runs whose cleanup failed are kept so a later run can retry.
func recoverAbandoned(ctx context.Context, dir, scope, host, daemonID, ownRunID string, c client.DockerClient, warn func(string, ...any)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		warn("recovery: reading state directory %s: %v", dir, err)
		return
	}
	for _, entry := range entries {
		runID, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() || runID == ownRunID {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) // #nosec G304 -- path is inside the state directory
		if err != nil {
			warn("recovery: reading %s: %v", path, err)
			continue
		}
		var m runManifest
		if decodeErr := json.Unmarshal(data, &m); decodeErr != nil || m.RunID != runID {
			warn("recovery: skipping malformed record %s", path)
			continue
		}
		if m.Scope != scope || m.Host != host || !slices.Contains(m.Daemons, daemonID) {
			continue
		}

		lock, err := filelock.TryLock(lockPath(dir, runID))
		if errors.Is(err, filelock.ErrLocked) {
			continue // run is still active
		}
		if err != nil {
			warn("recovery: cannot prove ownership of run %s: %v", runID, err)
			continue
		}
		recoverRun(ctx, c, dir, m, daemonID, warn)
		if err := lock.Close(); err != nil {
			warn("recovery: releasing lock of run %s: %v", runID, err)
		}
	}
}

// recoverRun sweeps one abandoned run on one daemon while its lock is held and
// updates its manifest accordingly.
func recoverRun(ctx context.Context, c client.DockerClient, dir string, m runManifest, daemonID string, warn func(string, ...any)) {
	want := map[string]string{labelManaged: labelTrue, labelScope: m.Scope, labelHost: m.Host, labelRun: m.RunID}
	if err := sweepRun(ctx, c, want); err != nil {
		warn("recovery: run %s on daemon %s: %v", m.RunID, daemonID, err)
		return
	}
	m.Daemons = slices.DeleteFunc(m.Daemons, func(id string) bool { return id == daemonID })
	if len(m.Daemons) > 0 {
		if err := writeManifest(dir, m); err != nil {
			warn("recovery: updating record of run %s: %v", m.RunID, err)
		}
		return
	}
	if err := os.Remove(manifestPath(dir, m.RunID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		warn("recovery: removing record of run %s: %v", m.RunID, err)
	}
}
