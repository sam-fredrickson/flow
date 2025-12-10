// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"math/rand/v2"
	"time"
)

// A RetryPredicate determines whether a failed step should be retried.
//
// It receives the context, the number of attempts so far, and the error
// from the last attempt. It returns true to retry, false to stop.
type RetryPredicate = func(context.Context, int, error) bool

// BackoffOption configures backoff behavior for retry predicates.
type BackoffOption func(*backoffConfig)

// backoffConfig holds configuration for backoff strategies.
type backoffConfig struct {
	fullJitter    bool
	percentJitter float64       // 0 means no percentage jitter
	maxDelay      time.Duration // 0 means no max delay
	multiplier    float64       // for exponential backoff (default 2.0)
}

// WithFullJitter applies full jitter to backoff delays.
//
// The actual delay will be a random value between 0 and the calculated delay.
// This is the AWS-recommended approach for maximum desynchronization and is
// effective at preventing thundering herd problems in distributed systems.
//
// Cancels out any [WithPercentageJitter] option if both are provided.
// The last option in the list takes precedence.
//
// Applies to both [FixedBackoff] and [ExponentialBackoff].
func WithFullJitter() BackoffOption {
	return func(c *backoffConfig) {
		c.fullJitter = true
		c.percentJitter = 0
	}
}

// WithPercentageJitter applies percentage-based jitter to backoff delays.
//
// For example, WithPercentageJitter(0.2) adds ±20% randomness to the delay.
// The actual delay will be between (delay × (1-percent)) and (delay × (1+percent)).
//
// This provides more predictable retry timing than full jitter while still
// helping to desynchronize retries across multiple clients.
//
// Cancels out any [WithFullJitter] option if both are provided.
// The last option in the list takes precedence.
//
// Applies to both [FixedBackoff] and [ExponentialBackoff].
func WithPercentageJitter(percent float64) BackoffOption {
	return func(c *backoffConfig) {
		c.fullJitter = false
		c.percentJitter = percent
	}
}

// WithMaxDelay caps the maximum backoff delay.
//
// This is useful for preventing exponential backoff from growing unbounded.
// For example, WithMaxDelay(30*time.Second) ensures retries never wait longer
// than 30 seconds, even if the exponential calculation would produce a larger value.
//
// The cap is applied after jitter is calculated, ensuring the final delay
// (including jitter) never exceeds the maximum.
//
// Applies to both [FixedBackoff] and [ExponentialBackoff].
func WithMaxDelay(max time.Duration) BackoffOption {
	return func(c *backoffConfig) {
		c.maxDelay = max
	}
}

// WithMultiplier sets the exponential growth rate for [ExponentialBackoff].
//
// The default multiplier is 2.0, which doubles the delay on each retry.
// For example, WithMultiplier(1.5) creates gentler growth (1.5×), while
// WithMultiplier(3.0) creates more aggressive growth (3×).
//
// Only applies to [ExponentialBackoff]. This option is ignored by [FixedBackoff].
func WithMultiplier(m float64) BackoffOption {
	return func(c *backoffConfig) {
		c.multiplier = m
	}
}

// applyJitter applies jitter to a delay based on the configuration.
//
// Uses math/rand/v2, which is sufficient for backoff jitter as it uses the
// ChaCha8 algorithm and is auto-seeded with 256 bits of OS entropy. Users
// requiring additional security can seed the global generator themselves.
func applyJitter(delay time.Duration, cfg *backoffConfig) time.Duration {
	if cfg.fullJitter {
		// Full jitter: random value between 0 and delay
		if delay <= 0 {
			// This branch cannot easily be tested since backoff functions
			// always produce positive delays in normal operation.
			return 0
		}
		// #nosec G404 -- see doc comment for rationale
		return time.Duration(rand.Int64N(int64(delay) + 1))
	}
	if cfg.percentJitter > 0 {
		// Percentage jitter: delay ± (delay * percent)
		if delay <= 0 {
			// This branch cannot easily be tested since backoff functions
			// always produce positive delays in normal operation.
			return 0
		}
		jitterRange := float64(delay) * cfg.percentJitter
		// #nosec G404 -- see doc comment for rationale
		jitterAmount := (rand.Float64() * 2 * jitterRange) - jitterRange
		result := float64(delay) + jitterAmount
		if result < 0 {
			// This branch cannot easily be tested since it would require
			// extreme jitter percentages (>100%) with specific random values.
			return 0
		}
		return time.Duration(result)
	}
	return delay
}

