// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package filelock provides a non-blocking, exclusive, process-scoped file
// lock backed by flock on Unix-like systems and LockFileEx on Windows.
package filelock

import (
	"errors"
	"fmt"
	"os"
)

// ErrLocked is returned by TryLock when another open file description already
// holds the lock.
var ErrLocked = errors.New("file is locked")

// Lock is a held exclusive lock. The lock file itself is never removed so that
// concurrent lockers can never race on unlink and re-creation.
type Lock struct {
	f *os.File
}

// TryLock opens (creating if necessary) the file at path and tries to take an
// exclusive lock without blocking. It returns ErrLocked when the lock is held
// elsewhere, including by another descriptor in the same process.
func TryLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lock path is chosen by the caller
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		_ = f.Close() //nolint:errcheck // Prioritize returning the lock error
		if errors.Is(err, ErrLocked) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, path)
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Close releases the lock and closes the file. The file stays on disk.
func (l *Lock) Close() error {
	return errors.Join(unlock(l.f), l.f.Close())
}
