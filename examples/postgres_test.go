// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"github.com/ory/dockertest/v4"
	"testing"
	"time"
)

func TestPostgreSQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	postgres := pool.RunT(t, "postgres",
		dockertest.WithTag("14-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=testdb",
		}),
	)
	// Open connection outside retry loop to avoid leaking connection pools
	dsn := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable",
		postgres.GetHostPort("5432/tcp"))
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	// Wait for PostgreSQL to be ready
	err = pool.Retry(t.Context(), 30*time.Second, func() error {
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to PostgreSQL: %v", err)
	}

	// Run a query
	var version string
	err = db.QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("PostgreSQL version: %s", version)
}
