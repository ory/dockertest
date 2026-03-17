// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
)

// Retry retries fn with a fixed interval until it succeeds, the context is cancelled,
// or the timeout is reached. The interval is deterministic (no jitter).
func Retry(ctx context.Context, timeout, interval time.Duration, fn func() error) error {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Configure constant backoff
	b := backoff.NewConstantBackOff(interval)

	// Wrap fn to match backoff.Operation signature
	operation := func() (struct{}, error) {
		return struct{}{}, fn()
	}

	_, err := backoff.Retry(ctx, operation, backoff.WithBackOff(b))
	return err
}

// RetryWithBackoff retries fn with exponential backoff until it succeeds, the context
// is cancelled, or the timeout is reached.
//
// The interval starts at initialInterval and doubles after each attempt, capped at maxInterval.
// The backoff is deterministic (no jitter).
func RetryWithBackoff(ctx context.Context, timeout, initialInterval, maxInterval time.Duration, fn func() error) error {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Configure exponential backoff with no randomization (deterministic)
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = initialInterval
	b.MaxInterval = maxInterval
	b.Multiplier = 2.0
	b.RandomizationFactor = 0 // No jitter, deterministic
	b.Reset()

	// Wrap fn to match backoff.Operation signature
	operation := func() (struct{}, error) {
		return struct{}{}, fn()
	}

	_, err := backoff.Retry(ctx, operation, backoff.WithBackOff(b))
	return err
}

// Retry is a convenience method that wraps the package-level Retry function.
// If timeout is 0, pool.maxWait is used as the default. The interval is fixed at 1 second.
func (p *pool) Retry(ctx context.Context, timeout time.Duration, fn func() error) error {
	if timeout == 0 {
		timeout = p.maxWait
	}
	return Retry(ctx, timeout, 1*time.Second, fn)
}
