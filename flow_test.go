// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"fmt"
	"testing"
)

func TestFeed(t *testing.T) {
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
				GetCount,
				Feed(
					func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return n + 5, nil
					},
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 5,
			validator:       isNil,
		},
		{
			name: "TransformError",
			step: With(
				GetCount,
				Feed(
					func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return 0, error2
					},
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(error2),
		},
		{
			name: "ConsumeError",
			step: With(
				GetCount,
				Feed(
					func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return n + 5, nil
					},
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return error3
					},
				),
			),
			expectedCounter: 5,
			validator:       matches(error3),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestPipeline(t *testing.T) {
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
				GetCount,
				func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return n + 10, nil
				},
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n
					return nil
				},
			),
			expectedCounter: 10,
			validator:       isNil,
		},
		{
			name: "ExtractError",
			step: Pipeline(
				GetCountFailing,
				func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return n + 10, nil
				},
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n
					return nil
				},
			),
			expectedCounter: 0,
			validator:       matches(error1),
		},
		{
			name: "TransformError",
			step: Pipeline(
				GetCount,
				func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return 0, error2
				},
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n
					return nil
				},
			),
			expectedCounter: 0,
			validator:       matches(error2),
		},
		{
			name: "ConsumeError",
			step: Pipeline(
				GetCount,
				func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return n + 10, nil
				},
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n
					return error3
				},
			),
			expectedCounter: 10,
			validator:       matches(error3),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestWith(t *testing.T) {
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
				GetCount,
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n + 7
					return nil
				},
			),
			expectedCounter: 7,
			validator:       isNil,
		},
		{
			name: "ExtractError",
			step: With(
				GetCountFailing,
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n + 7
					return nil
				},
			),
			expectedCounter: 0,
			validator:       matches(error1),
		},
		{
			name: "ConsumeError",
			step: With(
				GetCount,
				func(_ context.Context, c *CountingFlow, n int64) error {
					c.Counter = n + 7
					return error2
				},
			),
			expectedCounter: 7,
			validator:       matches(error2),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestSpawn(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		step            Step[*CountingFlow]
		expectedCounter int64
		validator       func(error) error
	}{
		{
			name: "Success",
			step: Spawn(
				func(_ context.Context, c *CountingFlow) (*ChildState, error) {
					return &ChildState{Value: c.Counter + 10}, nil
				},
				func(_ context.Context, child *ChildState) error {
					child.Value += 5
					return nil
				},
			),
			expectedCounter: 0,
			validator:       isNil,
		},
		{
			name: "DeriveError",
			step: Spawn(
				func(_ context.Context, c *CountingFlow) (*ChildState, error) {
					return nil, error1
				},
				func(_ context.Context, child *ChildState) error {
					child.Value += 5
					return nil
				},
			),
			expectedCounter: 0,
			validator:       matches(error1),
		},
		{
			name: "StepError",
			step: Spawn(
				func(_ context.Context, c *CountingFlow) (*ChildState, error) {
					return &ChildState{Value: 42}, nil
				},
				func(_ context.Context, child *ChildState) error {
					return error2
				},
			),
			expectedCounter: 0,
			validator:       matches(error2),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestChain(t *testing.T) {
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
				GetCount,
				Feed(
					Chain(
						// int64 -> string
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return fmt.Sprintf("%d", n+5), nil
						},
						// string -> int64
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							var result int64
							_, _ = fmt.Sscanf(s, "%d", &result)
							return result * 2, nil
						},
					),
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 10, // (0+5)*2 = 10
			validator:       isNil,
		},
		{
			name: "FirstTransformError",
			step: With(
				GetCount,
				Feed(
					Chain(
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "", error1
						},
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							return 100, nil
						},
					),
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(error1),
		},
		{
			name: "SecondTransformError",
			step: With(
				GetCount,
				Feed(
					Chain(
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "test", nil
						},
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							return 0, error2
						},
					),
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(error2),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestChain3(t *testing.T) {
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
				GetCount,
				Feed(
					Chain3(
						// int64 -> string
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return fmt.Sprintf("%d", n+3), nil
						},
						// string -> int64
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							var result int64
							_, _ = fmt.Sscanf(s, "%d", &result)
							return result * 2, nil
						},
						// int64 -> string (final)
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return fmt.Sprintf("result:%d", n+1), nil
						},
					),
					func(_ context.Context, c *CountingFlow, s string) error {
						// Parse "result:N" and set counter
						var n int64
						_, _ = fmt.Sscanf(s, "result:%d", &n)
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 7, // ((0+3)*2)+1 = 7
			validator:       isNil,
		},
		{
			name: "TransformError",
			step: With(
				GetCount,
				Feed(
					Chain3(
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "test", nil
						},
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							return 0, error2
						},
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "ok", nil
						},
					),
					func(_ context.Context, c *CountingFlow, s string) error {
						c.Counter = 999
						return nil
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(error2),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestChain4(t *testing.T) {
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
				GetCount,
				Feed(
					Chain4(
						// int64 -> string
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return fmt.Sprintf("%d", n+2), nil
						},
						// string -> int64
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							var result int64
							_, _ = fmt.Sscanf(s, "%d", &result)
							return result * 3, nil
						},
						// int64 -> string
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return fmt.Sprintf("val:%d", n+1), nil
						},
						// string -> int64 (final)
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							var n int64
							_, _ = fmt.Sscanf(s, "val:%d", &n)
							return n * 2, nil
						},
					),
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 14, // (((0+2)*3)+1)*2 = 14
			validator:       isNil,
		},
		{
			name: "TransformError",
			step: With(
				GetCount,
				Feed(
					Chain4(
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "test", nil
						},
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							return 42, nil
						},
						func(_ context.Context, c *CountingFlow, n int64) (string, error) {
							return "", error3
						},
						func(_ context.Context, c *CountingFlow, s string) (int64, error) {
							return 200, nil
						},
					),
					func(_ context.Context, c *CountingFlow, n int64) error {
						c.Counter = n
						return nil
					},
				),
			),
			expectedCounter: 0,
			validator:       matches(error3),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStepTest(t, tc.step, tc.expectedCounter, tc.validator)
		})
	}
}

