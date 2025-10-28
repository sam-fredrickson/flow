# Flow Library Benchmarks

This directory contains benchmarks comparing the flow library's compositional approach against traditional Go code.

## Quick Start

From the repository root:
```bash
go test -bench=. -benchmem ./bench/...
```

Or if you have a justfile configured:
```bash
just bench
```

## Overview

The benchmarks measure the performance overhead (if any) of using the flow library's higher-order functions and compositional combinators versus hand-written imperative Go code. Both implementations perform identical operations, allowing for direct performance comparison.

## Benchmark Workflow

The benchmarks simulate a realistic DevOps deployment workflow with the following characteristics:

**Sequential steps:**
- Configuration validation
- Conditional database setup
- Health checks
- DNS updates (with retry logic)
- Best-effort notifications

**Parallel execution:**
- Deploying multiple services concurrently

**Control flow features:**
- Conditionals (`When`)
- Retry with exponential backoff
- Error ignoring (`IgnoreError`)
- Dynamic step generation (`From`/`Map`)

This workflow exercises the core patterns of the library while remaining realistic for the target use case.

## Configurable I/O Simulation

Each benchmark can be run with different simulated I/O delays:

- **`cpu_bound`** (0µs): Pure CPU overhead measurement - shows the cost of compositional abstractions
- **`io_1us`** (1µs): Very fast I/O operations (e.g., memory cache lookups)
- **`io_10us`** (10µs): Fast I/O operations (e.g., local Redis)
- **`io_100us`** (100µs): Moderate I/O operations (e.g., database queries)

The configurable delay demonstrates how in realistic scenarios (with I/O), any compositional overhead becomes negligible.

## Running Benchmarks

All commands should be run from the repository root.

### Run all benchmarks
```bash
go test -bench=. -benchmem ./bench/...
```

### Run specific benchmark variants
```bash
# CPU-bound only (shows maximum overhead)
go test -bench=/cpu_bound -benchmem ./bench/...

# I/O-bound (shows realistic scenarios)
go test -bench=/io_10us -benchmem ./bench/...

# Compare flow vs traditional for full workflow
go test -bench='Full/' -benchmem ./bench/...

# Compare constructor vs static variable approaches
go test -bench='(Flow_Full|Flow_FullStatic|Traditional_Full)/cpu_bound' -benchmem ./bench/...
```

### Extended benchmark runs for stable results

**IMPORTANT:** Single benchmark runs can show high variance. For reliable measurements:

```bash
# Run for longer duration AND multiple iterations (recommended)
go test -bench='Full/cpu_bound' -benchmem -count=20 -benchtime=5s ./bench/...

# Run multiple times and use benchstat for comparison
go test -bench=. -benchmem -count=10 ./bench/... | tee bench_results.txt
```

**Why this matters:**
- Single runs can vary by 20-50% due to system noise, cache effects, and scheduling
- The documented results use `-count=20 -benchtime=5s` for stability (±1-3% variance)
- With default settings, you may see much higher/lower numbers on a single run

### Using benchstat for statistical comparison
```bash
# Install benchstat if you don't have it
go install golang.org/x/perf/cmd/benchstat@latest

# Run benchmarks multiple times for statistical significance
go test -bench='Full/cpu_bound' -benchmem -count=10 ./bench/... > results.txt

# Analyze variance and confidence intervals
benchstat results.txt
```

## Benchmark Variants

### Simple Benchmarks
- `BenchmarkFlow_Simple` - Two sequential steps
- `BenchmarkTraditional_Simple` - Traditional implementation

Tests basic sequential execution overhead.

### Parallel Benchmarks
- `BenchmarkFlow_Parallel` - Parallel service deployment
- `BenchmarkTraditional_Parallel` - Traditional goroutine orchestration

Tests overhead of parallel execution primitives.

### Full Benchmarks
- `BenchmarkFlow_Full` - Complete deployment workflow (constructor function)
- `BenchmarkFlow_FullStatic` - Complete deployment workflow (static variable)
- `BenchmarkTraditional_Full` - Traditional implementation

Tests the complete workflow with all features (conditionals, retry, parallel, etc.).

The static variable approach pre-allocates the workflow structure at package initialization, reducing per-invocation overhead by ~7.7% compared to the constructor approach.

## Interpreting Results

### What to look for:

1. **CPU-bound overhead**: In the `cpu_bound` variant, any performance difference shows the pure overhead of abstractions
2. **I/O scenarios**: As I/O delay increases, the percentage overhead should approach zero
3. **Allocations**: Memory allocation differences (B/op and allocs/op)
4. **Scalability**: How overhead changes with workflow complexity

### Example results interpretation:

**CPU-bound (worst case for overhead):**
```
BenchmarkFlow_Full/cpu_bound-16          5645 ns/op ± 1%    1568 B/op    37 allocs/op
BenchmarkFlow_FullStatic/cpu_bound-16    5210 ns/op ± 1%    1144 B/op    23 allocs/op
BenchmarkTraditional_Full/cpu_bound-16   4446 ns/op ± 3%     720 B/op    15 allocs/op
```

This shows:
- Flow (constructor): 27% slower, 37 allocations, 1568 bytes
- Flow (static): 17% slower, 23 allocations, 1144 bytes (7.7% faster than constructor!)
- Traditional: baseline, 15 allocations, 720 bytes
- **Absolute overhead:** ~764ns (less than 1 microsecond)

**I/O-bound (realistic scenario with 100µs I/O):**
```
BenchmarkFlow_Full/io_100us-16           951.6 µs/op    1856 B/op    40 allocs/op
BenchmarkTraditional_Full/io_100us-16    949.0 µs/op    1008 B/op    18 allocs/op
```

With realistic I/O:
- Flow version: <0.3% difference (within measurement variance)
- I/O time dominates, compositional overhead is insignificant
- The ~764ns overhead is 0.08% of the total execution time

## Testing Behavioral Equivalence

The benchmarks are accompanied by comprehensive tests that verify both implementations produce identical results:

```bash
# Run behavioral tests from repository root
go test -v -run TestBehaviorMatch ./bench/...
go test -v -run TestWorkflowSteps ./bench/...
go test -v -run TestRetryBehavior ./bench/...
go test -v -run TestConditionalExecution ./bench/...
```

These tests ensure:
- Both implementations execute the same steps
- Both handle errors identically
- Both produce the same final state
- Retry logic works equivalently
- Conditional execution matches

## Profiling

To see exactly where CPU time is spent:

```bash
# Generate CPU and memory profiles
go test -bench='Flow_Full/cpu_bound' -cpuprofile=cpu.prof -memprofile=mem.prof ./bench/...

# Analyze CPU profile (top functions)
go tool pprof -top cpu.prof

# Analyze specific function
go tool pprof -list='DoWith' cpu.prof

# Interactive exploration
go tool pprof cpu.prof
```

See **`ANALYSIS.md`** for detailed profiling insights and overhead analysis.

## Files in This Directory

- **`BENCHMARK.md`** (this file) - Guide for running and interpreting benchmarks
- **`ANALYSIS.md`** - Deep dive into overhead sources, profiling insights, and performance characteristics
- **`flow_bench_test.go`** - Main benchmark implementations
- **`flow_bench_behavior_test.go`** - Tests verifying behavioral equivalence
- **`flow_overhead_demo_test.go`** - Isolated benchmarks demonstrating overhead sources
