// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/ory/dockertest/v4"
)

func TestMinio(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	resource := pool.RunT(t, "minio/minio",
		dockertest.WithTag("latest"),
		dockertest.WithCmd([]string{"server", "/data"}),
		dockertest.WithEnv([]string{
			"MINIO_ROOT_USER=minioadmin",
			"MINIO_ROOT_PASSWORD=minioadmin",
		}),
	)
	endpoint := resource.GetHostPort("9000/tcp")

	// Wait for MinIO to be ready via health endpoint
	err := pool.Retry(t.Context(), 30*time.Second, func() error {
		resp, err := http.Get(fmt.Sprintf("http://%s/minio/health/live", endpoint))
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("minio not ready: status %d", resp.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Could not connect to MinIO: %v", err)
	}

	ctx := t.Context()
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio.New failed: %v", err)
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}

	t.Logf("MinIO connected, found %d buckets", len(buckets))
}
