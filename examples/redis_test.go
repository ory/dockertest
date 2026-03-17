// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"github.com/ory/dockertest/v4"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := dockertest.NewPoolT(t, "")

	redisContainer := pool.RunT(t, "redis",
		dockertest.WithTag("7-alpine"),
	)
	// Create Redis client
	addr := redisContainer.GetHostPort("6379/tcp")
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	t.Cleanup(func() {
		client.Close()
	})

	// Wait for Redis to be ready
	ctx := t.Context()
	err := pool.Retry(ctx, 30*time.Second, func() error {
		return client.Ping(ctx).Err()
	})
	if err != nil {
		t.Fatalf("Could not connect to Redis: %v", err)
	}

	// Test SET/GET
	err = client.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		t.Fatalf("SET failed: %v", err)
	}

	val, err := client.Get(ctx, "test-key").Result()
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	if val != "test-value" {
		t.Errorf("GET returned %q, want %q", val, "test-value")
	}

	t.Logf("Redis test successful: %s", val)
}
