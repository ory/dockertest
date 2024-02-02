// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package v1p19 provides specific API types for the API version 1, patch 19.
package v1p19 // import "github.com/ory/dockertest/v3/docker/types/versions/v1p19"

import (
	"github.com/docker/go-connections/nat"
	"github.com/ory/dockertest/v3/docker/types"
	"github.com/ory/dockertest/v3/docker/types/container"
	"github.com/ory/dockertest/v3/docker/types/versions/v1p20"
)

// ContainerJSON is a backcompatibility struct for APIs prior to 1.20.
// Note this is not used by the Windows daemon.
type ContainerJSON struct {
	*types.ContainerJSONBase
	Volumes         map[string]string
	VolumesRW       map[string]bool
	Config          *ContainerConfig
	NetworkSettings *v1p20.NetworkSettings
}

// ContainerConfig is a backcompatibility struct for APIs prior to 1.20.
type ContainerConfig struct {
	*container.Config

	MacAddress      string
	NetworkDisabled bool
	ExposedPorts    map[nat.Port]struct{}

	// backward compatibility, they now live in HostConfig
	VolumeDriver string
	Memory       int64
	MemorySwap   int64
	CPUShares    int64  `json:"CpuShares"`
	CPUSet       string `json:"Cpuset"`
}
