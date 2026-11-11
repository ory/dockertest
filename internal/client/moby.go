// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"
	"strings"

	mobyclient "github.com/moby/moby/client"
)

// NewMobyClient creates a new Docker client from environment variables.
// It uses DOCKER_HOST, DOCKER_API_VERSION, DOCKER_CERT_PATH, and DOCKER_TLS_VERIFY.
func NewMobyClient(ctx context.Context) (DockerClient, error) {
	// Request the continue handshake to avoid stalls during chunked context uploads.
	c, err := mobyclient.New(mobyclient.FromEnv, mobyclient.WithHTTPRequestHook(func(req *http.Request) error {
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/build") {
			req.Header.Set("Expect", "100-continue")
		}
		return nil
	}))
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
