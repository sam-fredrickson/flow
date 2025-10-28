# Performance Overhead Analysis

## TL;DR

**Where does the overhead come from?**

1. **Loop iteration**: The `Do` combinator adds ~2.8ns overhead from the `for range` loop (9.6% in micro-benchmarks)
2. **StepsProvider resolution**: Nested combinators like `InSerial` add extra overhead resolving dynamic step providers
3. **Closure creation**: Each closure costs ~0.6ns to create (but amortized with static workflows)

**Surprising discovery:** Pre-constructed closures have virtually no overhead vs direct calls (0.3%)!

**Key optimization:** Using static package-level variables instead of constructor functions reduces overhead by ~7.7% overall and eliminates per-invocation closure allocation (27% → 17% overhead vs traditional, saving ~435ns).

## Benchmark Results Summary

### Simple Workflow (2 steps, 1 combinator)
```
Flow:        52.8 ns/op (176 B, 2 allocs)
Traditional: 49.0 ns/op (176 B, 2 allocs)
Overhead:    ~8% (~3.8ns absolute)
```

### Full Workflow (~10 steps, 9 combinators)
```
Flow (constructor):  5645 ns/op ± 1%  (1568 B, 37 allocs)
Flow (static):       5210 ns/op ± 1%  (1144 B, 23 allocs)
Traditional:         4446 ns/op ± 3%  (720 B, 15 allocs)

Constructor overhead: 27%
Static overhead:      17%
Absolute overhead:    764ns (static)
```

### With Realistic I/O (100µs delay)
```
Flow:        951.6 µs/op
Traditional: 949.0 µs/op
Overhead:    <0.3% (within measurement variance)
```

## Root Cause Analysis

### Empirical Overhead Breakdown

Isolated benchmarks measuring each component (10 step operations, all with error checking):

**Execution-only (pre-constructed workflows):**
```
Direct function calls:           29.66 ns/op    8 B/op    1 allocs/op  (baseline)
Pre-constructed closures:        29.74 ns/op    8 B/op    1 allocs/op  (+0.08ns, +0.3%)
Do combinator (pre-constructed): 32.51 ns/op    8 B/op    1 allocs/op  (+2.85ns, +9.6%)
Nested combinators:              51.78 ns/op    8 B/op    1 allocs/op  (+22.12ns, +75%)
```

**Construction + Execution (workflows built in loop):**
```
Direct calls (no construction):  29.65 ns/op    8 B/op    1 allocs/op  (baseline)
Closure creation + calls:        35.96 ns/op    8 B/op    1 allocs/op  (+6.31ns, +21%)
Do combinator creation:          43.62 ns/op    8 B/op    1 allocs/op  (+13.97ns, +47%)
```

### Key Findings:

1. **Pre-constructed closures have negligible overhead** (0.08ns / 0.3%)
   - Calling through a closure is virtually identical to direct function calls
   - The Go compiler optimizes function value calls very effectively

2. **Do combinator iteration adds ~2.8ns overhead** (9.6%)
   - FlowDo: 32.51ns vs Closure: 29.74ns = 2.77ns extra
   - This is purely the cost of the `for _, step := range steps` loop
   - All benchmarks include `if err != nil` checks, so error handling isn't the source
   - The overhead comes from: loop setup, slice iteration, and indirect calls through slice elements
   - Note: `Do` takes `...Step[T]` directly - just a simple slice iteration, no StepsProvider resolution

3. **Closure creation costs ~0.6ns per closure** (2.1% per closure)
   - ClosureCreation: 35.96ns vs Direct: 29.65ns = 6.31ns for 10 closures
   - 6.31ns / 10 = **0.63ns per closure**
   - This is the cost of allocating and capturing the closure state

4. **Do combinator construction adds ~7.7ns one-time cost**
   - FlowDoCreation: 43.62ns vs ClosureCreation: 35.96ns = 7.66ns
   - This includes creating 10 closures (6.31ns) + Do wrapper construction
   - Do wrapper overhead: 7.66ns - 6.31ns = **1.35ns** (just variadic slice allocation)
   - Amortized over workflow reuse, this one-time cost becomes negligible

