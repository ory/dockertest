// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// WithPlatform selects the image platform, such as "linux/amd64" or
// "linux/arm64/v8", for pulling the image and creating the container. A
// locally present image for another platform is not used. Containers for
// different platforms are not reused for each other.
func WithPlatform(platform string) RunOption {
	return func(rc *runConfig) error {
		p, err := parsePlatform(platform)
		if err != nil {
			return err
		}
		rc.platform = p
		return nil
	}
}

// parsePlatform parses "os/arch" or "os/arch/variant".
func parsePlatform(s string) (*ocispec.Platform, error) {
	parts := strings.Split(s, "/")
	if len(parts) < 2 || len(parts) > 3 || slices.Contains(parts, "") {
		return nil, fmt.Errorf("%w: platform %q is not os/arch[/variant]", ErrInvalidOption, s)
	}
	p := &ocispec.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) == 3 {
		p.Variant = parts[2]
	}
	return p, nil
}

// WithRegistryAuth sets the credentials for pulling the image from a private
// registry. Images that are already present are not pulled again.
func WithRegistryAuth(auth registry.AuthConfig) RunOption { //nolint:gocritic // a value matches how callers build registry.AuthConfig
	return func(rc *runConfig) error {
		buf, err := json.Marshal(auth) //nolint:gosec // the daemon expects the credentials in the X-Registry-Auth header
		if err != nil {
			return fmt.Errorf("%w: registry auth: %w", ErrInvalidOption, err)
		}
		rc.registryAuth = base64.URLEncoding.EncodeToString(buf)
		return nil
	}
}
