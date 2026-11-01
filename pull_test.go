// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/registry"
	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
)

func TestRunPullsWithRegistryAuthAndPlatform(t *testing.T) {
	stopPull := errors.New("stop after inspecting pull request")
	var pullQuery, pullAuth string
	c, err := mobyclient.New(
		mobyclient.WithAPIVersion("1.54"),
		mobyclient.WithHTTPClient(&http.Client{
			Transport: buildRoundTripper(func(req *http.Request) (*http.Response, error) {
				switch {
				case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/json"):
					// The image is not present, so Run pulls it.
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"message":"no such image"}`)),
						Request:    req,
					}, nil
				case req.Method == http.MethodPost && req.URL.Path == "/v1.54/images/create":
					pullQuery = req.URL.RawQuery
					pullAuth = req.Header.Get(registry.AuthHeader)
					return nil, stopPull
				}
				t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, stopPull
			}),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	pool := dockertest.NewPoolT(t, "", dockertest.WithMobyClient(c))
	auth := registry.AuthConfig{Username: "user", Password: "secret", ServerAddress: "registry.example.com"}
	_, err = pool.Run(t.Context(), "registry.example.com/team/app",
		dockertest.WithTag("1"),
		dockertest.WithRegistryAuth(auth),
		dockertest.WithPlatform("linux/amd64"),
	)
	if !errors.Is(err, stopPull) {
		t.Fatalf("Run error = %v, want %v", err, stopPull)
	}

	raw, err := base64.URLEncoding.DecodeString(pullAuth)
	if err != nil {
		t.Fatalf("decoding %s header %q: %v", registry.AuthHeader, pullAuth, err)
	}
	var got registry.AuthConfig
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decoding auth config %s: %v", raw, err)
	}
	if got != auth {
		t.Fatalf("pull auth = %+v, want %+v", got, auth)
	}
	if !strings.Contains(pullQuery, "platform=linux%2Famd64") {
		t.Fatalf("pull query %q has no linux/amd64 platform", pullQuery)
	}
}
