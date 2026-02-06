// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/ory/dockertest/v4"
	"testing"
	"time"
)

func TestMySQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	mysql := pool.RunT(t, "mysql",
		dockertest.WithTag("8"),
		dockertest.WithEnv([]string{
			"MYSQL_ROOT_PASSWORD=secret",
			"MYSQL_DATABASE=testdb",
		}),
	)
	mysql.Cleanup(t)

	// Open connection outside retry loop to avoid leaking connection pools
	dsn := fmt.Sprintf("root:secret@tcp(%s)/testdb",
		mysql.GetHostPort("3306/tcp"))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	// Wait for MySQL to be ready
	err = pool.Retry(t.Context(), 30*time.Second, func() error {
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to MySQL: %v", err)
	}

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

	t.Logf("MySQL test successful: retrieved name '%s'", name)
}
