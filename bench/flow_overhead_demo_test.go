// SPDX-License-Identifier: Apache-2.0

package flow_test

import (
	"context"
	"testing"

	"github.com/sam-fredrickson/flow"
)

// This file demonstrates where the compositional overhead comes from
// by comparing direct calls vs closure-based composition.

type SimpleState struct {
	counter int
}

// Direct function - no closure.
// Made complex enough to prevent inlining.
//
//go:noinline
func incrementDirect(ctx context.Context, s *SimpleState) error {
	// Add enough operations to prevent inlining
	s.counter++
	if s.counter < 0 {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// Step constructor - returns closure.
// The returned closure is complex enough to prevent inlining.
//
//go:noinline
func IncrementClosure() flow.Step[*SimpleState] {
	return func(ctx context.Context, s *SimpleState) error {
		// Add enough operations to prevent inlining
		s.counter++
		if s.counter < 0 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
}

// Package-level variable to prevent compiler from eliminating benchmarks.
var benchmarkResult int

// Benchmark 1: Direct function calls (traditional style).
func BenchmarkDirect_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		// 10 direct function calls with error checking (matches what Do does)
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkResult = s.counter
}

// Benchmark 2: Pre-constructed closures called manually (flow style, no combinator).
func BenchmarkClosure_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	// Pre-construct all closures outside the benchmark
	step1 := IncrementClosure()
	step2 := IncrementClosure()
	step3 := IncrementClosure()
	step4 := IncrementClosure()
	step5 := IncrementClosure()
	step6 := IncrementClosure()
	step7 := IncrementClosure()
	step8 := IncrementClosure()
	step9 := IncrementClosure()
	step10 := IncrementClosure()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		// 10 closure calls (pre-constructed) with error checking
		if err := step1(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step2(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step3(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step4(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step5(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step6(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step7(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step8(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step9(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := step10(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkResult = s.counter
}

// Benchmark 3: Using Do combinator (pre-constructed workflow).
func BenchmarkFlowDo_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	workflow := flow.Do(
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
		IncrementClosure(),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		_ = workflow(ctx, s)
	}
	benchmarkResult = s.counter
}

// ============================================================================
// Construction + Execution Benchmarks (workflow built inside loop)
// ============================================================================

// Benchmark 4: Direct function calls with construction (baseline - no construction cost).
func BenchmarkDirectCreation_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		// Direct calls have no construction phase, but include error checking
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
		if err := incrementDirect(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkResult = s.counter
}

// Benchmark 5: Closure creation + calls (measures creation overhead).
func BenchmarkClosureCreation_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		// Create 10 closures + call them with error checking
		step1 := IncrementClosure()
		if err := step1(ctx, s); err != nil {
			b.Fatal(err)
		}
		step2 := IncrementClosure()
		if err := step2(ctx, s); err != nil {
			b.Fatal(err)
		}
		step3 := IncrementClosure()
		if err := step3(ctx, s); err != nil {
			b.Fatal(err)
		}
		step4 := IncrementClosure()
		if err := step4(ctx, s); err != nil {
			b.Fatal(err)
		}
		step5 := IncrementClosure()
		if err := step5(ctx, s); err != nil {
			b.Fatal(err)
		}
		step6 := IncrementClosure()
		if err := step6(ctx, s); err != nil {
			b.Fatal(err)
		}
		step7 := IncrementClosure()
		if err := step7(ctx, s); err != nil {
			b.Fatal(err)
		}
		step8 := IncrementClosure()
		if err := step8(ctx, s); err != nil {
			b.Fatal(err)
		}
		step9 := IncrementClosure()
		if err := step9(ctx, s); err != nil {
			b.Fatal(err)
		}
		step10 := IncrementClosure()
		if err := step10(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkResult = s.counter
}

// Benchmark 6: Do combinator creation + execution (measures full overhead).
func BenchmarkFlowDoCreation_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		// Create workflow + execute (all inside loop)
		workflow := flow.Do(
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
			IncrementClosure(),
		)
		_ = workflow(ctx, s)
	}
	benchmarkResult = s.counter
}

// ============================================================================
// Additional Benchmarks
// ============================================================================

// Benchmark 7: Nested combinators (realistic flow usage - pre-constructed).
func BenchmarkFlowNested_10Steps(b *testing.B) {
	ctx := context.Background()
	var s *SimpleState

	// Simulates a more realistic flow with nested combinators
	workflow := flow.InSerial(
		flow.Steps(
			IncrementClosure(),
			IncrementClosure(),
		),
		flow.Steps(
			IncrementClosure(),
			IncrementClosure(),
		),
		flow.Steps(
			IncrementClosure(),
			IncrementClosure(),
		),
		flow.Steps(
			IncrementClosure(),
			IncrementClosure(),
		),
		flow.Steps(
			IncrementClosure(),
			IncrementClosure(),
		),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = &SimpleState{}
		_ = workflow(ctx, s)
	}
	benchmarkResult = s.counter
}
