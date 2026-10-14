// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"errors"
	"io"
	"net/http"
	"testing"

	mobyclient "github.com/moby/moby/client"
	dockertest "github.com/ory/dockertest/v4"
)

type buildRoundTripper func(*http.Request) (*http.Response, error)

func (fn buildRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestBuildAndRunBuilderVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "daemon default"},
		{name: "classic", version: "1"},
		{name: "BuildKit", version: "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buildCalled := false
			stopBuild := errors.New("stop after inspecting build request")
			c, err := mobyclient.New(
				mobyclient.WithAPIVersion("1.54"),
				mobyclient.WithHTTPClient(&http.Client{
					Transport: buildRoundTripper(func(req *http.Request) (*http.Response, error) {
						if req.Method != http.MethodPost || req.URL.Path != "/v1.54/build" {
							t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
							return nil, stopBuild
						}
						buildCalled = true
						query := req.URL.Query()
						if got := query.Get("version"); got != tc.version {
							t.Errorf("builder version = %q, want %q", got, tc.version)
						}
						if tc.version == "" && query.Has("version") {
							t.Error("default builder must omit the version query parameter")
						}
						defer req.Body.Close()
						if _, err := io.Copy(io.Discard, req.Body); err != nil {
							t.Errorf("reading build context: %v", err)
						}
						// Stop before container creation: this test only exercises the build request.
						return nil, stopBuild
					}),
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Close() })

			pool := dockertest.NewPoolT(t, "", dockertest.WithMobyClient(c))
			_, err = pool.BuildAndRun(t.Context(), "test-builder-version", &dockertest.BuildOptions{
				ContextDir: t.TempDir(),
				Version:    tc.version,
			})
			if !errors.Is(err, stopBuild) {
				t.Fatalf("BuildAndRun error = %v, want %v", err, stopBuild)
			}
			if !buildCalled {
				t.Fatal("build request was not sent")
			}
		})
	}
}
