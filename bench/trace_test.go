// SPDX-License-Identifier: Apache-2.0

package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/sam-fredrickson/flow"
)

// CountingFlow is a simple test state for tracing benchmarks.
type CountingFlow struct {
	Counter int64
}

// Increment returns a step that atomically adds n to the counter.
func Increment(n int64) flow.Step[*CountingFlow] {
	return func(ctx context.Context, c *CountingFlow) error {
		atomic.AddInt64(&c.Counter, n)
		return nil
	}
}

// =============================================================================
// Tracing Benchmarks
// =============================================================================

// Benchmark Named without tracing (baseline).
func BenchmarkNamedWithoutTrace(b *testing.B) {
	step := flow.Named("test", Increment(1))
	ctx := b.Context()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := &CountingFlow{}
		if err := step(ctx, state); err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark Named with tracing enabled.
func BenchmarkNamedWithTrace(b *testing.B) {
	step := flow.Named("test", Increment(1))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := b.Context()
		state := &CountingFlow{}
		_, err := flow.Traced(step)(ctx, state)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark deeply nested Named calls to measure O(n) context lookup cost.
func BenchmarkNamedDeeplyNested(b *testing.B) {
	depths := []int{5, 10, 20, 50}

	for _, depth := range depths {
		depthName := fmt.Sprintf("depth_%02d", depth)
		b.Run(depthName, func(b *testing.B) {
			// Build nested workflow
			var step = Increment(1)
			for i := 0; i < depth; i++ {
				levelName := fmt.Sprintf("level%02d", i)
				step = flow.Named(levelName, step)
			}

			b.Run("without_trace", func(b *testing.B) {
				ctx := b.Context()

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					state := &CountingFlow{}
					if err := step(ctx, state); err != nil {
						b.Fatal(err)
					}
				}
			})

			b.Run("with_trace", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ctx := b.Context()
					state := &CountingFlow{}
					_, err := flow.Traced(step)(ctx, state)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// Benchmark concurrent access to trace (parallel step recording).
func BenchmarkTraceConcurrentAccess(b *testing.B) {
	parallelCounts := []int{4, 10, 50, 100}

	for _, count := range parallelCounts {
		countName := fmt.Sprintf("parallel_%03d", count)
		b.Run(countName, func(b *testing.B) {
			// Build workflow with many parallel steps
			steps := make([]flow.Step[*CountingFlow], count)
			for i := 0; i < count; i++ {
				stepName := fmt.Sprintf("step%03d", i)
				steps[i] = flow.Named(stepName, Increment(1))
			}
			workflow := flow.InParallel(flow.Steps(steps...))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := b.Context()
				state := &CountingFlow{}
				_, err := flow.Traced(workflow)(ctx, state)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Benchmark filter operations on large traces.
func BenchmarkTraceFilter(b *testing.B) {
	eventCounts := []int{100, 500, 1000}

	for _, eventCount := range eventCounts {
		countName := fmt.Sprintf("events_%04d", eventCount)
		b.Run(countName, func(b *testing.B) {
			// Create workflow with many events
			steps := make([]flow.Step[*CountingFlow], eventCount)
			for i := 0; i < eventCount; i++ {
				stepName := fmt.Sprintf("step%04d", i)
				steps[i] = flow.Named(stepName, Increment(1))
			}
			workflow := flow.Do(steps...)

			// Generate trace once
			ctx := b.Context()
			state := &CountingFlow{}
			trace, err := flow.Traced(workflow)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}

			b.Run("single_filter", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = trace.Filter(flow.DepthEquals(1))
				}
			})

			b.Run("multiple_filters", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = trace.Filter(
						flow.DepthEquals(1),
						flow.NoError(),
						flow.MinDuration(0),
					)
				}
			})

			b.Run("pattern_match", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = trace.Filter(flow.NameMatches("step*"))
				}
			})
		})
	}
}

// Benchmark trace output operations.
func BenchmarkTraceOutput(b *testing.B) {
	// Create a moderately complex trace
	workflow := flow.Named("parent", flow.Do(
		flow.Named("child1", Increment(1)),
		flow.Named("child2", flow.Do(
			flow.Named("grandchild1", Increment(1)),
			flow.Named("grandchild2", Increment(1)),
		)),
		flow.Named("child3", Increment(1)),
	))

	ctx := b.Context()
	state := &CountingFlow{}
	trace, err := flow.Traced(workflow)(ctx, state)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("WriteTo_JSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			_, err := trace.WriteTo(&buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("WriteText", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			_, err := trace.WriteText(&buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkStreamingOverhead measures the overhead of streaming events.
func BenchmarkStreamingOverhead(b *testing.B) {
	eventCounts := []int{10, 50, 100}

	for _, count := range eventCounts {
		countName := fmt.Sprintf("events_%03d", count)

		// Build workflow with many steps
		steps := make([]flow.Step[*CountingFlow], count)
		for i := 0; i < count; i++ {
			stepName := fmt.Sprintf("step%03d", i)
			steps[i] = flow.Named(stepName, Increment(1))
		}
		workflow := flow.Do(steps...)

		b.Run(countName+"/no_streaming", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := b.Context()
				state := &CountingFlow{}
				_, err := flow.Traced(workflow)(ctx, state)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(countName+"/with_streaming", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := b.Context()
				state := &CountingFlow{}
				var buf bytes.Buffer
				_, err := flow.Traced(workflow, flow.WithStreamTo(&buf))(ctx, state)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTraceMemoryAllocations measures memory allocations per event.
func BenchmarkTraceMemoryAllocations(b *testing.B) {
	// Single event
	b.Run("single_event", func(b *testing.B) {
		step := flow.Named("test", Increment(1))

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			_, err := flow.Traced(step)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Nested events
	b.Run("nested_events", func(b *testing.B) {
		step := flow.Named("parent", flow.Do(
			flow.Named("child1", Increment(1)),
			flow.Named("child2", Increment(1)),
			flow.Named("child3", Increment(1)),
		))

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			_, err := flow.Traced(step)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Deep nesting
	b.Run("deep_nesting_20", func(b *testing.B) {
		var step = Increment(1)
		for i := 0; i < 20; i++ {
			name := fmt.Sprintf("level%02d", i)
			step = flow.Named(name, step)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			_, err := flow.Traced(step)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTracingOverheadComparison directly compares traced vs untraced overhead.
func BenchmarkTracingOverheadComparison(b *testing.B) {
	// Simple workflow
	simpleWorkflow := flow.Do(
		flow.Named("step1", Increment(1)),
		flow.Named("step2", Increment(1)),
		flow.Named("step3", Increment(1)),
	)

	b.Run("simple/untraced", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			if err := simpleWorkflow(ctx, state); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("simple/traced", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			_, err := flow.Traced(simpleWorkflow)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	// Complex workflow with nesting
	complexWorkflow := flow.Named("parent", flow.Do(
		flow.Named("child1", Increment(1)),
		flow.Named("child2", flow.Do(
			flow.Named("grandchild1", Increment(1)),
			flow.Named("grandchild2", Increment(1)),
		)),
		flow.Named("child3", Increment(1)),
	))

	b.Run("complex/untraced", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			if err := complexWorkflow(ctx, state); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("complex/traced", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := b.Context()
			state := &CountingFlow{}
			_, err := flow.Traced(complexWorkflow)(ctx, state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
