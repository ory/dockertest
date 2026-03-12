// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ory/dockertest/v4"
)

func TestMountebank(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	mb := pool.RunT(t, "bbyars/mountebank",
		dockertest.WithTag("2.9.1"),
		dockertest.WithCmd([]string{"--allowInjection"}),
		dockertest.WithoutReuse(),
	)
	mb.Cleanup(t)

	adminURL := fmt.Sprintf("http://%s", mb.GetHostPort("2525/tcp"))

	err := pool.Retry(t.Context(), 30*time.Second, func() error {
		resp, err := http.Get(adminURL + "/imposters")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("mountebank not ready: status %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Could not connect to Mountebank: %v", err)
	}

	t.Logf("Mountebank admin API available at %s", adminURL)
}
