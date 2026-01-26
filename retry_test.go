// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRetry tests the Retry function with constant backoff.
func TestRetry(t *testing.T) {
	t.Run("succeeds after N attempts", func(t *testing.T) {
		ctx := t.Context()
		attempts := 0
		expectedAttempts := 3

		fn := func() error {
			attempts++
			if attempts < expectedAttempts {
				return errors.New("not yet")
			}
			return nil
		}

		err := Retry(ctx, 5*time.Second, 100*time.Millisecond, fn)
		if err != nil {
			t.Fatalf("Retry() error = %v, want nil", err)
		}

		if attempts != expectedAttempts {
			t.Errorf("attempts = %d, want %d", attempts, expectedAttempts)
		}
	})

	t.Run("succeeds on first attempt", func(t *testing.T) {
		ctx := t.Context()
		attempts := 0

		fn := func() error {
			attempts++
			return nil
		}

		err := Retry(ctx, 1*time.Second, 100*time.Millisecond, fn)
		if err != nil {
			t.Fatalf("Retry() error = %v, want nil", err)
		}

		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
	})

	t.Run("times out", func(t *testing.T) {
		ctx := t.Context()
		attempts := 0

		fn := func() error {
			attempts++
			return errors.New("persistent error")
		}

		err := Retry(ctx, 250*time.Millisecond, 50*time.Millisecond, fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Retry() error = %v, want context.DeadlineExceeded", err)
		}

		if attempts < 2 {
			t.Errorf("attempts = %d, want at least 2", attempts)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0

		fn := func() error {
			attempts++
			if attempts == 2 {
				cancel()
			}
			return errors.New("error")
		}

		err := Retry(ctx, 5*time.Second, 50*time.Millisecond, fn)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Retry() error = %v, want context.Canceled", err)
		}
	})
}

// TestRetryWithBackoff tests the RetryWithBackoff function with exponential backoff.
func TestRetryWithBackoff(t *testing.T) {
	t.Run("succeeds after N attempts", func(t *testing.T) {
		ctx := t.Context()
		attempts := 0
		expectedAttempts := 3

		fn := func() error {
			attempts++
			if attempts < expectedAttempts {
				return errors.New("not yet")
			}
			return nil
		}

		err := RetryWithBackoff(ctx, 5*time.Second, 50*time.Millisecond, 500*time.Millisecond, fn)
		if err != nil {
			t.Fatalf("RetryWithBackoff() error = %v, want nil", err)
		}

		if attempts != expectedAttempts {
			t.Errorf("attempts = %d, want %d", attempts, expectedAttempts)
		}
	})

	t.Run("times out", func(t *testing.T) {
		ctx := t.Context()
		attempts := 0

		fn := func() error {
			attempts++
			return errors.New("persistent error")
		}

		err := RetryWithBackoff(ctx, 250*time.Millisecond, 50*time.Millisecond, 500*time.Millisecond, fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("RetryWithBackoff() error = %v, want context.DeadlineExceeded", err)
		}

		if attempts < 2 {
			t.Errorf("attempts = %d, want at least 2", attempts)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		attempts := 0

		fn := func() error {
			attempts++
			if attempts == 2 {
				cancel()
			}
			return errors.New("error")
		}

		err := RetryWithBackoff(ctx, 5*time.Second, 50*time.Millisecond, 500*time.Millisecond, fn)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("RetryWithBackoff() error = %v, want context.Canceled", err)
		}
	})
}

// TestPoolRetry tests the Pool.Retry convenience method.
func TestPoolRetry(t *testing.T) {
	t.Run("succeeds after N attempts", func(t *testing.T) {
		ctx := t.Context()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v", err)
		}
		t.Cleanup(func() { pool.Close() })

		attempts := 0
		expectedAttempts := 3

		fn := func() error {
			attempts++
			if attempts < expectedAttempts {
				return errors.New("not yet")
			}
			return nil
		}

		err = pool.Retry(ctx, 5*time.Second, fn)
		if err != nil {
			t.Fatalf("Pool.Retry() error = %v, want nil", err)
		}

		if attempts != expectedAttempts {
			t.Errorf("attempts = %d, want %d", attempts, expectedAttempts)
		}
	})

	t.Run("times out", func(t *testing.T) {
		ctx := t.Context()
		pool, err := NewPool(ctx, "")
		if err != nil {
			t.Fatalf("NewPool() error = %v", err)
		}
		t.Cleanup(func() { pool.Close() })

		attempts := 0
		fn := func() error {
			attempts++
			return errors.New("persistent error")
		}

		err = pool.Retry(ctx, 2500*time.Millisecond, fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Pool.Retry() error = %v, want context.DeadlineExceeded", err)
		}

		if attempts < 2 {
			t.Errorf("attempts = %d, want at least 2", attempts)
		}
	})
}
