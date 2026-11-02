// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// upTo returns a Pool puller yielding i*10 for pull indices below n.
func upTo(n int) func(context.Context, int) (int, error) {
	return func(_ context.Context, i int) (int, error) {
		if i >= n {
			return 0, ErrExhausted
		}
		return i * 10, nil
	}
}

// gaugeTask returns a Pool task that counts itself as running on g for d.
func gaugeTask(g *concurrencyGauge, d time.Duration) func(context.Context, int, int) error {
	consume := g.consume(d)
	return func(ctx context.Context, _ int, _ int) error {
		return consume(ctx, nil, 0)
	}
}

func TestPool(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts ParallelOptions
	}{
		{"Default", ParallelOptions{}},
		{"SingleWorker", ParallelOptions{Limit: 1}},
		{"Limited", ParallelOptions{Limit: 3}},
		{"Prefetch", ParallelOptions{Limit: 3, Prefetch: 4}},
		{"PrefetchSingleWorker", ParallelOptions{Limit: 1, Prefetch: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Every pulled value reaches the task with the index it was
			// pulled at, exactly once.
			const n = 50
			var mu sync.Mutex
			seen := map[int]int{}
			err := Pool(t.Context(), upTo(n),
				func(_ context.Context, i int, u int) error {
					mu.Lock()
					defer mu.Unlock()
					seen[i] = u
					return nil
				},
				tc.opts,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(seen) != n {
				t.Fatalf("ran %d tasks, want %d", len(seen), n)
			}
			for i, u := range seen {
				if u != i*10 {
					t.Errorf("task %d got %d, want %d", i, u, i*10)
				}
			}
		})
	}
}

