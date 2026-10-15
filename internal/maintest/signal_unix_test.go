// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package maintest

import (
	"os/exec"
	"syscall"
	"testing"
)

func setupProcessGroup(*exec.Cmd) {}

func interrupt(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}
}

func terminate(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}
}
