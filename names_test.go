// SPDX-License-Identifier: Apache-2.0

package flow

import (
	"context"
	"errors"
	"testing"
)

func TestNamed(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name: "StepNoError",
			step: Do(
				Named("first", Increment(1)),
			),
			validator: isNil,
		},
		{
			name: "StepNameInErrorMessage",
			step: Do(
				Named("first", IncrementAndFail(errorNonRetryable)),
			),
			validator: all(
				matches(errorNonRetryable),
				contains("first"),
			),
		},
		{
			name: "ExtractNoError",
			step: With(
				NamedExtract("GetCount", GetCount),
				SendCount,
			),
			validator: isNil,
		},
		{
			name: "ExtractNameInErrorMessage",
			step: With(
				NamedExtract("GetCount", GetCountFailing),
				SendCount,
			),
			validator: all(
				matches(error1),
				contains("GetCount"),
			),
		},
		{
			name: "TransformNoError",
			step: Pipeline(
				GetCount,
				NamedTransform("AddTen", func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return n + 10, nil
				}),
				SendCount,
			),
			validator: isNil,
		},
		{
			name: "TransformNameInErrorMessage",
			step: Pipeline(
				GetCount,
				NamedTransform("FailingTransform", func(_ context.Context, c *CountingFlow, n int64) (int64, error) {
					return 0, error1
				}),
				SendCount,
			),
			validator: all(
				matches(error1),
				contains("FailingTransform"),
			),
		},
		{
			name: "ConsumeNoError",
			step: With(
				GetCount,
				NamedConsume("StoreCount", func(_ context.Context, c *CountingFlow, n int64) error {
					return nil
				}),
			),
			validator: isNil,
		},
		{
			name: "ConsumeNameInErrorMessage",
			step: With(
				GetCount,
				NamedConsume("FailingConsume", func(_ context.Context, c *CountingFlow, n int64) error {
					return error2
				}),
			),
			validator: all(
				matches(error2),
				contains("FailingConsume"),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			testErr := tc.step(t.Context(), &c)
			if err := tc.validator(testErr); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestAutoNamed(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		step      Step[*CountingFlow]
		validator func(error) error
	}{
		{
			name:      "Step",
			step:      testAutoNamedHelper(),
			validator: contains("testAutoNamedHelper"),
		},
		{
			name: "Extract",
			step: With(
				testAutoNamedExtractHelper(),
				func(_ context.Context, _ *CountingFlow, _ int64) error {
					return nil
				},
			),
			validator: contains("testAutoNamedExtractHelper"),
		},
		{
			name: "Transform",
			step: Pipeline(
				func(ctx context.Context, t *CountingFlow) (int64, error) {
					return 0, nil
				},
				testAutoNamedTransformHelper(),
				func(ctx context.Context, t *CountingFlow, i int64) error {
					return nil
				},
			),
			validator: contains("testAutoNamedTransformHelper"),
		},
		{
			name: "Consume",
			step: With(
				func(ctx context.Context, t *CountingFlow) (int64, error) {
					return 0, nil
				},
				testAutoNamedConsumeHelper(),
			),
			validator: contains("testAutoNamedConsumeHelper"),
		},
		{
			name:      "MegaWrapper",
			step:      testMegaWrapper(),
			validator: contains("testMegaWrapper"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var c CountingFlow
			testErr := tc.step(t.Context(), &c)
			if testErr == nil {
				t.Fatal("expected error, got nil")
			}
			err := tc.validator(testErr)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func testAutoNamedHelper() Step[*CountingFlow] {
	return AutoNamed(func(ctx context.Context, c *CountingFlow) error {
		return errors.New("test error")
	})
}

func testAutoNamedExtractHelper() Extract[*CountingFlow, int64] {
	return AutoNamedExtract(func(ctx context.Context, c *CountingFlow) (int64, error) {
		return 0, errors.New("test error")
	})
}

func testAutoNamedConsumeHelper() Consume[*CountingFlow, int64] {
	return AutoNamedConsume(func(ctx context.Context, c *CountingFlow, _ int64) error {
		return errors.New("test error")
	})
}

func testAutoNamedTransformHelper() Transform[*CountingFlow, int64, int64] {
	return AutoNamedTransform(func(ctx context.Context, c *CountingFlow, _ int64) (int64, error) {
		return 0, errors.New("test error")
	})
}

func testMegaWrapper() Step[*CountingFlow] {
	return megaWrapper(func(_ context.Context, c *CountingFlow) error {
		return errors.New("test error")
	})
}

func megaWrapper(step Step[*CountingFlow]) Step[*CountingFlow] {
	return AutoNamed(Retry(step), SkipCaller(1))
}

func TestExtractFunctionName(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		fullName string
		expected string
	}{
		{
			fullName: "github.com/sam-fredrickson/flow.CreateDatabase",
			expected: "CreateDatabase",
		},
		{
			fullName: "main.DoWork",
			expected: "DoWork",
		},
		{
			fullName: "github.com/user/repo/pkg.HandleRequest",
			expected: "HandleRequest",
		},
		{
			fullName: "main.(*Server).HandleRequest",
			expected: "HandleRequest",
		},
		{
			fullName: "pkg.(Server).HandleRequest",
			expected: "HandleRequest",
		},
		{
			fullName: "github.com/sam-fredrickson/flow/internal/example2.CreateAwsAccount",
			expected: "CreateAwsAccount",
		},
		{
			fullName: "SimpleName",
			expected: "SimpleName",
		},
		{
			fullName: "package/NoDot",
			expected: "NoDot",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.fullName, func(t *testing.T) {
			t.Parallel()
			result := extractFunctionName(tc.fullName)
			if result != tc.expected {
				t.Errorf("extractFunctionName(%q) = %q, want %q", tc.fullName, result, tc.expected)
			}
		})
	}
}
