// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	dockertest "github.com/ory/dockertest/v4"
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

	// Wait for MySQL to be ready
	var db *sql.DB
	err := pool.Retry(t.Context(), 30*time.Second, func() error {
		var err error
		dsn := fmt.Sprintf("root:secret@tcp(%s)/testdb",
			mysql.GetHostPort("3306/tcp"))
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return err
		}
		return db.Ping()
	})
	if err != nil {
		t.Fatalf("Could not connect to MySQL: %v", err)
	}
	defer db.Close()

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
