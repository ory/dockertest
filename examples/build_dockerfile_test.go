// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4"
)

func TestBuildDockerfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")
	// Built images are not removed automatically. This cleanup runs after the
	// container's, so the image is no longer in use.
	c, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("image cleanup client: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		t.Cleanup(cancel)
		if _, rmErr := c.ImageRemove(ctx, "dockertest-build-example:test", mobyclient.ImageRemoveOptions{PruneChildren: true}); rmErr != nil {
			t.Errorf("remove built image: %v", rmErr)
		}
	})

	resource := pool.BuildAndRunT(t, "dockertest-build-example:test",
		&dockertest.BuildOptions{
			ContextDir: "testdata",
			Dockerfile: "Dockerfile",
		},
	)
	dsn := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable",
		resource.GetHostPort("5432/tcp"))
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	err = pool.Retry(t.Context(), 30*time.Second, func() error {
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to built image: %v", err)
	}

	var version string
	err = db.QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("Built image PostgreSQL version: %s", version)
}
