// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package filelock_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ory/dockertest/v4/internal/filelock"
)

func TestTryLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	first, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	t.Cleanup(func() { first.Close() })

	if _, err := filelock.TryLock(path); !errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("second TryLock() error = %v, want ErrLocked", err)
	}
}

func TestCloseReleasesLockAndKeepsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	first, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("lock file removed after Close: %v", statErr)
	}

	second, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock() after Close error = %v, want nil", err)
	}
	t.Cleanup(func() { second.Close() })
}

func TestTryLockMissingDirectory(t *testing.T) {
	_, err := filelock.TryLock(filepath.Join(t.TempDir(), "missing", "run.lock"))
	if err == nil {
		t.Fatal("TryLock() error = nil, want error for missing directory")
	}
	if errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("TryLock() error = %v, want a non-ErrLocked error", err)
	}
}
