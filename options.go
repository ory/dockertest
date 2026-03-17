// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/ory/dockertest/v4/internal/client"
)

// PoolOption is a functional option for configuring a pool.
// Use with NewPool or NewPoolT to customize pool behavior.
type PoolOption func(*pool)

// WithMaxWait sets the maximum wait time for operations.
// The default maxWait is 60 seconds.
func WithMaxWait(d time.Duration) PoolOption {
	return func(p *pool) {
		p.maxWait = d
	}
}

// WithMobyClient sets a custom Docker client.
// When a custom client is provided, the pool will not close it on Close().
func WithMobyClient(c client.DockerClient) PoolOption {
	return func(p *pool) {
		p.client = c
		p.ownedClient = false
	}
}

// RunOption configures container creation.
// Use with Pool.Run or Pool.RunT to customize how containers are started.
type RunOption func(*runConfig) error

// runConfig holds container configuration.
//
//nolint:govet // field alignment traded for readability
type runConfig struct {
	env                []string
	cmd                []string
	entrypoint         []string
	binds              []string
	tag                string
	name               string
	reuseID            string
	user               string
	workingDir         string
	hostname           string
	labels             map[string]string
	portBindings       network.PortMap
	configModifier     func(*container.Config)
	hostConfigModifier func(*container.HostConfig)
	noReuse            bool
	noPull             bool // skip image pull (for locally built images)
}

// WithTag sets the image tag. Default is "latest".
func WithTag(tag string) RunOption {
	return func(rc *runConfig) error {
		rc.tag = tag
		return nil
	}
}

// WithEnv sets environment variables for the container.
// Each string should be in "KEY=value" format.
func WithEnv(env []string) RunOption {
	return func(rc *runConfig) error {
		rc.env = env
		return nil
	}
}

// WithCmd sets the container command, overriding the image's default CMD.
func WithCmd(cmd []string) RunOption {
	return func(rc *runConfig) error {
		rc.cmd = cmd
		return nil
	}
}

// WithEntrypoint sets the container entrypoint, overriding the image's default ENTRYPOINT.
func WithEntrypoint(entrypoint []string) RunOption {
	return func(rc *runConfig) error {
		rc.entrypoint = entrypoint
		return nil
	}
}

// WithReuseID sets a custom reuse ID for container reuse.
// By default, containers are reused based on "repository:tag".
func WithReuseID(id string) RunOption {
	return func(rc *runConfig) error {
		rc.reuseID = id
		return nil
	}
}

// WithoutReuse disables container reuse for this run.
// A new container will be created every time.
func WithoutReuse() RunOption {
	return func(rc *runConfig) error {
		rc.noReuse = true
		return nil
	}
}

// WithUser sets the user that will run commands inside the container.
// Supports both "user" and "user:group" formats.
func WithUser(user string) RunOption {
	return func(rc *runConfig) error {
		rc.user = user
		return nil
	}
}

// WithWorkingDir sets the working directory for commands run in the container.
func WithWorkingDir(dir string) RunOption {
	return func(rc *runConfig) error {
		rc.workingDir = dir
		return nil
	}
}

// WithLabels sets labels on the container.
// Labels are useful for marking test containers or adding metadata.
func WithLabels(labels map[string]string) RunOption {
	return func(rc *runConfig) error {
		rc.labels = labels
		return nil
	}
}

// WithHostname sets the container's hostname.
func WithHostname(hostname string) RunOption {
	return func(rc *runConfig) error {
		rc.hostname = hostname
		return nil
	}
}

// WithContainerConfig allows direct modification of the container.Config.
// This is useful for advanced options not covered by other WithXxx functions.
// The modifier is applied after all other options are processed.
func WithContainerConfig(modifier func(*container.Config)) RunOption {
	return func(rc *runConfig) error {
		rc.configModifier = modifier
		return nil
	}
}

// WithHostConfig allows direct modification of the container.HostConfig.
// Use this to set host-level options like port bindings, volume mounts,
// restart policies, memory/CPU limits, or AutoRemove.
// The modifier is applied after the default HostConfig is constructed.
func WithHostConfig(modifier func(*container.HostConfig)) RunOption {
	return func(rc *runConfig) error {
		rc.hostConfigModifier = modifier
		return nil
	}
}

// WithName sets the container name.
func WithName(name string) RunOption {
	return func(rc *runConfig) error {
		rc.name = name
		return nil
	}
}

// WithPortBindings sets explicit port bindings for the container.
// Use network.PortMap to specify the bindings.
func WithPortBindings(bindings network.PortMap) RunOption {
	return func(rc *runConfig) error {
		rc.portBindings = bindings
		return nil
	}
}

// WithMounts sets bind mounts for the container.
// Each string should be in "host:container" or "host:container:mode" format.
func WithMounts(binds []string) RunOption {
	return func(rc *runConfig) error {
		rc.binds = binds
		return nil
	}
}
