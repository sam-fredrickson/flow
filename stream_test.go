// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sliceSource returns a Source that yields the given items one at a time,
// then ErrExhausted.
func sliceSource(items ...int64) Source[*CountingFlow, int64] {
	i := 0
	return func(_ context.Context, _ *CountingFlow) (int64, error) {
		if i >= len(items) {
			return 0, ErrExhausted
		}
		item := items[i]
		i++
		return item, nil
	}
}

// batchSource returns a Source that yields the given batches one at a time,
// then ErrExhausted.
func batchSource(batches ...[]int64) Source[*CountingFlow, []int64] {
	i := 0
	return func(_ context.Context, _ *CountingFlow) ([]int64, error) {
		if i >= len(batches) {
			return nil, ErrExhausted
		}
		batch := batches[i]
		i++
		return batch, nil
	}
}

// failAfterSource yields items, then the given error instead of ErrExhausted.
func failAfterSource(err error, items ...int64) Source[*CountingFlow, int64] {
	i := 0
	return func(_ context.Context, _ *CountingFlow) (int64, error) {
		if i >= len(items) {
			return 0, err
		}
		item := items[i]
		i++
		return item, nil
	}
}

// addToCounter consumes an item by adding it to the counter.
func addToCounter(_ context.Context, c *CountingFlow, n int64) error {
	atomic.AddInt64(&c.Counter, n)
	return nil
}

// failOn returns a consumer that adds to the counter but fails on the given item.
func failOn(bad int64, err error) Consume[*CountingFlow, int64] {
	return func(_ context.Context, c *CountingFlow, n int64) error {
		if n == bad {
			return err
		}
		atomic.AddInt64(&c.Counter, n)
		return nil
	}
}

func TestDrain(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name:            "EmptySource",
			step:            Drain(sliceSource(), addToCounter),
			expectedCounter: 0,
			validator:       isNil,
		},
		{
			name:            "ConsumesAllItems",
			step:            Drain(sliceSource(1, 2, 3, 4, 5), addToCounter),
			expectedCounter: 15,
			validator:       isNil,
		},
		{
			name:            "SourceErrorWrapped",
			step:            Drain(failAfterSource(error1, 1, 2), addToCounter),
			expectedCounter: 3,
			validator:       all(matches(error1), indexedAt(2)),
		},
		{
			name:            "ConsumeErrorFailFast",
			step:            Drain(sliceSource(1, 2, 3), failOn(2, error1)),
			expectedCounter: 1,
			validator:       all(matches(error1), indexedAt(1)),
		},
		{
			name: "FluentStreamViaDrain",
			step: func() Step[*CountingFlow] {
				// Stream converts a plain function (or Extract) into a Source
				// so the fluent methods become available.
				i := int64(0)
				next := func(_ context.Context, _ *CountingFlow) (int64, error) {
					if i >= 3 {
						return 0, ErrExhausted
					}
					i++
					return i, nil
				}
				return Stream(next).
					Via(func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
						return n * 10, nil
					}).
					Drain(addToCounter)
			}(),
			expectedCounter: 60,
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

func TestDrainCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var c CountingFlow
	err := Drain(sliceSource(1, 2, 3), addToCounter)(ctx, &c)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if c.Counter != 0 {
		t.Errorf("expected no items consumed, got counter %d", c.Counter)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name:            "NoBatches",
			step:            Drain(Expand(batchSource()), addToCounter),
			expectedCounter: 0,
			validator:       isNil,
		},
		{
			name: "FlattensBatches",
			step: Drain(
				Expand(batchSource([]int64{1, 2}, []int64{3}, []int64{4, 5})),
				addToCounter,
			),
			expectedCounter: 15,
			validator:       isNil,
		},
		{
			name: "SkipsEmptyBatches",
			step: Drain(
				Expand(batchSource(nil, []int64{1}, nil, nil, []int64{2, 3}, nil)),
				addToCounter,
			),
			expectedCounter: 6,
			validator:       isNil,
		},
		{
			name: "SourceErrorPassesThrough",
			step: Drain(
				Expand(func(_ context.Context, _ *CountingFlow) ([]int64, error) {
					return nil, error1
				}),
				addToCounter,
			),
			expectedCounter: 0,
			validator:       all(matches(error1), indexedAt(0)),
		},
		{
			name: "CollectMaterializes",
			step: Expand(batchSource([]int64{1, 2}, []int64{3})).
				Collect().
				To(func(_ context.Context, c *CountingFlow, items []int64) error {
					atomic.AddInt64(&c.Counter, int64(len(items))*100)
					return nil
				}),
			expectedCounter: 300,
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

func TestDrainParallel(t *testing.T) {
	t.Parallel()
	items := make([]int64, 100)
	var want int64
	for i := range items {
		items[i] = int64(i + 1)
		want += int64(i + 1)
	}

	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name:            "EmptySource",
			step:            DrainParallel(sliceSource(), addToCounter, ParallelOptions{Limit: 4}),
			expectedCounter: 0,
			validator:       isNil,
		},
		{
			name:            "ConsumesAllItems",
			step:            DrainParallel(sliceSource(items...), addToCounter, ParallelOptions{Limit: 4}),
			expectedCounter: want,
			validator:       isNil,
		},
		{
			name:            "DefaultWorkerCount",
			step:            DrainParallel(sliceSource(items...), addToCounter, ParallelOptions{}),
			expectedCounter: want,
			validator:       isNil,
		},
		{
			name: "SourceErrorWrapped",
			step: DrainParallel(
				failAfterSource(error1, 1, 2),
				addToCounter,
				ParallelOptions{Limit: 1},
			),
			expectedCounter: 3,
			validator:       all(matches(error1), indexedAt(2)),
		},
		{
			name: "ConsumeErrorFailFast",
			step: DrainParallel(
				sliceSource(1, 2, 3),
				failOn(2, error1),
				ParallelOptions{Limit: 1},
			),
			expectedCounter: 1,
			validator:       all(matches(error1), indexedAt(1)),
		},
		{
			name: "JoinErrorsRunsToCompletion",
			step: DrainParallel(
				sliceSource(1, 2, 3, 4, 5),
				func(_ context.Context, c *CountingFlow, n int64) error {
					if n%2 == 0 {
						return fmt.Errorf("item %d: %w", n, error1)
					}
					atomic.AddInt64(&c.Counter, n)
					return nil
				},
				ParallelOptions{Limit: 2, JoinErrors: true},
			),
			expectedCounter: 9, // 1 + 3 + 5; items 2 and 4 fail but don't stop the pool
			validator:       matches(error1),
		},
		{
			name: "JoinErrorsIncludesSourceError",
			step: DrainParallel(
				failAfterSource(error2, 1, 2, 3),
				addToCounter,
				ParallelOptions{Limit: 2, JoinErrors: true},
			),
			expectedCounter: 6,
			validator:       matches(error2),
		},
		{
			name:            "PrefetchConsumesAllItems",
			step:            DrainParallel(sliceSource(items...), addToCounter, ParallelOptions{Limit: 4, Prefetch: 8}),
			expectedCounter: want,
			validator:       isNil,
		},
		{
			name: "PrefetchSourceErrorWrapped",
			step: DrainParallel(
				failAfterSource(error1, 1, 2),
				addToCounter,
				ParallelOptions{Limit: 1, Prefetch: 2},
			),
			expectedCounter: 3,
			validator:       all(matches(error1), indexedAt(2)),
		},
		{
			name: "PrefetchConsumeErrorFailFast",
			step: DrainParallel(
				sliceSource(1, 2, 3),
				failOn(2, error1),
				ParallelOptions{Limit: 1, Prefetch: 2},
			),
			expectedCounter: 1,
			validator:       all(matches(error1), indexedAt(1)),
		},
		{
			name: "FluentExpandDrainParallel",
			step: Expand(batchSource([]int64{1, 2}, []int64{3, 4})).
				DrainParallel(addToCounter, ParallelOptions{Limit: 4}),
			expectedCounter: 10,
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

func TestDrainParallelConcurrency(t *testing.T) {
	t.Parallel()

	// Verify that the worker limit is respected while items are consumed
	// concurrently: track the high-water mark of in-flight consumers.
	const limit = 3
	var inFlight, maxInFlight int64
	var mu sync.Mutex

	gate := make(chan struct{})
	step := DrainParallel(
		sliceSource(1, 2, 3, 4, 5, 6, 7, 8),
		func(_ context.Context, c *CountingFlow, n int64) error {
			cur := atomic.AddInt64(&inFlight, 1)
			mu.Lock()
			if cur > maxInFlight {
				maxInFlight = cur
			}
			if maxInFlight == limit {
				// All workers have been observed running at once; let
				// everyone through from now on.
				select {
				case <-gate:
				default:
					close(gate)
				}
			}
			mu.Unlock()
			<-gate
			atomic.AddInt64(&inFlight, -1)
			atomic.AddInt64(&c.Counter, n)
			return nil
		},
		ParallelOptions{Limit: limit},
	)

	var c CountingFlow
	if err := step(t.Context(), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 36 {
		t.Errorf("got counter %d, want 36", c.Counter)
	}
	if maxInFlight != limit {
		t.Errorf("got max in-flight %d, want %d", maxInFlight, limit)
	}
}

func TestDrainParallelRespectsGlobalSemaphore(t *testing.T) {
	t.Parallel()

	var inFlight, maxInFlight int64
	step := WithMaxConcurrency(2, DrainParallel(
		sliceSource(1, 2, 3, 4, 5, 6),
		func(_ context.Context, c *CountingFlow, n int64) error {
			cur := atomic.AddInt64(&inFlight, 1)
			for {
				old := atomic.LoadInt64(&maxInFlight)
				if cur <= old || atomic.CompareAndSwapInt64(&maxInFlight, old, cur) {
					break
				}
			}
			defer atomic.AddInt64(&inFlight, -1)
			atomic.AddInt64(&c.Counter, n)
			return nil
		},
		ParallelOptions{Limit: 6},
	))

	var c CountingFlow
	if err := step(t.Context(), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 21 {
		t.Errorf("got counter %d, want 21", c.Counter)
	}
	if maxInFlight > 2 {
		t.Errorf("global semaphore not respected: max in-flight %d", maxInFlight)
	}
}

func TestDrainParallelCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var c CountingFlow
	err := DrainParallel(
		sliceSource(1, 2, 3),
		addToCounter,
		ParallelOptions{Limit: 2},
	)(ctx, &c)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if c.Counter != 0 {
		t.Errorf("expected no items consumed, got counter %d", c.Counter)
	}
}

// countingSource returns an endless Source that counts its pulls.
func countingSource(pulls *atomic.Int64) Source[*CountingFlow, int64] {
	return func(_ context.Context, _ *CountingFlow) (int64, error) {
		return pulls.Add(1), nil
	}
}

func TestDrainParallelPrefetchPullsAhead(t *testing.T) {
	t.Parallel()

	// With the only worker blocked on the first item, the source should
	// still be pulled Prefetch more times, and no further.
	const prefetch = 3
	var pulls atomic.Int64
	gate := make(chan struct{})
	step := DrainParallel(
		func(ctx context.Context, c *CountingFlow) (int64, error) {
			n := pulls.Add(1)
			if n > 1+prefetch+1 {
				return 0, ErrExhausted
			}
			return n, nil
		},
		func(_ context.Context, c *CountingFlow, n int64) error {
			<-gate
			atomic.AddInt64(&c.Counter, n)
			return nil
		},
		ParallelOptions{Limit: 1, Prefetch: prefetch},
	)

	var c CountingFlow
	errc := make(chan error, 1)
	go func() { errc <- step(t.Context(), &c) }()

	deadline := time.Now().Add(5 * time.Second)
	for pulls.Load() < 1+prefetch && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // give an over-eager prefetcher time to misbehave
	if got := pulls.Load(); got != 1+prefetch {
		t.Errorf("got %d pulls while the worker was blocked, want %d", got, 1+prefetch)
	}

	close(gate)
	if err := <-errc; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Counter != 15 {
		t.Errorf("got counter %d, want 15", c.Counter)
	}
}

func TestDrainParallelPrefetchStopsAfterConsumerError(t *testing.T) {
	t.Parallel()

	// An endless source must not be pulled after the drain returns.
	var pulls atomic.Int64
	err := DrainParallel(
		countingSource(&pulls),
		func(context.Context, *CountingFlow, int64) error { return error1 },
		ParallelOptions{Limit: 2, Prefetch: 4},
	)(t.Context(), &CountingFlow{})
	if !errors.Is(err, error1) {
		t.Fatalf("expected error1, got %v", err)
	}

	atReturn := pulls.Load()
	time.Sleep(20 * time.Millisecond)
	if got := pulls.Load(); got != atReturn {
		t.Errorf("source pulled %d more times after the drain returned", got-atReturn)
	}
	// Two workers took an item each; at most Prefetch more were pulled.
	if atReturn > 2+4 {
		t.Errorf("got %d pulls, want at most %d", atReturn, 2+4)
	}
}

func TestDrainParallelPrefetchStopsOnSourceError(t *testing.T) {
	t.Parallel()

	// A broken source is not called again, even in JoinErrors mode.
	var pulls atomic.Int64
	err := DrainParallel(
		func(context.Context, *CountingFlow) (int64, error) {
			pulls.Add(1)
			return 0, error1
		},
		addToCounter,
		ParallelOptions{Limit: 2, Prefetch: 4, JoinErrors: true},
	)(t.Context(), &CountingFlow{})
	if err := all(matches(error1), indexedAt(0))(err); err != nil {
		t.Error(err)
	}
	if got := pulls.Load(); got != 1 {
		t.Errorf("got %d pulls, want 1", got)
	}
}

func TestDrainParallelPrefetchCancelsInFlightPull(t *testing.T) {
	t.Parallel()

	// When a consumer fails while the source is mid-pull and another worker
	// is waiting on it, the pull is cancelled rather than awaited, and has
	// finished by the time the drain returns.
	var pulls, inPull atomic.Int64
	pulling := make(chan struct{})
	step := DrainParallel(
		func(ctx context.Context, _ *CountingFlow) (int64, error) {
			inPull.Add(1)
			defer inPull.Add(-1)
			if pulls.Add(1) == 1 {
				return 1, nil
			}
			close(pulling)
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(5 * time.Second):
				return 0, errors.New("pull was not cancelled")
			}
		},
		func(context.Context, *CountingFlow, int64) error {
			<-pulling
			time.Sleep(20 * time.Millisecond) // let the other worker start waiting
			return error1
		},
		ParallelOptions{Limit: 2, Prefetch: 1},
	)

	start := time.Now()
	err := step(t.Context(), &CountingFlow{})
	if !errors.Is(err, error1) {
		t.Errorf("expected error1, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("drain took %v to return; in-flight pull was not cancelled", elapsed)
	}
	if n := inPull.Load(); n != 0 {
		t.Errorf("source still running after the drain returned (%d calls)", n)
	}
}

func TestDrainParallelPrefetchCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var pulls atomic.Int64
	var c CountingFlow
	err := DrainParallel(
		countingSource(&pulls),
		addToCounter,
		ParallelOptions{Limit: 2, Prefetch: 4},
	)(ctx, &c)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if c.Counter != 0 {
		t.Errorf("expected no items consumed, got counter %d", c.Counter)
	}
	if got := pulls.Load(); got != 0 {
		t.Errorf("expected no pulls, got %d", got)
	}
}

// indexedAt returns a validator that checks the error is an IndexedError
// with the given index.
func indexedAt(index int) func(error) error {
	return func(testErr error) error {
		var ie *IndexedError
		if !errors.As(testErr, &ie) {
			return fmt.Errorf("expected IndexedError, got %v", testErr)
		}
		if ie.Index != index {
			return fmt.Errorf("got index %d, want %d", ie.Index, index)
		}
		return nil
	}
}
