// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFromMap(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "EmptyList",
			step: InSerial(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{}, nil
					},
					Increment,
				),
			),
			expectedCounter: 0,
			validator:       isNil,
		},
		{
			name: "MultipleItems",
			step: InSerial(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{1, 2, 3, 4, 5}, nil
					},
					Increment,
				),
			),
			expectedCounter: 15, // 1 + 2 + 3 + 4 + 5
			validator:       isNil,
		},
		{
			name: "AccessesState",
			step: InSerial(
				Steps(Increment(10)),
				ForEach(
					func(_ context.Context, c *CountingFlow) ([]int64, error) {
						// Returns list based on current state
						if c.Counter >= 10 {
							return []int64{1, 2}, nil
						}
						return []int64{}, nil
					},
					Increment,
				),
			),
			expectedCounter: 13, // 10 + 1 + 2
			validator:       isNil,
		},
		{
			name: "StopsOnError",
			step: InSerial(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]error, error) {
						return []error{nil, error1, error2}, nil
					},
					IncrementAndFail,
				),
			),
			// incremented for nil and error1, stopped at error1
			expectedCounter: 2,
			validator:       matches(error1),
		},
		{
			name: "GetterError",
			step: InSerial(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return nil, error1
					},
					Increment,
				),
			),
			expectedCounter: 0,
			validator:       matches(error1),
		},
		{
			name: "NilStep",
			step: InSerial(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{1, 2, 3}, nil
					},
					func(n int64) Step[*CountingFlow] {
						if n == 2 {
							return nil
						}
						return Increment(n)
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(ErrNilStep),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestEachParallel(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
	}{
		{
			name: "EmptyList",
			step: InParallel(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{}, nil
					},
					Increment,
				),
			),
			expectedCounter: 0,
		},
		{
			name: "MultipleItems",
			step: InParallel(
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{1, 2, 3, 4, 5}, nil
					},
					Increment,
				),
			),
			// 1 + 2 + 3 + 4 + 5
			expectedCounter: 15,
		},
		{
			name: "WithOptions",
			step: InParallelWith(
				ParallelOptions{Limit: 2},
				ForEach(
					func(_ context.Context, _ *CountingFlow) ([]int64, error) {
						return []int64{5, 10, 15}, nil
					},
					Increment,
				),
			),
			// 5 + 10 + 15
			expectedCounter: 30,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			err := tc.step(t.Context(), &c)
			if err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
			if c.Counter != tc.expectedCounter {
				t.Errorf("got counter %d, want %d",
					c.Counter, tc.expectedCounter)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "Success",
			step: With(
				Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
					if c.Counter >= 5 {
						return 0, ErrExhausted
					}
					c.Counter++
					return c.Counter, nil
				}),
				func(_ context.Context, c *CountingFlow, items []int64) error {
					// Verify we collected 5 items: [1, 2, 3, 4, 5]
					if len(items) != 5 {
						return fmt.Errorf("expected 5 items, got %d", len(items))
					}
					for i, item := range items {
						if item != int64(i+1) {
							return fmt.Errorf("expected item[%d] = %d, got %d", i, i+1, item)
						}
					}
					return nil
				},
			),
			expectedCounter: 5,
			validator:       isNil,
		},
		{
			name: "EmptyCollection",
			step: With(
				Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
					return 0, ErrExhausted
				}),
				func(_ context.Context, c *CountingFlow, items []int64) error {
					if len(items) != 0 {
						return fmt.Errorf("expected 0 items, got %d", len(items))
					}
					c.Counter = 42
					return nil
				},
			),
			expectedCounter: 42,
			validator:       isNil,
		},
		{
			name: "ErrorDuringExtraction",
			step: With(
				Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
					if c.Counter >= 3 {
						return 0, error1
					}
					c.Counter++
					return c.Counter, nil
				}),
				func(_ context.Context, c *CountingFlow, items []int64) error {
					c.Counter = 999 // Should not reach here
					return nil
				},
			),
			expectedCounter: 3,
			validator:       matches(error1),
		},
		{
			name: "ContextCancellation",
			step: func(ctx context.Context, c *CountingFlow) error {
				ctx, cancel := context.WithCancel(ctx)
				cancel() // Cancel immediately

				return With(
					Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
						c.Counter++
						return c.Counter, nil
					}),
					func(_ context.Context, c *CountingFlow, items []int64) error {
						c.Counter = 999 // Should not reach here
						return nil
					},
				)(ctx, c)
			},
			expectedCounter: 0,
			validator:       matches(context.Canceled),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "Success",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3, 4, 5}, nil
				},
				Apply(func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter += n
					return nil
				}),
			),
			expectedCounter: 15, // 1+2+3+4+5
			validator:       isNil,
		},
		{
			name: "EmptySlice",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					c.Counter = 42
					return []int64{}, nil
				},
				Apply(func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = 999 // Should not be called
					return nil
				}),
			),
			expectedCounter: 42,
			validator:       isNil,
		},
		{
			name: "ErrorDuringConsume",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3, 4, 5}, nil
				},
				Apply(func(_ context.Context, c *CountingFlow, n int64) error {
					if n == 3 {
						return error3
					}
					c.Counter++ // Track how many we processed before error
					return nil
				}),
			),
			expectedCounter: 2, // Should have processed items 1 and 2 before failing on 3
			validator:       matches(error3),
		},
		{
			name: "ComposedWithFeed",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3}, nil
				},
				Apply(Feed(
					func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return n * 10, nil
					},
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter += n
						return nil
					},
				)),
			),
			expectedCounter: 60, // (1*10) + (2*10) + (3*10) = 60
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

