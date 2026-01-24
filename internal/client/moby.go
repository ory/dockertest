// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"

	mobyclient "github.com/moby/moby/client"
)

// NewMobyClient creates a new Docker client from environment variables.
// It uses DOCKER_HOST, DOCKER_API_VERSION, DOCKER_CERT_PATH, and DOCKER_TLS_VERIFY.
func NewMobyClient(ctx context.Context) (DockerClient, error) {
	c, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		return nil, err
	}

	// Ping to verify connection
	if _, err := c.Ping(ctx, mobyclient.PingOptions{}); err != nil {
		_ = c.Close() //nolint:errcheck // Prioritize returning the ping error
		return nil, err
	}

	return c, nil
}
