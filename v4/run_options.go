// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

// runConfig holds the configuration for running a container.
type runConfig struct {
	// Basic options
	name       string
	tag        string
	env        []string
	cmd        []string
	privileged bool
	platform   string
	tty        bool

	// Port options
	exposedPorts []string
	portBindings nat.PortMap

	// Volume options
	mounts []mount.Mount

	// Network options
	networks []*Network

	// Label options
	labels map[string]string

	// Reuse options
	reuseID   string
	noReuse   bool
	expiry    time.Duration
	hasExpiry bool
	noExpiry  bool
}

// RunOption configures container creation.
type RunOption func(*runConfig) error

// WithName sets the container name.
func WithName(name string) RunOption {
	return func(c *runConfig) error {
		c.name = name
		return nil
	}
}

// WithTag sets the image tag. Default is "latest".
func WithTag(tag string) RunOption {
	return func(c *runConfig) error {
		c.tag = tag
		return nil
	}
}

// WithEnv sets environment variables for the container.
func WithEnv(env []string) RunOption {
	return func(c *runConfig) error {
		c.env = env
		return nil
	}
}

// WithCmd sets the command to run in the container.
func WithCmd(cmd []string) RunOption {
	return func(c *runConfig) error {
		c.cmd = cmd
		return nil
	}
}

// WithExposedPorts exposes the given ports in the container.
// Ports should be in the format "port/protocol", e.g., "5432/tcp".
// If protocol is omitted, "tcp" is assumed.
func WithExposedPorts(ports ...string) RunOption {
	return func(c *runConfig) error {
		c.exposedPorts = ports
		return nil
	}
}

// WithPortBindings sets custom port bindings for the container.
func WithPortBindings(bindings nat.PortMap) RunOption {
	return func(c *runConfig) error {
		c.portBindings = bindings
		return nil
	}
}

// WithMounts sets volume mounts for the container.
func WithMounts(mounts []mount.Mount) RunOption {
	return func(c *runConfig) error {
		c.mounts = mounts
		return nil
	}
}

// WithNetworks connects the container to the given networks.
func WithNetworks(networks ...*Network) RunOption {
	return func(c *runConfig) error {
		c.networks = networks
		return nil
	}
}

// WithLabels sets labels on the container.
func WithLabels(labels map[string]string) RunOption {
	return func(c *runConfig) error {
		c.labels = labels
		return nil
	}
}

// WithPrivileged runs the container in privileged mode.
func WithPrivileged(privileged bool) RunOption {
	return func(c *runConfig) error {
		c.privileged = privileged
		return nil
	}
}

// WithPlatform sets the platform for the container.
// Example: "linux/amd64", "linux/arm64"
func WithPlatform(platform string) RunOption {
	return func(c *runConfig) error {
		c.platform = platform
		return nil
	}
}

// WithTTY allocates a pseudo-TTY for the container.
func WithTTY(tty bool) RunOption {
	return func(c *runConfig) error {
		c.tty = tty
		return nil
	}
}

// WithReuseID sets a custom reuse ID for the container.
// By default, containers are reused by "repository:tag".
// Use this when you need multiple containers of the same image.
func WithReuseID(id string) RunOption {
	return func(c *runConfig) error {
		c.reuseID = id
		return nil
	}
}

// WithoutReuse disables container reuse for this container.
// The container will be created fresh and not registered for reuse.
func WithoutReuse() RunOption {
	return func(c *runConfig) error {
		c.noReuse = true
		return nil
	}
}

// WithExpiry sets a custom expiry duration for the container.
// Default is DefaultExpiry (10 minutes).
func WithExpiry(d time.Duration) RunOption {
	return func(c *runConfig) error {
		c.expiry = d
		c.hasExpiry = true
		return nil
	}
}

// WithoutExpiry disables automatic expiry for the container.
// Use with caution - container will persist until explicitly removed.
func WithoutExpiry() RunOption {
	return func(c *runConfig) error {
		c.noExpiry = true
		return nil
	}
}

// newRunConfig creates a runConfig with defaults.
func newRunConfig() *runConfig {
	return &runConfig{
		tag:    "latest",
		labels: make(map[string]string),
	}
}