func TestFlatten(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "Success",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([][]int64, error) {
					return [][]int64{{1, 2}, {3, 4}, {5}}, nil
				},
				Chain(
					Flatten[*CountingFlow, int64],
					func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
						// Verify flattened to [1, 2, 3, 4, 5]
						if len(items) != 5 {
							return 0, fmt.Errorf("expected 5 items, got %d", len(items))
						}
						for i, item := range items {
							if item != int64(i+1) {
								return 0, fmt.Errorf("expected item[%d] = %d, got %d", i, i+1, item)
							}
						}
						// Sum all items
						var sum int64
						for _, item := range items {
							sum += item
						}
						return sum, nil
					},
				),
				func(_ context.Context, c *CountingFlow, sum int64) error {
					c.Counter = sum
					return nil
				},
			),
			expectedCounter: 15, // 1+2+3+4+5
			validator:       isNil,
		},
		{
			name: "EmptySlice",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([][]int64, error) {
					return [][]int64{}, nil
				},
				Chain(
					Flatten[*CountingFlow, int64],
					func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
						if len(items) != 0 {
							return 0, fmt.Errorf("expected 0 items, got %d", len(items))
						}
						return 42, nil
					},
				),
				func(_ context.Context, c *CountingFlow, val int64) error {
					c.Counter = val
					return nil
				},
			),
			expectedCounter: 42,
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

func TestRenderParallel(t *testing.T) {
	t.Parallel()

	t.Run("PreservesOrder", func(t *testing.T) {
		t.Parallel()
		items := []int64{10, 20, 30, 40, 50}

		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				return n * 2, nil
			},
			ParallelOptions{},
		)

		results, err := transform(t.Context(), &CountingFlow{}, items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := []int64{20, 40, 60, 80, 100}
		for i, got := range results {
			if got != expected[i] {
				t.Errorf("results[%d] = %d, want %d", i, got, expected[i])
			}
		}
	})

	t.Run("IndexedErrorOnFailure", func(t *testing.T) {
		t.Parallel()
		items := []int64{1, 2, 3, 4, 5}

		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				if n == 3 {
					return 0, error1
				}
				return n, nil
			},
			ParallelOptions{Limit: 1}, // serialize to ensure deterministic failure
		)

		_, err := transform(t.Context(), &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var ie *IndexedError
		if !errors.As(err, &ie) {
			t.Fatalf("expected IndexedError, got %T: %v", err, err)
		}
		if ie.Index != 2 {
			t.Errorf("expected index 2, got %d", ie.Index)
		}
		if !errors.Is(err, error1) {
			t.Errorf("expected wrapped error to match error1")
		}
	})

	t.Run("ContextCancellationStopsScheduling", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately

		items := []int64{1, 2, 3}
		var count atomic.Int64

		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				count.Add(1)
				return n, nil
			},
			ParallelOptions{},
		)

		_, err := transform(ctx, &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("JoinErrorsCollectsAll", func(t *testing.T) {
		t.Parallel()
		items := []int64{1, 2, 3, 4, 5}

		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				if n%2 == 0 {
					return 0, fmt.Errorf("fail-%d", n)
				}
				return n, nil
			},
			ParallelOptions{JoinErrors: true},
		)

		_, err := transform(t.Context(), &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// Should contain errors for elements 2 and 4
		var ie2, ie4 *IndexedError
		for _, wrapped := range []error{err} {
			for _, inner := range unwrapAll(wrapped) {
				var ie *IndexedError
				if errors.As(inner, &ie) {
					switch ie.Index {
					case 1:
						ie2 = ie
					case 3:
						ie4 = ie
					}
				}
			}
		}
		if ie2 == nil {
			t.Error("expected IndexedError for index 1 (element 2)")
		}
		if ie4 == nil {
			t.Error("expected IndexedError for index 3 (element 4)")
		}
	})

	t.Run("LimitBoundsConcurrency", func(t *testing.T) {
		t.Parallel()
		items := make([]int64, 20)
		for i := range items {
			items[i] = int64(i)
		}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				cur := current.Add(1)
				// Track max
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				current.Add(-1)
				return n, nil
			},
			ParallelOptions{Limit: 3},
		)

		results, err := transform(t.Context(), &CountingFlow{}, items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != len(items) {
			t.Errorf("expected %d results, got %d", len(items), len(results))
		}
		if maxConcurrent.Load() > 3 {
			t.Errorf("max concurrency %d exceeded limit 3", maxConcurrent.Load())
		}
	})

	t.Run("EmptySlice", func(t *testing.T) {
		t.Parallel()
		transform := RenderParallel(
			func(_ context.Context, _ *CountingFlow, n int64) (int64, error) {
				t.Fatal("should not be called")
				return 0, nil
			},
			ParallelOptions{},
		)

		results, err := transform(t.Context(), &CountingFlow{}, []int64{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected empty results, got %d", len(results))
		}
	})
}