// Retry executes a step and retries it on failure based on the given
// predicates.
//
// All predicates must return true for a retry to occur. If any predicate
// returns false, the last error is returned immediately.
//
// If no predicates are provided, this defaults to retrying up to 3 times with
// exponential backoff starting at 100ms and full jitter to prevent thundering
// herd problems.
func Retry[T any](
	step Step[T],
	predicates ...RetryPredicate,
) Step[T] {
	// set defaults if no predicates provided
	if len(predicates) == 0 {
		predicates = []RetryPredicate{
			UpTo(3),
			ExponentialBackoff(100*time.Millisecond, WithFullJitter()),
		}
	}
	return func(ctx context.Context, t T) error {
		attempts := 0
		for {
			err := step(ctx, t)
			if err == nil {
				return nil
			}
			attempts++
			for _, predicate := range predicates {
				if !predicate(ctx, attempts, err) {
					return err
				}
			}
		}
	}
}

// UpTo limits retries to a maximum number of attempts.
//
// The predicate returns true if attempts < maxAttempts, allowing retries
// to continue until the limit is reached.
func UpTo(maxAttempts int) RetryPredicate {
	return func(_ context.Context, attempts int, _ error) bool {
		return attempts < maxAttempts
	}
}

// FixedBackoff waits for a fixed duration before each retry.
//
// The delay is applied before each retry. If the context is cancelled during
// the wait, the predicate returns false and the retry is aborted.
//
// Options:
//   - [WithFullJitter] randomizes delay between 0 and the fixed duration
//   - [WithPercentageJitter] adds ±N% randomness to the fixed duration
//   - [WithMaxDelay] caps the delay (useful with jitter)
//   - [WithMultiplier] ignored (only applies to ExponentialBackoff)
func FixedBackoff(delay time.Duration, opts ...BackoffOption) RetryPredicate {
	cfg := backoffConfig{
		multiplier: 2.0, // default, though unused by FixedBackoff
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(ctx context.Context, _ int, _ error) bool {
		actualDelay := applyJitter(delay, &cfg)

		// Apply max delay cap if configured
		if cfg.maxDelay > 0 && actualDelay > cfg.maxDelay {
			// This branch cannot easily be tested since it requires jitter
			// to push the delay above the maxDelay cap, which is unlikely
			// with FixedBackoff and requires specific random values.
			actualDelay = cfg.maxDelay
		}

		timer := time.NewTimer(actualDelay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}
}

// ExponentialBackoff waits with exponentially increasing delays before each retry.
//
// By default, the delay for attempt N is base × 2^(N-1).
// For example, with a base of 100ms:
//   - Attempt 1: 100ms
//   - Attempt 2: 200ms
//   - Attempt 3: 400ms
//
// If the number of attempts is less than 1, or the calculated delay overflows,
// the base delay is used instead.
//
// If the context is cancelled during the wait, the predicate returns false.
//
// Options:
//   - [WithFullJitter] randomizes delay between 0 and the calculated delay
//   - [WithPercentageJitter] adds ±N% randomness to the calculated delay
//   - [WithMaxDelay] caps the maximum delay
//   - [WithMultiplier] changes the growth rate (default 2.0)
func ExponentialBackoff(base time.Duration, opts ...BackoffOption) RetryPredicate {
	cfg := backoffConfig{
		multiplier: 2.0, // default multiplier
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(ctx context.Context, attempts int, _ error) bool {
		if attempts < 1 {
			attempts = 1
		}

		var delay time.Duration
		if cfg.multiplier == 2.0 {
			// Use bit-shifting for the common case (multiplier = 2.0)
			// #nosec G115 -- attempts >= 1, conversion is safe
			shift := uint(attempts) - 1
			if shift > 62 {
				shift = 62
			}
			delay = base * time.Duration(1<<shift)
			if delay.Seconds() == 0 {
				delay = base
			}
		} else {
			// Use power calculation for custom multipliers
			multiplier := cfg.multiplier
			for i := 1; i < attempts; i++ {
				base = time.Duration(float64(base) * multiplier)
				// Prevent overflow
				if base.Seconds() == 0 || base < 0 {
					// This branch cannot easily be tested since it requires
					// extreme multipliers or attempt counts to cause overflow.
					base = time.Hour * 24 * 365 // cap at 1 year
					break
				}
			}
			delay = base
		}

		// Apply jitter
		delay = applyJitter(delay, &cfg)

		// Apply max delay cap if configured
		if cfg.maxDelay > 0 && delay > cfg.maxDelay {
			delay = cfg.maxDelay
		}

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}
}

// OnlyIf conditionally retries based on the error.
//
// The predicate returns true only if the provided check function returns true
// for the error. This is useful for retrying only transient errors while
// immediately failing on permanent errors like validation failures.
func OnlyIf(check func(error) bool) RetryPredicate {
	return func(_ context.Context, _ int, err error) bool {
		return check(err)
	}
}
