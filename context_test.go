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

func TestFlowCtxSkipOptimization(t *testing.T) {
	t.Parallel()

	t.Run("UserValueAtRoot", func(t *testing.T) {
		t.Parallel()
		type testKey struct{}
		ctx := context.WithValue(t.Context(), testKey{}, "root-value")
		// Stack several addName layers on top
		ctx, _ = addName(ctx, "step1")
		ctx, _ = addName(ctx, "step2")
		ctx, _ = addName(ctx, "step3")
		// The root value should still be reachable
		got := ctx.Value(testKey{})
		if got != "root-value" {
			t.Errorf("expected root-value, got %v", got)
		}
	})

	t.Run("UserValueBetweenFlowLayers", func(t *testing.T) {
		t.Parallel()
		type testKey struct{}
		ctx := t.Context()
		ctx, _ = addName(ctx, "outer")
		ctx = context.WithValue(ctx, testKey{}, "interleaved")
		ctx, _ = addName(ctx, "inner")
		// The interleaved value should be found
		got := ctx.Value(testKey{})
		if got != "interleaved" {
			t.Errorf("expected interleaved, got %v", got)
		}
	})

	t.Run("ConcurrentBranchesWithInterleavedValues", func(t *testing.T) {
		t.Parallel()
		type testKey struct{}
		ctx := t.Context()
		ctx, _ = addName(ctx, "root")
		ctx = context.WithValue(ctx, testKey{}, "a")

		errs := make(chan error, 2)

		// Branch 1: sees "a"
		go func() {
			branchCtx, _ := addName(ctx, "branch1")
			got := branchCtx.Value(testKey{})
			if got != "a" {
				errs <- errors.New("branch1: expected 'a', got something else")
				return
			}
			errs <- nil
		}()

		// Branch 2: adds WithValue "b", should see "b"
		go func() {
			branchCtx := context.WithValue(ctx, testKey{}, "b")
			branchCtx, _ = addName(branchCtx, "branch2")
			got := branchCtx.Value(testKey{})
			if got != "b" {
				errs <- errors.New("branch2: expected 'b', got something else")
				return
			}
			errs <- nil
		}()

		for range 2 {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
	})

	t.Run("DeepNestingWithoutIntermediateNonFlowContexts", func(t *testing.T) {
		t.Parallel()
		type testKey struct{}
		ctx := context.WithValue(t.Context(), testKey{}, "deep-root")
		// Add many flow layers
		for range 100 {
			ctx, _ = addName(ctx, "layer")
		}
		got := ctx.Value(testKey{})
		if got != "deep-root" {
			t.Errorf("expected deep-root, got %v", got)
		}

		// Verify the skip optimization: the innermost flowCtx's embedded
		// Context should point directly to the non-flowCtx ancestor.
		fc, ok := ctx.Value(flowCtxKey{}).(*flowCtx)
		if !ok {
			t.Fatal("expected flowCtx")
		}
		if _, isFlow := fc.Context.(*flowCtx); isFlow {
			t.Error("expected embedded Context to skip past flowCtx layers")
		}
	})

	t.Run("FlowCtxKeyLookupsStillWork", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		ctx, _ = addName(ctx, "a")
		ctx, _ = addName(ctx, "b")
		ctx, _ = addName(ctx, "c")

		// Names should reflect the full stack
		names := Names(ctx)
		if len(names) != 3 || names[0] != "a" || names[1] != "b" || names[2] != "c" {
			t.Errorf("expected [a b c], got %v", names)
		}

		// Logger and Slogger should return non-nil defaults
		if Logger(ctx) == nil {
			t.Error("expected non-nil Logger")
		}
		if Slogger(ctx) == nil {
			t.Error("expected non-nil Slogger")
		}
	})
}

func TestWithMaxConcurrency(t *testing.T) {
	t.Parallel()

	t.Run("CapsGlobalConcurrency", func(t *testing.T) {
		t.Parallel()
		items := make([]int64, 20)
		for i := range items {
			items[i] = int64(i)
		}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

		step := WithMaxConcurrency(2, With(
			func(_ context.Context, _ *CountingFlow) ([]int64, error) {
				return items, nil
			},
			ApplyParallel(
				func(_ context.Context, _ *CountingFlow, n int64) error {
					cur := current.Add(1)
					for {
						old := maxConcurrent.Load()
						if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
							break
						}
					}
					time.Sleep(5 * time.Millisecond)
					current.Add(-1)
					return nil
				},
				ParallelOptions{}, // no per-combinator limit
			),
		))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if maxConcurrent.Load() > 2 {
			t.Errorf("max concurrency %d exceeded global cap 2", maxConcurrent.Load())
		}
	})

	t.Run("ComposesWithPerCombinatorLimit", func(t *testing.T) {
		t.Parallel()
		items := make([]int64, 20)
		for i := range items {
			items[i] = int64(i)
		}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

		// Per-combinator limit of 10, but global cap of 3
		step := WithMaxConcurrency(3, With(
			func(_ context.Context, _ *CountingFlow) ([]int64, error) {
				return items, nil
			},
			ApplyParallel(
				func(_ context.Context, _ *CountingFlow, n int64) error {
					cur := current.Add(1)
					for {
						old := maxConcurrent.Load()
						if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
							break
						}
					}
					time.Sleep(5 * time.Millisecond)
					current.Add(-1)
					return nil
				},
				ParallelOptions{Limit: 10},
			),
		))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if maxConcurrent.Load() > 3 {
			t.Errorf("max concurrency %d exceeded global cap 3", maxConcurrent.Load())
		}
	})

	t.Run("AcrossNestedParallelCombinators", func(t *testing.T) {
		t.Parallel()
		items := make([]int64, 10)
		for i := range items {
			items[i] = int64(i)
		}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

		track := func(_ context.Context, _ *CountingFlow, n int64) error {
			cur := current.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			current.Add(-1)
			return nil
		}

		// Two parallel applies running concurrently via InParallelWith,
		// both sharing the same global cap of 4.
		step := WithMaxConcurrency(4, InParallel(
			Steps(
				With(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return items, nil
					},
					ApplyParallel(track, ParallelOptions{}),
				),
				With(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return items, nil
					},
					ApplyParallel(track, ParallelOptions{}),
				),
			),
		))

		var c CountingFlow
		err := step(t.Context(), &c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if maxConcurrent.Load() > 4 {
			t.Errorf("max concurrency %d exceeded global cap 4", maxConcurrent.Load())
		}
	})
}

// ==== Test Fixtures ====
