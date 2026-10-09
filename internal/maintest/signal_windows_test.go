// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package maintest

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// setupProcessGroup puts the child in its own console process group so that
// a Ctrl+Break event can be targeted at it without hitting the test runner.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// interrupt delivers Ctrl+Break, which os/signal reports as os.Interrupt.
func interrupt(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid)); err != nil { // #nosec G115 -- PIDs fit in uint32 on Windows
		t.Fatalf("sending Ctrl+Break: %v", err)
	}
}

// terminate has no portable equivalent on Windows: SIGTERM is only delivered
// for console close, logoff, and shutdown events.
func terminate(t *testing.T, _ *exec.Cmd) {
	t.Skip("SIGTERM cannot be sent to a process on Windows")
}