// unwrapAll returns all individual errors from a joined error.
func unwrapAll(err error) []error {
	type joinedError interface {
		Unwrap() []error
	}
	if je, ok := err.(joinedError); ok {
		return je.Unwrap()
	}
	return []error{err}
}

func TestRenderIndexedError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name: "WrapsErrorWithIndex",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3, 4, 5}, nil
				},
				Render(func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					if n == 3 {
						return 0, error1
					}
					return n * 10, nil
				}),
				func(_ context.Context, c *CountingFlow, results []int64) error {
					return nil // Just a no-op consume
				},
			),
			validator: func(err error) error {
				var ie *IndexedError
				if !errors.As(err, &ie) {
					return fmt.Errorf("expected IndexedError, got %T: %v", err, err)
				}
				if ie.Index != 2 {
					return fmt.Errorf("expected index 2, got %d", ie.Index)
				}
				if !errors.Is(err, error1) {
					return fmt.Errorf("expected wrapped error to match error1")
				}
				return nil
			},
		},
		{
			name: "PreservesErrorIsSemantics",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{10, 20}, nil
				},
				Render(func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					if n == 20 {
						return 0, error3
					}
					return n, nil
				}),
				func(_ context.Context, c *CountingFlow, results []int64) error {
					return nil // Just a no-op consume
				},
			),
			validator: func(err error) error {
				// errors.Is should still work through Unwrap()
				if !errors.Is(err, error3) {
					return fmt.Errorf("errors.Is failed: expected to find error3 in %v", err)
				}
				return nil
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, 0, tc.validator)
		})
	}
}