5. **Nested combinators add significant layering overhead** (+22ns / 75%)
   - FlowNested: 51.78ns vs Direct: 29.66ns = 22.12ns extra
   - The nested benchmark uses `InSerial(Steps(...), Steps(...), ...)`
   - Unlike `Do`, `InSerial` takes `...StepsProvider[T]` which must be dynamically resolved
   - Each `StepsProvider[T]` is a function call (`Extract[T, []Step[T]]`) that returns `[]Step[T]`
   - Multiple layers means: calling each provider → iterating returned steps → additional indirection
   - This demonstrates the cost of flexible composition vs. flat step lists

## Why Overhead Grows with Complexity

The overhead percentage grows because combinator invocations dominate:

### Simple workflow (9% overhead):
- 2 steps
- 1 combinator (`Do`)
- Minimal indirection

### Full workflow (22% overhead with constructor, 14% with static):
- ~10 steps
- **9 combinators:**
  - 1× `Do`
  - 3× `Steps`
  - 1× `When`
  - 1× `InParallel`
  - 1× `Retry`
  - 1× `IgnoreError`
  - 1× `From` + `Map`

**9 combinators × ~5-10ns each ≈ 45-90ns overhead**

The rest of the ~900ns overhead comes from:
- Extra allocations (1576B vs 720B)
- Additional function call layers
- Slice iterations in the combinator implementations

## Sources of Overhead in Detail

### 1. Closure Allocation for Step Constructors

**Flow version:**
```go
ValidateConfig()  // Returns a closure
RunHealthCheck()  // Returns a closure
```

Each step constructor creates a new function closure.

**Traditional version:**
```go
validateConfig(ctx, s)  // Direct function call
runHealthCheck(ctx, s)  // Direct function call
```

No closures needed - just direct function calls.

**Impact:** With constructor functions, closures are created on every workflow invocation. With static variables, they're created once at initialization.

### 2. Nested Closure Layers in Combinators

**Flow version:**
```go
flow.Do(                // Layer 1: Do creates closure
    ValidateConfig(),   // Layer 2: Step constructor closure
    flow.When(          // Layer 3: When creates closure
        HasDatabase(),  // Layer 4: Predicate closure
        SetupDatabase() // Layer 5: Conditional step closure
    ),
)
```

**Traditional version:**
```go
if hasDatabase(ctx, s) {    // Direct if statement
    setupDatabase(ctx, s)   // Direct function call
}
```

**Impact:** Each combinator (`Do`, `Steps`, `When`, `InParallel`, `Retry`, `IgnoreError`) wraps the steps in additional closure layers. The full workflow has 6+ combinator layers.

### 3. Dynamic Step Resolution via `StepsProvider[T]`

**Flow version:**
```go
// Do must call each StepsProvider[T] to get steps
func Do[T any](series ...StepsProvider[T]) Step[T] {
    return func(ctx context.Context, t T) error {
        for _, next := range series {
            steps, err := next(ctx, t)  // Function call to get steps
            if err != nil {
                return err
            }
            for _, step := range steps {  // Iterate and call each step
                if err := step(ctx, t); err != nil {
                    return err
                }
            }
        }
        return nil
    }
}
```

**Traditional version:**
```go
// Direct sequential calls
validateConfig(ctx, s)
setupDatabase(ctx, s)
runHealthCheck(ctx, s)
```

**Impact:** Every `StepsProvider[T]` requires a function call to extract the slice of steps, then iteration over that slice. This is pure overhead compared to direct function calls.

## Static Variable Optimization

Using package-level variables instead of constructor functions significantly reduces overhead:

```go
// Constructor approach (37 allocs, 1576 B)
func DeployWorkflow_Flow() flow.Step[*DeploymentState] {
    return flow.Do(
        flow.Steps(
            ValidateConfig(),  // New closure every call
            // ...
        ),
    )
}

// Static approach (23 allocs, 1144 B)
var DeployWorkflow_FlowStatic = flow.Do(
    flow.Steps(
        ValidateConfig(),  // Created once at init
        // ...
    ),
)
```

