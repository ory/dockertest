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
	const missing = `{"message":"no such image"}`
	for _, tc := range []struct {
		name, api   string
		localImage  string // inspect response body; missing means 404
		wantInspect []string
		wantPull    bool
	}{
		{name: "missing image", api: "1.54", localImage: missing, wantPull: true,
			wantInspect: []string{"platform"}},
		// API 1.44 cannot inspect by platform, so the local platform is compared.
		{name: "old API missing image", api: "1.44", localImage: missing, wantPull: true,
			wantInspect: []string{""}},
		{name: "old API other platform", api: "1.44", localImage: `{"Id":"sha256:a","Os":"linux","Architecture":"arm64"}`, wantPull: true,
			wantInspect: []string{""}},
		{name: "old API same platform", api: "1.44", localImage: `{"Id":"sha256:a","Os":"linux","Architecture":"amd64"}`,
			wantInspect: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopPull := errors.New("stop after inspecting pull request")
			stopCreate := errors.New("stop at container create")
			var inspects []string
			var pulled bool
			var pullQuery, pullAuth string
			c, err := mobyclient.New(
				mobyclient.WithAPIVersion(tc.api),
				mobyclient.WithHTTPClient(&http.Client{
					Transport: buildRoundTripper(func(req *http.Request) (*http.Response, error) {
						switch {
						case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/json"):
							inspects = append(inspects, req.URL.Query().Get("platform"))
							status := http.StatusOK
							if tc.localImage == missing {
								status = http.StatusNotFound
							}
							return &http.Response{
								StatusCode: status,
								Header:     http.Header{"Content-Type": {"application/json"}},
								Body:       io.NopCloser(strings.NewReader(tc.localImage)),
								Request:    req,
							}, nil
						case req.Method == http.MethodPost && req.URL.Path == "/v"+tc.api+"/images/create":
							pulled = true
							pullQuery = req.URL.RawQuery
							pullAuth = req.Header.Get(registry.AuthHeader)
							return nil, stopPull
						case req.Method == http.MethodPost && req.URL.Path == "/v"+tc.api+"/containers/create":
							return nil, stopCreate
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
			if want := map[bool]error{true: stopPull, false: stopCreate}[tc.wantPull]; !errors.Is(err, want) {
				t.Fatalf("Run error = %v, want %v", err, want)
			}
			if len(inspects) != len(tc.wantInspect) || (tc.wantInspect[0] == "") != (inspects[0] == "") {
				t.Fatalf("inspect platform queries = %q, want %q", inspects, tc.wantInspect)
			}
			if pulled != tc.wantPull || !pulled {
				return
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
		})
	}
}