func TestApplyParallel(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		items := []int64{1, 2, 3, 4, 5}
		var sum atomic.Int64

		consume := ApplyParallel(
			func(_ context.Context, _ *CountingFlow, n int64) error {
				sum.Add(n)
				return nil
			},
			ParallelOptions{},
		)

		err := consume(t.Context(), &CountingFlow{}, items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sum.Load() != 15 {
			t.Errorf("expected sum 15, got %d", sum.Load())
		}
	})

	t.Run("IndexedErrorOnFailure", func(t *testing.T) {
		t.Parallel()
		items := []int64{10, 20, 30}

		consume := ApplyParallel(
			func(_ context.Context, _ *CountingFlow, n int64) error {
				if n == 20 {
					return error2
				}
				return nil
			},
			ParallelOptions{Limit: 1},
		)

		err := consume(t.Context(), &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var ie *IndexedError
		if !errors.As(err, &ie) {
			t.Fatalf("expected IndexedError, got %T: %v", err, err)
		}
		if ie.Index != 1 {
			t.Errorf("expected index 1, got %d", ie.Index)
		}
		if !errors.Is(err, error2) {
			t.Errorf("expected wrapped error to match error2")
		}
	})

	t.Run("ContextCancellationStopsScheduling", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		items := []int64{1, 2, 3}
		consume := ApplyParallel(
			func(_ context.Context, _ *CountingFlow, n int64) error {
				return nil
			},
			ParallelOptions{},
		)

		err := consume(ctx, &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("JoinErrorsCollectsAll", func(t *testing.T) {
		t.Parallel()
		items := []int64{1, 2, 3}

		consume := ApplyParallel(
			func(_ context.Context, _ *CountingFlow, n int64) error {
				return fmt.Errorf("fail-%d", n)
			},
			ParallelOptions{JoinErrors: true},
		)

		err := consume(t.Context(), &CountingFlow{}, items)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// All three should have produced IndexedErrors
		errStr := err.Error()
		for _, needle := range []string{"element 0", "element 1", "element 2"} {
			if !strings.Contains(errStr, needle) {
				t.Errorf("expected error to contain %q, got %q", needle, errStr)
			}
		}
	})

	t.Run("LimitBoundsConcurrency", func(t *testing.T) {
		t.Parallel()
		items := make([]int64, 20)
		for i := range items {
			items[i] = int64(i)
		}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

		consume := ApplyParallel(
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
			ParallelOptions{Limit: 3},
		)

		err := consume(t.Context(), &CountingFlow{}, items)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if maxConcurrent.Load() > 3 {
			t.Errorf("max concurrency %d exceeded limit 3", maxConcurrent.Load())
		}
	})

	t.Run("EmptySlice", func(t *testing.T) {
		t.Parallel()
		consume := ApplyParallel(
			func(_ context.Context, _ *CountingFlow, n int64) error {
				t.Fatal("should not be called")
				return nil
			},
			ParallelOptions{},
		)

		err := consume(t.Context(), &CountingFlow{}, []int64{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
func TestApplyIndexedError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "WrapsErrorWithIndex",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{10, 20, 30}, nil
				},
				Apply(func(_ context.Context, c *CountingFlow, n int64) error {
					if n == 30 {
						return error2
					}
					c.Counter += n
					return nil
				}),
			),
			expectedCounter: 30, // 10 + 20
			validator: func(err error) error {
				var ie *IndexedError
				if !errors.As(err, &ie) {
					return fmt.Errorf("expected IndexedError, got %T: %v", err, err)
				}
				if ie.Index != 2 {
					return fmt.Errorf("expected index 2, got %d", ie.Index)
				}
				if !errors.Is(err, error2) {
					return fmt.Errorf("expected wrapped error to match error2")
				}
				return nil
			},
		},
		{
			name: "PreservesErrorIsSemantics",
			step: With(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3}, nil
				},
				Apply(func(_ context.Context, c *CountingFlow, n int64) error {
					if n == 2 {
						return error1
					}
					return nil
				}),
			),
			expectedCounter: 0,
			validator: func(err error) error {
				// errors.Is should still work through Unwrap()
				if !errors.Is(err, error1) {
					return fmt.Errorf("errors.Is failed: expected to find error1 in %v", err)
				}
				return nil
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestCollectIndexedError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "WrapsErrorWithIndex",
			step: With(
				Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
					if c.Counter >= 3 {
						return 0, error1
					}
					c.Counter++
					return c.Counter, nil
				}),
				func(_ context.Context, c *CountingFlow, items []int64) error {
					c.Counter = 999 // Should not reach here
					return nil
				},
			),
			expectedCounter: 3,
			validator: func(err error) error {
				var ie *IndexedError
				if !errors.As(err, &ie) {
					return fmt.Errorf("expected IndexedError, got %T: %v", err, err)
				}
				if ie.Index != 3 {
					return fmt.Errorf("expected index 3, got %d", ie.Index)
				}
				if !errors.Is(err, error1) {
					return fmt.Errorf("expected wrapped error to match error1")
				}
				return nil
			},
		},
		{
			name: "ExhaustedNotWrapped",
			step: With(
				Collect(func(ctx context.Context, c *CountingFlow) (int64, error) {
					if c.Counter >= 2 {
						return 0, ErrExhausted
					}
					c.Counter++
					return c.Counter, nil
				}),
				func(_ context.Context, c *CountingFlow, items []int64) error {
					if len(items) != 2 {
						return fmt.Errorf("expected 2 items, got %d", len(items))
					}
					return nil
				},
			),
			expectedCounter: 2,
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
