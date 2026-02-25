// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/ory/dockertest/v4"
)

func TestMultipleContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	net := pool.CreateNetworkT(t, "dockertest-multi-example", nil)
	t.Cleanup(func() {
		net.CloseT(t)
	})

	// Start two PostgreSQL containers on the same network
	pg1 := pool.RunT(t, "postgres",
		dockertest.WithTag("14-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=db1",
		}),
		dockertest.WithoutReuse(),
	)
	pg1.Cleanup(t)

	pg2 := pool.RunT(t, "postgres",
		dockertest.WithTag("14-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=db2",
		}),
		dockertest.WithoutReuse(),
	)
	pg2.Cleanup(t)

	// Connect both containers to the shared network
	if err := pg1.ConnectToNetwork(t.Context(), net); err != nil {
		t.Fatalf("ConnectToNetwork pg1 failed: %v", err)
	}
	if err := pg2.ConnectToNetwork(t.Context(), net); err != nil {
		t.Fatalf("ConnectToNetwork pg2 failed: %v", err)
	}

	// Verify each container has an IP in the network
	ip1 := pg1.GetIPInNetwork(net)
	ip2 := pg2.GetIPInNetwork(net)
	if ip1 == "" {
		t.Fatal("pg1 has no IP in network")
	}
	if ip2 == "" {
		t.Fatal("pg2 has no IP in network")
	}
	t.Logf("pg1 IP: %s, pg2 IP: %s", ip1, ip2)

	// Wait for both databases via host ports
	for _, tc := range []struct {
		name     string
		resource *dockertest.Resource
		dbName   string
	}{
		{"pg1", pg1, "db1"},
		{"pg2", pg2, "db2"},
	} {
		dsn := fmt.Sprintf("postgres://postgres:secret@%s/%s?sslmode=disable",
			tc.resource.GetHostPort("5432/tcp"), tc.dbName)
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("sql.Open %s failed: %v", tc.name, err)
		}
		t.Cleanup(func() {
			db.Close()
		})

		err = pool.Retry(t.Context(), 30*time.Second, func() error {
			return db.Ping()
		})
		if err != nil {
			t.Fatalf("Could not connect to %s: %v", tc.name, err)
		}

		t.Logf("%s (%s) is ready", tc.name, tc.dbName)
	}
}