func TestPoolWorkers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		limit int
		want  int64
	}{
		{"Limit", 3, 3},
		{"ZeroMeansGOMAXPROCS", 0, int64(runtime.GOMAXPROCS(0))},
		{"NegativeMeansGOMAXPROCS", -1, int64(runtime.GOMAXPROCS(0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var g concurrencyGauge
			err := Pool(t.Context(), upTo(int(tc.want)*4), gaugeTask(&g, 20*time.Millisecond),
				ParallelOptions{Limit: tc.limit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := g.Max(); got != tc.want {
				t.Errorf("max concurrency = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPoolSerializesNext(t *testing.T) {
	t.Parallel()

	var inNext, maxInNext atomic.Int64
	pulls := 0 // unsynchronized on purpose; the race detector checks Pool
	err := Pool(t.Context(),
		func(_ context.Context, i int) (int, error) {
			if n := inNext.Add(1); n > maxInNext.Load() {
				maxInNext.Store(n)
			}
			defer inNext.Add(-1)
			time.Sleep(time.Millisecond)
			pulls++
			if i >= 20 {
				return 0, ErrExhausted
			}
			return i, nil
		},
		func(context.Context, int, int) error { return nil },
		ParallelOptions{Limit: 8},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := maxInNext.Load(); got != 1 {
		t.Errorf("next ran %d at once, want 1", got)
	}
	if pulls != 21 {
		t.Errorf("next called %d times, want 21", pulls)
	}
}

func TestPoolErrors(t *testing.T) {
	t.Parallel()
	errTask := errors.New("task failed")
	errNext := errors.New("next failed")

	t.Run("TaskErrorFailsFast", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Int64
		err := Pool(t.Context(), upTo(1000),
			func(ctx context.Context, i int, _ int) error {
				ran.Add(1)
				if i == 0 {
					return errTask
				}
				<-ctx.Done()
				return ctx.Err()
			},
			ParallelOptions{Limit: 4},
		)
		if !errors.Is(err, errTask) {
			t.Fatalf("got %v, want %v", err, errTask)
		}
		if got := ran.Load(); got > 4 {
			t.Errorf("ran %d tasks, want at most 4 (one per worker)", got)
		}
	})

	t.Run("JoinErrorsCollectsTaskErrors", func(t *testing.T) {
		t.Parallel()
		var ran atomic.Int64
		err := Pool(t.Context(), upTo(10),
			func(_ context.Context, i int, _ int) error {
				ran.Add(1)
				if i%2 == 1 {
					return &IndexedError{Index: i, Err: errTask}
				}
				return nil
			},
			ParallelOptions{Limit: 3, JoinErrors: true},
		)
		if ran.Load() != 10 {
			t.Errorf("ran %d tasks, want 10", ran.Load())
		}
		joined, ok := err.(interface{ Unwrap() []error })
		if !ok {
			t.Fatalf("got %v, want a joined error", err)
		}
		if got := len(joined.Unwrap()); got != 5 {
			t.Errorf("joined %d errors, want 5", got)
		}
		if !errors.Is(err, errTask) {
			t.Errorf("got %v, want it to wrap %v", err, errTask)
		}
	})

	for _, joinErrors := range []bool{false, true} {
		name := "NextErrorStopsPulling"
		if joinErrors {
			name += "WithJoinErrors"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var pulls, ran atomic.Int64
			err := Pool(t.Context(),
				func(_ context.Context, i int) (int, error) {
					pulls.Add(1)
					if i == 5 {
						return 0, errNext
					}
					return i, nil
				},
				func(context.Context, int, int) error {
					ran.Add(1)
					return nil
				},
				ParallelOptions{Limit: 1, JoinErrors: joinErrors},
			)
			if !errors.Is(err, errNext) {
				t.Fatalf("got %v, want %v", err, errNext)
			}
			if got := pulls.Load(); got != 6 {
				t.Errorf("next called %d times, want 6", got)
			}
			if got := ran.Load(); got != 5 {
				t.Errorf("ran %d tasks, want 5", got)
			}
		})
	}
}

func TestPoolCancellation(t *testing.T) {
	t.Parallel()

	for _, prefetch := range []int{0, 4} {
		t.Run(fmt.Sprintf("Prefetch%d", prefetch), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			var pulls atomic.Int64
			start := make(chan struct{})
			errCh := make(chan error, 1)
			go func() {
				errCh <- Pool(ctx,
					func(context.Context, int) (int, error) {
						return int(pulls.Add(1)), nil
					},
					func(ctx context.Context, i int, _ int) error {
						if i == 0 {
							close(start)
						}
						<-ctx.Done()
						return ctx.Err()
					},
					ParallelOptions{Limit: 2, Prefetch: prefetch},
				)
			}()

			<-start
			cancel()
			select {
			case err := <-errCh:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want %v", err, context.Canceled)
				}
			case <-time.After(time.Second):
				t.Fatal("Pool did not return after cancellation")
			}
			// next is never called after Pool returns.
			n := pulls.Load()
			time.Sleep(20 * time.Millisecond)
			if got := pulls.Load(); got != n {
				t.Errorf("next called %d more times after Pool returned", got-n)
			}
		})
	}
}

func TestPoolNestedUnderGlobalSemaphore(t *testing.T) {
	t.Parallel()

	// Each outer task runs an inner pool; with a cap of 1, this deadlocks
	// unless the outer task yields its permit while the inner pool runs.
	var g concurrencyGauge
	inner := func(ctx context.Context, _ int, _ int) error {
		return Pool(ctx, upTo(3), gaugeTask(&g, 5*time.Millisecond), ParallelOptions{Limit: 3})
	}
	runBefore(t, 5*time.Second, WithMaxConcurrency(1, func(ctx context.Context, _ *CountingFlow) error {
		return Pool(ctx, upTo(4), inner, ParallelOptions{Limit: 4})
	}))
	if got := g.Max(); got != 1 {
		t.Errorf("max concurrency = %d, want 1", got)
	}
}

func TestParallelDoNoLimit(t *testing.T) {
	t.Parallel()

	// With no limit, every task runs at once, even beyond GOMAXPROCS: each
	// task waits for all the others to start, which deadlocks with fewer
	// workers than tasks.
	n := runtime.GOMAXPROCS(0)*2 + 1
	var started atomic.Int64
	allStarted := make(chan struct{})
	step := With(sliceOf(n), ApplyParallel(func(ctx context.Context, _ *CountingFlow, _ int64) error {
		if started.Add(1) == int64(n) {
			close(allStarted)
		}
		select {
		case <-allStarted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, ParallelOptions{}))
	runBefore(t, 5*time.Second, step)
}
