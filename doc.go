// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package dockertest provides Docker container orchestration for Go testing.
//
// Dockertest v4 uses the official github.com/moby/moby/client and provides
// a modern, context-aware API with automatic container reuse for fast tests.
//
// # Quick Start
//
//	func TestDatabase(t *testing.T) {
//	    pool := dockertest.NewPoolT(t, "")
//	    db := pool.RunT(t, "postgres",
//	        dockertest.WithTag("14"),
//	        dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
//	    )
//	    db.Cleanup(t)
//
//	    hostPort := db.GetHostPort("5432/tcp")
//	    connStr := fmt.Sprintf("postgres://postgres:secret@%s/postgres?sslmode=disable", hostPort)
//
//	    err := pool.Retry(t.Context(), 30*time.Second, func() error {
//	        conn, err := sql.Open("postgres", connStr)
//	        if err != nil {
//	            return err
//	        }
//	        defer conn.Close()
//	        return conn.Ping()
//	    })
//	    if err != nil {
//	        t.Fatalf("Database not ready: %v", err)
//	    }
//	}
//
// # Features
//
// - Container reuse: Containers are reused by default based on repository:tag for 2-3x faster tests
// - Context support: All operations accept context.Context for cancellation and timeouts
// - Test helpers: *T methods use t.Context() and t.Cleanup() for simplified test lifecycle
// - Networks: Create networks for container-to-container communication
// - Custom images: Build and run custom Docker images from Dockerfiles
//
// See https://github.com/ory/dockertest for more examples and documentation.
package dockertest
