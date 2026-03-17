// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/ory/dockertest/v4"
)

func TestCassandra(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	cassandra := pool.RunT(t, "cassandra",
		dockertest.WithTag("4"),
		dockertest.WithEnv([]string{
			"CASSANDRA_AUTHENTICATOR=PasswordAuthenticator",
		}),
	)
	host := cassandra.GetBoundIP("9042/tcp")
	port, err := strconv.Atoi(cassandra.GetPort("9042/tcp"))
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	err = pool.Retry(t.Context(), 120*time.Second, func() error {
		cluster := gocql.NewCluster(host)
		cluster.Port = port
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: "cassandra",
			Password: "cassandra",
		}
		cluster.ProtoVersion = 4

		session, err := cluster.CreateSession()
		if err != nil {
			return err
		}
		session.Close()
		return nil
	})
	if err != nil {
		t.Fatalf("Could not connect to Cassandra: %v", err)
	}

	t.Logf("Cassandra available at %s:%d", host, port)
}
