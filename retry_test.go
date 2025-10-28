// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "SucceedsFirstTry",
			step: Retry(
				Increment(1),
			),
			expectedCounter: 1,
			validator:       isNil,
		},
		{
			name: "FailsTwiceSucceedsThird",
			step: Retry(
				FailUntilCount(3),
				UpTo(5),
			),
			// counter reaches 1, 2, 3 - fails at 1 and 2, succeeds at 3
			expectedCounter: 3,
			validator:       isNil,
		},
		{
			name: "ExceedsMaxAttempts",
			step: Retry(
				FailUntilCount(10),
				UpTo(3),
			),
			// tries 3 times, counter reaches 1, 2, 3, all fail
			expectedCounter: 3,
			validator:       isNotNil,
		},
		{
			name: "OnlyIfRetryable",
			step: Retry(
				IncrementAndFail(errorRetryable),
				OnlyIf(func(err error) bool {
					return errors.Is(err, errorRetryable)
				}),
				UpTo(3),
			),
			// retries because error is retryable
			expectedCounter: 3,
			validator:       isNotNil,
		},
		{
			name: "OnlyIfNonRetryable",
			step: Retry(
				IncrementAndFail(errorNonRetryable),
				OnlyIf(func(err error) bool {
					return errors.Is(err, errorRetryable)
				}),
				UpTo(3),
			),
			// stops immediately, error not retryable
			expectedCounter: 1,
			validator:       isNotNil,
		},
		{
			name: "ComposedPredicates",
			step: Retry(
				FailUntilCount(3),
				OnlyIf(func(err error) bool { return true }),
				UpTo(5),
			),
			// passes OnlyIf and UpTo checks
			expectedCounter: 3,
			validator:       isNil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestRetryBackoff(t *testing.T) {
	t.Parallel()
	t.Run("FixedBackoff", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()
		// FailUntilCount(3) will fail on attempts 1 and 2, succeed on attempt 3
		// With 50ms fixed backoff, we should see:
		//   - Attempt 1 (counter=1): fails, waits 50ms
		//   - Attempt 2 (counter=2): fails, waits 50ms
		//   - Attempt 3 (counter=3): succeeds
		// Total expected delay: 100ms (2 waits of 50ms each)
		_ = Retry(
			FailUntilCount(3),
			UpTo(5),
			FixedBackoff(50*time.Millisecond),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Verify we waited at least 100ms (2 delays of 50ms)
		if elapsed < 100*time.Millisecond {
			t.Errorf("expected at least 100ms, got %v", elapsed)
		}
	})

	t.Run("ExponentialBackoff", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()
		// FailUntilCount(4) will fail on attempts 1, 2, and 3, succeed on attempt 4
		// With 50ms base exponential backoff, we should see:
		//   - Attempt 1 (counter=1): fails, waits 50ms
		//   - Attempt 2 (counter=2): fails, waits 100ms (50ms * 2^1)
		//   - Attempt 3 (counter=3): fails, waits 200ms (50ms * 2^2)
		//   - Attempt 4 (counter=4): succeeds
		// Total expected delay: 350ms
		_ = Retry(
			FailUntilCount(4),
			UpTo(5),
			ExponentialBackoff(50*time.Millisecond),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Verify we waited at least 300ms (allowing some margin for timing variability)
		if elapsed < 300*time.Millisecond {
			t.Errorf("expected at least 300ms for exponential backoff, got %v", elapsed)
		}
	})

	t.Run("ExponentialBackoffUnderflow", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		ExponentialBackoff(50*time.Millisecond)(t.Context(), -1, nil)
		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond {
			t.Errorf("expected at least 50ms for exponential backoff, got %v", elapsed)
		}
	})

	t.Run("ExponentialBackoffOverflow", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		start := time.Now()
		ExponentialBackoff(50*time.Millisecond)(ctx, 128, nil)
		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond {
			t.Errorf("expected at least 50ms for exponential backoff, got %v", elapsed)
		}
	})

	t.Run("FixedBackoffCancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		var c CountingFlow

		// Cancel the context after 25ms, which is less than the 100ms backoff delay.
		// This ensures cancellation happens during the first backoff wait.
		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()

		err := Retry(
			FailUntilCount(10),
			UpTo(5),
			FixedBackoff(100*time.Millisecond),
		)(ctx, &c)

		// Should fail because context was cancelled during backoff
		if err == nil {
			t.Error("expected error due to context cancellation")
		}
		// Counter should be 1 (first attempt) since cancellation happens
		// during first backoff, preventing attempt 2
		if c.Counter != 1 {
			t.Errorf("expected counter to be 1, got %d", c.Counter)
		}
	})

	t.Run("ExponentialBackoffCancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		var c CountingFlow

		// Cancel the context after 25ms, which is less than the 100ms backoff delay.
		// This ensures cancellation happens during the first backoff wait.
		go func() {
			time.Sleep(25 * time.Millisecond)
			cancel()
		}()

		err := Retry(
			FailUntilCount(10),
			UpTo(5),
			ExponentialBackoff(100*time.Millisecond),
		)(ctx, &c)

		// Should fail because context was cancelled during backoff
		if err == nil {
			t.Error("expected error due to context cancellation")
		}
		// Counter should be 1 (first attempt) since cancellation happens
		// during first backoff, preventing attempt 2
		if c.Counter != 1 {
			t.Errorf("expected counter to be 1, got %d", c.Counter)
		}
	})
}

func TestBackoffOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithFullJitter_ExponentialBackoff", func(t *testing.T) {
		t.Parallel()
		// Test that full jitter produces delays between 0 and the calculated delay
		// Run multiple times to ensure we get variation
		var c CountingFlow
		start := time.Now()

		// FailUntilCount(3) will fail on attempts 1 and 2, succeed on attempt 3
		// With 100ms base exponential backoff and full jitter:
		//   - Attempt 1 (counter=1): fails, waits random(0, 100ms)
		//   - Attempt 2 (counter=2): fails, waits random(0, 200ms)
		//   - Attempt 3 (counter=3): succeeds
		_ = Retry(
			FailUntilCount(3),
			UpTo(5),
			ExponentialBackoff(100*time.Millisecond, WithFullJitter()),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// With full jitter, the delay could be anywhere from 0 to 300ms
		// We can't test for exact timing, but we can verify it completes
		// and is less than the maximum possible delay (300ms + some buffer)
		if elapsed > 400*time.Millisecond {
			t.Errorf("expected less than 400ms with full jitter, got %v", elapsed)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter 3, got %d", c.Counter)
		}
	})

	t.Run("WithFullJitter_FixedBackoff", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()

		// With 100ms fixed backoff and full jitter, each delay is random(0, 100ms)
		_ = Retry(
			FailUntilCount(3),
			UpTo(5),
			FixedBackoff(100*time.Millisecond, WithFullJitter()),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Total delay should be less than 200ms (2 waits of max 100ms each)
		if elapsed > 300*time.Millisecond {
			t.Errorf("expected less than 300ms with full jitter, got %v", elapsed)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter 3, got %d", c.Counter)
		}
	})

	t.Run("WithPercentageJitter", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()

		// With 100ms base and 20% jitter:
		//   - Attempt 1: fails, waits 100ms ± 20% (80-120ms)
		//   - Attempt 2: fails, waits 200ms ± 20% (160-240ms)
		//   - Attempt 3: succeeds
		// Minimum total: 240ms, Maximum total: 360ms
		_ = Retry(
			FailUntilCount(3),
			UpTo(5),
			ExponentialBackoff(100*time.Millisecond, WithPercentageJitter(0.2)),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Verify we're within the expected range (with some buffer for timing variance)
		if elapsed < 200*time.Millisecond {
			t.Errorf("expected at least 200ms, got %v", elapsed)
		}
		if elapsed > 400*time.Millisecond {
			t.Errorf("expected less than 400ms, got %v", elapsed)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter 3, got %d", c.Counter)
		}
	})

	t.Run("WithMaxDelay", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()

		// With 100ms base exponential backoff capped at 150ms:
		//   - Attempt 1: fails, waits 100ms (under cap)
		//   - Attempt 2: fails, waits 150ms (200ms capped to 150ms)
		//   - Attempt 3: fails, waits 150ms (400ms capped to 150ms)
		//   - Attempt 4: succeeds
		// Total: 400ms
		_ = Retry(
			FailUntilCount(4),
			UpTo(5),
			ExponentialBackoff(100*time.Millisecond, WithMaxDelay(150*time.Millisecond)),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Verify the cap was applied (should be around 400ms, not 700ms)
		if elapsed < 350*time.Millisecond {
			t.Errorf("expected at least 350ms, got %v", elapsed)
		}
		if elapsed > 500*time.Millisecond {
			t.Errorf("expected less than 500ms (proving cap worked), got %v", elapsed)
		}
		if c.Counter != 4 {
			t.Errorf("expected counter 4, got %d", c.Counter)
		}
	})

	t.Run("WithMultiplier", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()

		// With 100ms base and 1.5x multiplier:
		//   - Attempt 1: fails, waits 100ms
		//   - Attempt 2: fails, waits 150ms (100 * 1.5)
		//   - Attempt 3: fails, waits 225ms (150 * 1.5)
		//   - Attempt 4: succeeds
		// Total: ~475ms (vs 700ms with 2.0 multiplier)
		_ = Retry(
			FailUntilCount(4),
			UpTo(5),
			ExponentialBackoff(100*time.Millisecond, WithMultiplier(1.5)),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// Verify gentler growth than default 2.0 multiplier
		if elapsed < 400*time.Millisecond {
			t.Errorf("expected at least 400ms, got %v", elapsed)
		}
		if elapsed > 600*time.Millisecond {
			t.Errorf("expected less than 600ms, got %v", elapsed)
		}
		if c.Counter != 4 {
			t.Errorf("expected counter 4, got %d", c.Counter)
		}
	})

	t.Run("CombinedOptions", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()

		// Combine jitter and max delay
		_ = Retry(
			FailUntilCount(4),
			UpTo(5),
			ExponentialBackoff(
				100*time.Millisecond,
				WithPercentageJitter(0.1),
				WithMaxDelay(180*time.Millisecond),
			),
		)(t.Context(), &c)
		elapsed := time.Since(start)

		// With 10% jitter and 180ms cap:
		//   - Attempt 1: 100ms ± 10% (90-110ms)
		//   - Attempt 2: 180ms max ± 10% (162-198ms, but capped)
		//   - Attempt 3: 180ms max ± 10% (162-198ms, but capped)
		// The cap should be applied after jitter, so max individual delay is 180ms
		// Total should be less than 540ms
		if elapsed > 600*time.Millisecond {
			t.Errorf("expected less than 600ms with cap, got %v", elapsed)
		}
		if c.Counter != 4 {
			t.Errorf("expected counter 4, got %d", c.Counter)
		}
	})

}
