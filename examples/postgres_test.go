// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	dockertest "github.com/ory/dockertest/v4"
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
	postgres.Cleanup(t)

	// Wait for PostgreSQL to be ready
	var db *sql.DB
	err := pool.Retry(t.Context(), 30*time.Second, func() error {
		var err error
		dsn := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable",
			postgres.GetHostPort("5432/tcp"))
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	// Run a query
	var version string
	err = db.QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("PostgreSQL version: %s", version)
}
