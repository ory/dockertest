// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"testing"
	"time"

	mobyclient "github.com/moby/moby/client"
	"github.com/ory/dockertest/v4/internal/client"
)

func TestNewPool(t *testing.T) {
	t.Run("creates pool with default options", func(t *testing.T) {
		ctx := context.Background()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		if pool == nil {
			t.Fatal("NewPool() pool = nil, want non-nil")
		}
		defer pool.Close()

		if pool.MaxWait != 60*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 60*time.Second)
		}
		if pool.client == nil {
			t.Error("pool.client = nil, want non-nil")
		}
	})

	t.Run("creates pool with custom MaxWait", func(t *testing.T) {
		ctx := context.Background()
		pool, err := NewPool(ctx, "", WithMaxWait(30*time.Second))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		defer pool.Close()

		if pool.MaxWait != 30*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 30*time.Second)
		}
	})

	t.Run("creates pool with custom client", func(t *testing.T) {
		ctx := context.Background()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		defer customClient.Close()

		pool, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		defer pool.Close()

		if pool.client != customClient {
			t.Error("pool.client != customClient, want same client")
		}
	})

	t.Run("custom client option prevents client creation", func(t *testing.T) {
		ctx := context.Background()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		defer customClient.Close()

		// Use invalid endpoint - should not fail because custom client is used
		pool, err := NewPool(ctx, "invalid://endpoint", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil (custom client should bypass endpoint)", err)
		}
		defer pool.Close()
	})
}

func TestNewPoolT(t *testing.T) {
	t.Run("creates pool with t.Context and t.Cleanup", func(t *testing.T) {
		pool := NewPoolT(t, "")
		if pool == nil {
			t.Fatal("NewPoolT() pool = nil, want non-nil")
		}

		// t.Cleanup should be registered, so we can't test it directly
		// but we can verify the pool is functional
		if pool.client == nil {
			t.Error("pool.client = nil, want non-nil")
		}
	})

	t.Run("accepts options", func(t *testing.T) {
		pool := NewPoolT(t, "", WithMaxWait(45*time.Second))
		if pool.MaxWait != 45*time.Second {
			t.Errorf("pool.MaxWait = %v, want %v", pool.MaxWait, 45*time.Second)
		}
	})
}

func TestPoolClose(t *testing.T) {
	t.Run("closes client", func(t *testing.T) {
		ctx := context.Background()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		err = pool.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	t.Run("does not close custom client", func(t *testing.T) {
		ctx := context.Background()
		customClient, err := client.NewMobyClient(ctx)
		if err != nil {
			t.Fatalf("NewMobyClient() error = %v, want nil", err)
		}
		defer customClient.Close()

		pool, err := NewPool(ctx, "", WithMobyClient(customClient))
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}

		// Close pool - should not close custom client
		err = pool.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}

		// Verify custom client is still usable by pinging
		ctx2 := context.Background()
		_, pingErr := customClient.Ping(ctx2, mobyclient.PingOptions{})
		if pingErr != nil {
			t.Error("custom client was closed by pool.Close(), want it to remain open")
		}
	})
}

func TestPoolCleanup(t *testing.T) {
	t.Run("cleanup with empty registry", func(t *testing.T) {
		ResetRegistry()
		ctx := context.Background()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v, want nil", err)
		}
		defer pool.Close()

		err = pool.Cleanup(ctx)
		if err != nil {
			t.Errorf("Cleanup() error = %v, want nil", err)
		}
	})
}
