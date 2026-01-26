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

func TestCockroachDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	cockroach := pool.RunT(t, "cockroachdb/cockroach",
		dockertest.WithTag("v24.3.2"),
		dockertest.WithCmd([]string{"start-single-node", "--insecure"}),
	)
	cockroach.Cleanup(t)

	// Wait for CockroachDB to be ready
	var db *sql.DB
	err := pool.Retry(t.Context(), 30*time.Second, func() error {
		var err error
		dsn := fmt.Sprintf("postgres://root@%s/defaultdb?sslmode=disable",
			cockroach.GetHostPort("26257/tcp"))
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to CockroachDB: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	// Create a table and insert data
	_, err = db.Exec("CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(50))")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	_, err = db.Exec("INSERT INTO users (id, name) VALUES (1, 'Alice')")
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Query data
	var name string
	err = db.QueryRow("SELECT name FROM users WHERE id = 1").Scan(&name)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if name != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", name)
	}

	t.Logf("CockroachDB test successful: retrieved name '%s'", name)
}
