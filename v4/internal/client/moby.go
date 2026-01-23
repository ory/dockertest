// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"github.com/docker/docker/client"
)

// MobyClient wraps the official Moby client to implement our Client interface.
type MobyClient struct {
	*client.Client
}

// NewMobyClient creates a new Moby client with default options.
// It uses environment variables (DOCKER_HOST, etc.) for configuration.
func NewMobyClient() (*MobyClient, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &MobyClient{Client: c}, nil
}

// NewMobyClientWithOptions creates a new Moby client with custom options.
func NewMobyClientWithOptions(opts ...client.Opt) (*MobyClient, error) {
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &MobyClient{Client: c}, nil
}

// All interface methods are satisfied by embedding *client.Client
// The Moby client already implements all required methods with matching signatures
