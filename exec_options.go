// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

// execConfig holds configuration for exec operations.
type execConfig struct {
	env          []string
	workingDir   string
	privileged   bool
	user         string
	attachStdout bool
	attachStderr bool
}

// ExecOption configures an exec operation.
type ExecOption func(*execConfig) error

// WithExecEnv sets environment variables for the exec.
func WithExecEnv(env []string) ExecOption {
	return func(c *execConfig) error {
		c.env = env
		return nil
	}
}

// WithExecWorkingDir sets the working directory for the exec.
func WithExecWorkingDir(dir string) ExecOption {
	return func(c *execConfig) error {
		c.workingDir = dir
		return nil
	}
}

// WithExecPrivileged runs the exec in privileged mode.
func WithExecPrivileged(privileged bool) ExecOption {
	return func(c *execConfig) error {
		c.privileged = privileged
		return nil
	}
}

// WithExecUser sets the user for the exec.
func WithExecUser(user string) ExecOption {
	return func(c *execConfig) error {
		c.user = user
		return nil
	}
}

func newExecConfig() *execConfig {
	return &execConfig{
		attachStdout: true,
		attachStderr: true,
	}
}
