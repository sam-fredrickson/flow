# Ideas for Future Exploration

This document captures potential enhancements and design considerations for the `flow` library. These are not commitments—just ideas to revisit as real-world usage reveals pain points.

## Potential Additions

### Method Adapter for Common Go Patterns

Go methods often have the signature `func (t T) Method(ctx context.Context) error`, but `Step[T]` expects `func(context.Context, T) error` (flipped parameter order). A helper could reduce boilerplate:

```go
func AdaptMethod[T any](method func(T, context.Context) error) Step[T] {
    return func(ctx context.Context, t T) error {
        return method(t, ctx)
    }
}

// Usage
type Service struct { /* ... */ }
func (s *Service) Start(ctx context.Context) error { /* ... */ }

flow.Do(
    flow.AdaptMethod((*Service).Start),
)
```

**Status:** Speculative. The wrapper is trivial enough that it may not justify API surface. Add only if real usage shows this pattern is common.

### Additional Looping Constructs

Currently have `While`. Could add:
- `Until(predicate, step)` - Loop until predicate becomes true (equivalent to `While(Not(predicate), step)` but clearer intent)
- `Repeat(n, step)` - Execute step exactly N times
- `Forever(step)` - Infinite loop (must be cancelled via context)

**Status:** Wait for real use cases. `While` combined with `Not` may be sufficient.

### FirstSuccess - Race Pattern

Run steps in parallel until one succeeds, cancel the rest. Useful for trying multiple backends, fallback strategies, or racing redundant requests.

**Status:** Purely speculative. No real use case identified yet. Will revisit if/when needed.

**Design consideration:** Should this be a separate `FirstSuccess()` function, or a flag in `ParallelOptions` (e.g., `FirstSuccess: true` or `Race: true`)? The latter would allow natural composition with `Limit` and other parallel execution options.

**Usage (as separate function):**
```go
flow.FirstSuccess(
    tryPrimary,
    trySecondary,
    tryTertiary,
)
```

**Usage (as ParallelOptions flag):**
```go
flow.InParallelWith(
    ParallelOptions{FirstSuccess: true, Limit: 3},
    Steps(tryPrimary, trySecondary, tryTertiary),
)
```

### Collection Operator Extensions

The collection operators (`Collect`, `Render`, `Apply`) implemented in the collection algebra proposal complete the basic type algebra. Several extensions have been identified for future consideration:

#### 1. Error-Collecting Variants

Current `Render` and `Apply` fail fast. Error-collecting variants would continue processing and join all errors:

```go
func RenderAll[T, In, Out any](f Transform[T, In, Out]) Transform[T, []In, []Out]  // errors.Join
func ApplyAll[T, U any](f Consume[T, U]) Consume[T, []U]                            // errors.Join
```

**Use case:** Validation or cleanup operations where you want to see all failures, not just the first.

**Example:**
```go
// Fail-fast (current): stops at first validation error
flow.With(GetRecords, flow.Apply(ValidateRecord))

// Collect-all-errors: validates all records, returns joined errors
flow.With(GetRecords, flow.ApplyAll(ValidateRecord))
```

**Status:** Wait for user demand. Current fail-fast behavior is usually preferred.

#### 2. Skip Sentinel Error

Add a `Skip` sentinel error that `Collect` recognizes to skip items without stopping collection:

```go
var Skip = errors.New("skip item")

func Collect[T, U any](f Extract[T, U]) Extract[T, []U] {
    // ... in the loop:
    item, err := f(ctx, state)
    if errors.Is(err, Skip) {
        continue  // Skip this item, keep collecting
    }
    if errors.Is(err, ErrExhausted) {
        return collected, nil
    }
    // ...
}
```

**Use case:** Filtering during collection without separate filter step.

**Example:**
```go
func NextEvenNumber(ctx context.Context, state *State) (int, error) {
    if state.counter >= 10 {
        return 0, flow.ErrExhausted
    }
    state.counter++
    if state.counter % 2 != 0 {
        return 0, flow.Skip  // Skip odd numbers
    }
    return state.counter, nil
}

// Collects only even numbers: [2, 4, 6, 8, 10]
flow.With(flow.Collect(NextEvenNumber), flow.Apply(Process))
```

**Status:** Speculative. Can be worked around with `Render` + filtering logic, or by having the Extract function maintain skip logic internally.

#### 3. Parallel Collection Variants

Provide parallel versions of `Render` and `Apply`:

```go
func RenderInParallel[T, In, Out any](f Transform[T, In, Out]) Transform[T, []In, []Out]
func ApplyInParallel[T, U any](f Consume[T, U]) Consume[T, []U]
```

**Use case:** When you want parallel execution but still need the result as `Transform` or `Consume` (not `StepsProvider`).

**Status:** Wait for user demand. Current approach is to use `Map` + `InParallel`, which is explicit about parallel execution intent. Parallel variants of `Render`/`Apply` might hide parallelism too much.
