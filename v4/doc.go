// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

// Package dockertest provides tools to test against Docker containers.
//
// It uses the official Moby client and provides a modern, context-aware API
// with automatic container reuse for faster test execution.
//
// Planned usage (API under development):
//
//	func TestMain(m *testing.M) {
//	    defer dockertest.Cleanup()
//	    code := m.Run()
//	    os.Exit(code)
//	}
//
//	func TestDatabase(t *testing.T) {
//	    pool, err := dockertest.NewPool("")
//	    require.NoError(t, err)
//
//	    db := pool.RunT(t, "postgres",
//	        dockertest.WithTag("14"),
//	        dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
//	    )
//	    // Container automatically reused across tests
//	    // Automatic cleanup via Cleanup()
//	}
package dockertest
