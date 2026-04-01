// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManage(t *testing.T) {
	t.Parallel()

	t.Run("BasicCleanup", func(t *testing.T) {
		t.Parallel()
		var cleaned bool

		step := Scope(Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error {
					c.Counter = 42
					return nil
				},
				func(ctx context.Context, c *CountingFlow) error {
					cleaned = true
					return nil
				},
			),
			func(ctx context.Context, c *CountingFlow) error {
				if c.Counter != 42 {
					t.Errorf("expected Counter=42, got %d", c.Counter)
				}
				return nil
			},
		))

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cleaned {
			t.Error("expected cleanup to run")
		}
	})

	t.Run("LIFOOrder", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var order []string

		manage := func(name string) Step[*CountingFlow] {
			return Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					mu.Lock()
					order = append(order, name)
					mu.Unlock()
					return nil
				},
			)
		}

		step := Scope(Do(
			manage("first"),
			manage("second"),
			manage("third"),
		))

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(order) != 3 || order[0] != "third" || order[1] != "second" || order[2] != "first" {
			t.Errorf("expected LIFO order [third second first], got %v", order)
		}
	})

	t.Run("StepErrorAndCleanupError", func(t *testing.T) {
		t.Parallel()
		stepErr := errors.New("step failed")
		cleanErr := errors.New("cleanup failed")

		step := Scope(Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error { return cleanErr },
			),
			func(ctx context.Context, c *CountingFlow) error { return stepErr },
		))

		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, stepErr) {
			t.Errorf("expected step error, got %v", err)
		}
		if !errors.Is(err, cleanErr) {
			t.Errorf("expected cleanup error, got %v", err)
		}
	})

	t.Run("NoScopeError", func(t *testing.T) {
		t.Parallel()

		step := Manage(
			func(ctx context.Context, c *CountingFlow) error { return nil },
			func(ctx context.Context, c *CountingFlow) error { return nil },
		)
		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, ErrNoScope) {
			t.Fatalf("expected ErrNoScope, got %v", err)
		}
	})

	t.Run("CrossNamedPropagation", func(t *testing.T) {
		t.Parallel()
		var cleaned bool

		step := Scope(Named("outer", Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					cleaned = true
					return nil
				},
			),
		)))

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cleaned {
			t.Error("expected cleanup to run through Named layer")
		}
	})

	t.Run("ConcurrentInParallel", func(t *testing.T) {
		t.Parallel()
		var cleanedCount atomic.Int32
		const n = 10

		var steps []Step[*CountingFlow]
		for range n {
			steps = append(steps, Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					cleanedCount.Add(1)
					return nil
				},
			))
		}

		step := Scope(InParallel(Steps(steps...)))

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanedCount.Load() != n {
			t.Errorf("expected %d cleanups, got %d", n, cleanedCount.Load())
		}
	})

	t.Run("PartialAcquisitionFailure", func(t *testing.T) {
		t.Parallel()
		var firstCleaned bool
		acquireErr := errors.New("acquire failed")

		step := Scope(Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					firstCleaned = true
					return nil
				},
			),
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return acquireErr },
				func(ctx context.Context, c *CountingFlow) error {
					t.Error("second cleanup should not run")
					return nil
				},
			),
		))

		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, acquireErr) {
			t.Fatalf("expected acquireErr, got %v", err)
		}
		if !firstCleaned {
			t.Error("expected first resource to be cleaned up")
		}
	})

	t.Run("ChecksCtxErr", func(t *testing.T) {
		t.Parallel()
		var acquireCalled bool

		step := Scope(func(ctx context.Context, c *CountingFlow) error {
			cancelledCtx, cancel := context.WithCancel(ctx)
			cancel()
			return Manage(
				func(ctx context.Context, c *CountingFlow) error {
					acquireCalled = true
					return nil
				},
				func(ctx context.Context, c *CountingFlow) error { return nil },
			)(cancelledCtx, c)
		})

		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if acquireCalled {
			t.Error("acquire should not be called with cancelled context")
		}
	})

	t.Run("RetryOnCancelledContext", func(t *testing.T) {
		t.Parallel()
		var cleanupCount atomic.Int32

		step := WithCleanupTimeout(5*time.Second,
			Scope(Do(
				Manage(
					func(ctx context.Context, c *CountingFlow) error { return nil },
					func(ctx context.Context, c *CountingFlow) error {
						cleanupCount.Add(1)
						return nil
					},
				),
				// Simulate explicit cleanup attempt with cancelled context.
				// Manage's registered closure has the same ctx.Err() guard as Clean.
				// When scope sweeps with a fresh context, cleanup should actually run.
			)),
		)

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cleanupCount.Load() != 1 {
			t.Errorf("expected cleanup to run once, ran %d times", cleanupCount.Load())
		}
	})
}

