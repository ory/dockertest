// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// The lock covers the first byte of the file; the file content is never used.
const lockRange = 1

func lock(f *os.File) error {
	var ol windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, lockRange, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return ErrLocked
	}
	return err
}

func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockRange, 0, &ol)
}
