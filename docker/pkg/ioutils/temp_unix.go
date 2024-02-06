// Copyright © 2023 Ory Corp
// SPDX-License-Identifier: Apache-2.0

//go:build !windows
// +build !windows

package ioutils // import "github.com/ory/dockertest/v3/docker/pkg/ioutils"

import "os"

// TempDir on Unix systems is equivalent to ioutil.TempDir.
func TempDir(dir, prefix string) (string, error) {
	return os.MkdirTemp(dir, prefix)
}