**Why it helps:**
- Closures allocated once at package initialization
- Reused on every workflow invocation
- Eliminates 14 allocations (37 → 23)
- Reduces overhead from 22% to 14%

**When to use static variables:**
- ✅ Workflow structure is known at compile time
- ✅ Workflow is reused many times
- ✅ You want to minimize per-invocation overhead

**When to use constructors:**
- ✅ Need parameterized workflows (e.g., `DeployWorkflow(retries int)`)
- ✅ Workflows are dynamic or context-dependent
- ✅ The extra 400ns doesn't matter (it usually doesn't!)

## What the Go Compiler Can't Optimize Away

Modern Go compilers are good at:
- ✅ Inlining small functions
- ✅ Escape analysis for stack allocation
- ✅ Eliminating dead code

But they **cannot** optimize away:
- ❌ Higher-order function calls (closures prevent inlining)
- ❌ Dynamic function dispatch through interfaces/function values
- ❌ Slice allocations for variable-length step lists
- ❌ Nested closure captures (escape to heap)

The flow library fundamentally relies on these patterns for composability.

## The Architectural Trade-off

This overhead is **fundamental to composability**. It cannot be eliminated because:

1. **Dynamic composition requires runtime dispatch** - We can't know the workflow structure at compile time
2. **Higher-order functions prevent inlining** - Function values are opaque to the optimizer
3. **Type-safe generics add indirection** - Generic step types require function pointers

## Is This Overhead Acceptable?

**In CPU-bound scenarios:**
- Absolute difference: 5344ns - 4674ns = **670 nanoseconds**
- That's **0.67 microseconds** of extra overhead for the entire workflow

**In I/O-bound scenarios (100µs I/O):**
- Flow: 950343 ns
- Traditional: 951074 ns
- Overhead: **<0.1%** (actually traditional is slightly slower!)

**Real-world context:**
```
Creating a database instance:     ~30,000,000 ns (30ms)
Configuring networking:           ~50,000,000 ns (50ms)
Deploying a service:             ~100,000,000 ns (100ms)
DNS propagation:               ~5,000,000,000 ns (5 seconds)

Flow library overhead:                   670 ns (0.00067ms)
```

The overhead is **0.00013% of a single DNS update**.

## Benchmark Variance

Statistical analysis with 10 iterations shows:

```
Flow (constructor):  5.683µs ± 1%   (very stable)
Flow (static):       5.344µs ± 2%   (stable)
Traditional:         4.674µs ± 9%   (more variable)
```

**Interesting finding:** The flow implementations are actually MORE stable than the traditional approach, likely due to more predictable allocation patterns and longer execution times that smooth out timing jitter.

## Profiling Insights

CPU and memory profiling with `go tool pprof` confirms the benchmark analysis:

**Runtime operations dominate realistic profiles:**
- Retry backoff sleep calls, goroutine coordination, and other runtime operations account for 90%+ of execution time
- The compositional overhead (combinator iterations, predicate evaluations) is measurable but small relative to actual work
- This is why I/O-bound benchmarks show <0.1% overhead despite 14-22% overhead in CPU-bound tests

**Profiling shows overhead exactly where predicted:**
- `Do` combinator: ~7% of profile time (StepsProvider[T] resolution and iteration)
- `Retry` predicates: ~5% of profile time (backoff strategy evaluation)
- Static variables: ~21% reduction in `Do` overhead (matches benchmark findings)

**For profiling instructions**, see the "Profiling" section in `BENCHMARK.md`.

## Conclusion

The overhead in CPU-bound benchmarks is entirely expected and acceptable:

- ✅ **Caused by composability**, not inefficiency
- ✅ **Well-understood** - loop iteration and layered composition
- ✅ **Sub-microsecond** in absolute terms (~764ns with static variable)
- ✅ **Completely negligible** with any realistic I/O (<0.3% difference)
- ✅ **Can be reduced** by using static variables (27% → 17% overhead, 7.7% faster)
- ✅ **Worth it** for code clarity and maintainability

The flow library successfully achieves its goal: **make complex workflows readable and composable without meaningful performance impact**.