func TestRender(t *testing.T) {
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
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3, 4, 5}, nil
				},
				Chain(
					Render(func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return n * 2, nil
					}),
					func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
						// Sum all doubled items: 2+4+6+8+10 = 30
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
			expectedCounter: 30,
			validator:       isNil,
		},
		{
			name: "EmptySlice",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{}, nil
				},
				Chain(
					Render(func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						return n * 2, nil
					}),
					func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
						if len(items) != 0 {
							return 0, fmt.Errorf("expected 0 items, got %d", len(items))
						}
						return 42, nil
					},
				),
				func(_ context.Context, c *CountingFlow, sum int64) error {
					c.Counter = sum
					return nil
				},
			),
			expectedCounter: 42,
			validator:       isNil,
		},
		{
			name: "ErrorDuringTransform",
			step: Pipeline(
				func(_ context.Context, c *CountingFlow) ([]int64, error) {
					return []int64{1, 2, 3, 4, 5}, nil
				},
				Chain(
					Render(func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
						if n == 3 {
							return 0, error2
						}
						c.Counter++ // Track how many we processed before error
						return n * 2, nil
					}),
					func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
						return 999, nil // Should not reach here
					},
				),
				func(_ context.Context, c *CountingFlow, sum int64) error {
					c.Counter = 888 // Should not reach here
					return nil
				},
			),
			expectedCounter: 2, // Should have processed items 1 and 2 before failing on 3
			validator:       matches(error2),
		},
		{
			name: "ContextCancellation",
			step: func(ctx context.Context, c *CountingFlow) error {
				ctx, cancel := context.WithCancel(ctx)

				return Pipeline(
					func(_ context.Context, c *CountingFlow) ([]int64, error) {
						return []int64{1, 2, 3, 4, 5}, nil
					},
					Chain(
						Render(func(ctx context.Context, c *CountingFlow, n int64) (int64, error) {
							if n == 3 {
								cancel() // Cancel on third item
							}
							c.Counter++ // Track how many we attempted
							return n * 2, nil
						}),
						func(_ context.Context, c *CountingFlow, items []int64) (int64, error) {
							return 999, nil // Should not reach here
						},
					),
					func(_ context.Context, c *CountingFlow, sum int64) error {
						c.Counter = 888 // Should not reach here
						return nil
					},
				)(ctx, c)
			},
			expectedCounter: 3, // Should have processed items 1, 2, 3 before cancellation check
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