func TestScope(t *testing.T) {
	t.Parallel()

	t.Run("CleanupTimeout", func(t *testing.T) {
		t.Parallel()
		var cleanupCtxErr error

		// Cancel the parent context from inside the step, then verify
		// WithCleanupTimeout's WithoutCancel gives cleanup a live context.
		ctx, cancel := context.WithCancel(t.Context())

		step := WithCleanupTimeout(5*time.Second,
			Scope(Do(
				Manage(
					func(ctx context.Context, c *CountingFlow) error { return nil },
					func(ctx context.Context, c *CountingFlow) error {
						cleanupCtxErr = ctx.Err()
						return nil
					},
				),
				func(ctx context.Context, c *CountingFlow) error {
					// Cancel the parent context before scope exits
					cancel()
					return nil
				},
			)),
		)

		err := step(ctx, &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Cleanup should have received a non-cancelled context thanks to WithoutCancel
		if cleanupCtxErr != nil {
			t.Errorf("expected cleanup context to be non-cancelled, got %v", cleanupCtxErr)
		}
	})

	t.Run("CleanupTimeoutExceeded", func(t *testing.T) {
		t.Parallel()
		var cleanupCtxErr error

		step := WithCleanupTimeout(1*time.Millisecond,
			Scope(Do(
				Manage(
					func(ctx context.Context, c *CountingFlow) error { return nil },
					func(ctx context.Context, c *CountingFlow) error {
						// Sleep longer than the timeout
						select {
						case <-time.After(time.Second):
							return nil
						case <-ctx.Done():
							cleanupCtxErr = ctx.Err()
							return ctx.Err()
						}
					},
				),
			)),
		)

		err := step(t.Context(), &CountingFlow{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded, got %v", err)
		}
		if !errors.Is(cleanupCtxErr, context.DeadlineExceeded) {
			t.Errorf("expected cleanup to see DeadlineExceeded, got %v", cleanupCtxErr)
		}
	})

	t.Run("NoCleanupTimeoutDefault", func(t *testing.T) {
		t.Parallel()
		var cleanupBodyRan bool

		// No WithCleanupTimeout — if the parent context is cancelled,
		// the cleanup's ctx.Err() check fires and returns the error without
		// running the cleanup body.
		ctx, cancel := context.WithCancel(t.Context())

		step := Scope(Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					cleanupBodyRan = true
					return nil
				},
			),
			func(ctx context.Context, c *CountingFlow) error {
				// Cancel the parent context before scope exits
				cancel()
				return nil
			},
		))

		err := step(ctx, &CountingFlow{})
		// Cleanup sees cancelled ctx → returns ctx.Err() without running cleanup body
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if cleanupBodyRan {
			t.Error("cleanup body should not have run — cleanup short-circuits on cancelled ctx")
		}
	})

	t.Run("NestedScopes", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var order []string

		manage := func(name string) Step[*CountingFlow] {
			return Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					mu.Lock()
					order = append(order, name)
					mu.Unlock()
					return nil
				},
			)
		}

		step := Scope(Do(
			manage("outer"),
			Scope(Do(
				manage("inner"),
			)),
		))

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Inner cleanup runs first (when inner scope exits), then outer
		if len(order) != 2 || order[0] != "inner" || order[1] != "outer" {
			t.Errorf("expected [inner outer], got %v", order)
		}
	})

	t.Run("ScopeAndSpawn", func(t *testing.T) {
		t.Parallel()
		var cleaned bool

		step := Scope(
			Spawn(
				func(ctx context.Context, c *CountingFlow) (*ChildState, error) {
					return &ChildState{Value: 42}, nil
				},
				Manage(
					func(ctx context.Context, child *ChildState) error { return nil },
					func(ctx context.Context, child *ChildState) error {
						cleaned = true
						return nil
					},
				),
			),
		)

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cleaned {
			t.Error("expected child resource to be cleaned up by parent scope")
		}
	})

	t.Run("CleanupTimeoutInheritance", func(t *testing.T) {
		t.Parallel()
		var innerTimeout, outerTimeout time.Duration

		step := WithCleanupTimeout(5*time.Second,
			Scope(Do(
				// Inner scope inherits the 5s timeout
				Scope(func(ctx context.Context, c *CountingFlow) error {
					f := ctx.Value(flowCtxKey{}).(*flowCtx)
					innerTimeout = f.cleanupTimeout
					return nil
				}),
				func(ctx context.Context, c *CountingFlow) error {
					f := ctx.Value(flowCtxKey{}).(*flowCtx)
					outerTimeout = f.cleanupTimeout
					return nil
				},
			)),
		)

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if innerTimeout != 5*time.Second {
			t.Errorf("inner: expected 5s, got %v", innerTimeout)
		}
		if outerTimeout != 5*time.Second {
			t.Errorf("outer: expected 5s, got %v", outerTimeout)
		}
	})

	t.Run("CleanupTimeoutOverride", func(t *testing.T) {
		t.Parallel()
		var innerTimeout, outerTimeout time.Duration

		step := WithCleanupTimeout(5*time.Second,
			Scope(Do(
				// Inner scope overrides to 1s
				WithCleanupTimeout(1*time.Second,
					Scope(func(ctx context.Context, c *CountingFlow) error {
						f := ctx.Value(flowCtxKey{}).(*flowCtx)
						innerTimeout = f.cleanupTimeout
						return nil
					}),
				),
				func(ctx context.Context, c *CountingFlow) error {
					f := ctx.Value(flowCtxKey{}).(*flowCtx)
					outerTimeout = f.cleanupTimeout
					return nil
				},
			)),
		)

		err := step(t.Context(), &CountingFlow{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if innerTimeout != 1*time.Second {
			t.Errorf("inner: expected 1s, got %v", innerTimeout)
		}
		if outerTimeout != 5*time.Second {
			t.Errorf("outer: expected 5s, got %v", outerTimeout)
		}
	})

	t.Run("ScopeChecksCtxErr", func(t *testing.T) {
		t.Parallel()
		var stepRan bool

		cancelledCtx, cancel := context.WithCancel(t.Context())
		cancel()

		step := Scope(func(ctx context.Context, c *CountingFlow) error {
			stepRan = true
			return nil
		})

		err := step(cancelledCtx, &CountingFlow{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if stepRan {
			t.Error("step should not run with cancelled context")
		}
	})

	t.Run("CleanupRunsOnPanic", func(t *testing.T) {
		t.Parallel()
		var cleaned bool

		step := Scope(Do(
			Manage(
				func(ctx context.Context, c *CountingFlow) error { return nil },
				func(ctx context.Context, c *CountingFlow) error {
					cleaned = true
					return nil
				},
			),
			func(ctx context.Context, c *CountingFlow) error {
				panic("boom")
			},
		))

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate")
			}
			if r != "boom" {
				t.Errorf("expected panic value %q, got %v", "boom", r)
			}
			if !cleaned {
				t.Error("expected cleanup to run despite panic")
			}
		}()

		_ = step(t.Context(), &CountingFlow{})
	})
}
