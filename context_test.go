// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithTimeout(t *testing.T) {
	t.Parallel()
	t.Run("CompletesBeforeTimeout", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Fast step that completes before timeout
		step := WithTimeout(100*time.Millisecond, Increment(5))
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 5 {
			t.Errorf("expected counter=5, got %d", c.Counter)
		}
	})

	t.Run("ExceedsTimeout", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Slow step that exceeds timeout
		slowStep := func(ctx context.Context, cf *CountingFlow) error {
			select {
			case <-time.After(200 * time.Millisecond):
				atomic.AddInt64(&cf.Counter, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		step := WithTimeout(50*time.Millisecond, slowStep)
		err := step(t.Context(), &c)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
		// Counter should not be incremented since step was cancelled
		if c.Counter != 0 {
			t.Errorf("expected counter=0, got %d", c.Counter)
		}
	})

}

func TestWithDeadline(t *testing.T) {
	t.Parallel()
	t.Run("CompletesBeforeDeadline", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Fast step that completes before deadline
		deadline := time.Now().Add(100 * time.Millisecond)
		step := WithDeadline(deadline, Increment(7))
		err := step(t.Context(), &c)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 7 {
			t.Errorf("expected counter=7, got %d", c.Counter)
		}
	})

	t.Run("ExceedsDeadline", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		// Slow step that exceeds deadline
		slowStep := func(ctx context.Context, cf *CountingFlow) error {
			select {
			case <-time.After(200 * time.Millisecond):
				atomic.AddInt64(&cf.Counter, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		deadline := time.Now().Add(50 * time.Millisecond)
		step := WithDeadline(deadline, slowStep)
		err := step(t.Context(), &c)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
		// Counter should not be incremented since step was cancelled
		if c.Counter != 0 {
			t.Errorf("expected counter=0, got %d", c.Counter)
		}
	})

}

func TestSleep(t *testing.T) {
	t.Parallel()
	t.Run("SleepsForDuration", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()
		step := Do(
			Increment(1),
			Sleep[*CountingFlow](100*time.Millisecond),
			Increment(2),
		)
		err := step(t.Context(), &c)
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter=3, got %d", c.Counter)
		}
		if elapsed < 100*time.Millisecond {
			t.Errorf("expected at least 100ms sleep, got %v", elapsed)
		}
	})

	t.Run("RespectsContextCancellation", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()
		step := Do(
			Increment(1),
			Sleep[*CountingFlow](200*time.Millisecond),
			Increment(2),
		)
		start := time.Now()
		err := step(ctx, &c)
		elapsed := time.Since(start)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if c.Counter != 1 {
			t.Errorf("expected counter=1 (second increment didn't run), got %d", c.Counter)
		}
		// Should have been cancelled after ~50ms, not waited full 200ms
		if elapsed > 150*time.Millisecond {
			t.Errorf("expected cancellation around 50ms, took %v", elapsed)
		}
	})

	t.Run("InWhileLoop", func(t *testing.T) {
		t.Parallel()
		var c CountingFlow
		start := time.Now()
		step := While(
			func(_ context.Context, cf *CountingFlow) (bool, error) {
				return cf.Counter < 3, nil
			},
			Do(
				Increment(1),
				Sleep[*CountingFlow](30*time.Millisecond),
			),
		)
		err := step(t.Context(), &c)
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if c.Counter != 3 {
			t.Errorf("expected counter=3, got %d", c.Counter)
		}
		// 3 iterations * 30ms = ~90ms
		if elapsed < 90*time.Millisecond {
			t.Errorf("expected at least 90ms (3 sleeps), got %v", elapsed)
		}
	})
}

// ==== Test Fixtures ====
