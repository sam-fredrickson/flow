# Data Pipeline Example

This example demonstrates the **collection operators** (`Collect`, `Render`, `Apply`) for building clean, composable data processing pipelines, and their **streaming counterparts** (`Stream`, `Expand`, `DrainParallel`) for processing items one at a time without holding the whole collection in memory.

## What This Example Shows

Three common patterns in data processing:

1. **Complete ETL Pipeline** - Pull data from a paginated API, transform it through multiple stages, and save
   - **1b. Streaming ETL Pipeline** - The same pipeline, streamed one page at a time and saved by a pool of workers
2. **Queue Processing** - Drain a queue by pulling items until exhausted
3. **Batch Transformation** - Transform and save a batch of cached records

All three use the collection operators for clean, functional composition without orchestrator wrappers.

## The Collection Operators

### `Collect` - Repeated Extraction

```go
func Collect[T, U any](f Extract[T, U]) Extract[T, []U]
```

Repeatedly calls an `Extract` function until it returns `flow.ErrExhausted`, collecting all results into a slice.

**Use for:** Iterator patterns, paginated APIs, queue draining, streaming data sources

**Example:**
```go
flow.Pipeline(
    flow.Collect(FetchNextPage),  // Pull all pages until exhausted
    flow.Render(ProcessPage),     // Process each page
    flow.Apply(SavePage),         // Save each page
)
```

### `Render` - Transform Each Element

```go
func Render[T, In, Out any](f Transform[T, In, Out]) Transform[T, []In, []Out]
```

Applies a `Transform` to each element in a slice, producing a new slice (serial, fail-fast).

**Use for:** Element-wise transformations, validation, enrichment, parsing

**Example:**
```go
flow.Pipeline(
    GetRawRecords,                // Extract[*State, []RawRecord]
    flow.Render(ValidateRecord),  // []RawRecord → []ValidatedRecord
    flow.Render(EnrichRecord),    // []ValidatedRecord → []EnrichedRecord
    flow.Apply(SaveRecord),       // Save each enriched record
)
```

### `Apply` - Consume Each Element

```go
func Apply[T, U any](f Consume[T, U]) Consume[T, []U]
```

Applies a `Consume` to each element in a slice (serial, fail-fast).

**Use for:** Saving, logging, side effects, post-processing

**Example:**
```go
flow.With(
    GetPendingOrders,      // Extract[*State, []Order]
    flow.Apply(ProcessOrder), // Process each order
)
```

## Streaming Counterparts

`Collect` pulls every page before anything downstream runs. The streaming combinators process items as they arrive instead:

```go
flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
    Via(ValidateRecord).
    Via(EnrichRecord).
    DrainParallel(SaveRecord, flow.ParallelOptions{Limit: 4})
```

- `Stream` marks `FetchNextPage` as a `Source`: an `Extract` that returns one item per call until `ErrExhausted`.
- `Source.Via` transforms each item as it is pulled.
- `Expand` turns a source of batches (pages of records) into a source of records, buffering one page at a time. It is the streaming counterpart of `Collect` + `Flatten`.
- `DrainParallel` saves records with a pool of 4 workers. `Drain` is the serial version.

Because `SaveRecord` runs on several workers at once, the state it touches must be thread-safe; here `recordsSaved` is updated atomically. See the [drain-parallel example](../drain-parallel/) for how `ParallelOptions.Prefetch` keeps those workers busy while the next page is fetched.

## Running the Example

```bash
cd examples/data-pipeline
go run example.go
```

Expected output:
```
=== Data Pipeline Examples ===

Example 1: Complete ETL Pipeline (Collect → Render → Apply)
------------------------------------------------------------
📥 Fetched page 1 (10 records)
📥 Fetched page 2 (10 records)
📥 Fetched page 3 (10 records)
💾 Saved record rec-001: Record 1 (0.63, medium)
💾 Saved record rec-002: Record 2 (0.76, medium)
...
✅ Pipeline completed: 30 records saved

Example 1b: Streaming ETL Pipeline (Stream → Expand → DrainParallel)
------------------------------------------------------------
📥 Fetched page 1 (10 records)
💾 Saved record rec-004: Record 4 (1.02, high)
💾 Saved record rec-003: Record 3 (0.89, medium)
...
📥 Fetched page 2 (10 records)
...
✅ Streaming pipeline completed: 30 records saved

Example 2: Queue Processing (Collect → Apply)
------------------------------------------------------------
⚙️  Processing queue item: queue-item-1
⚙️  Processing queue item: queue-item-2
...
✅ Queue processing completed: 5 items processed

Example 3: Batch Transform (Extract → Render → Apply)
------------------------------------------------------------
💾 Saved record rec-001: Record 1 (0.63, medium)
...
✅ Batch transform completed: 10 records saved
```

## Key Concepts

### Serial Execution with Fail-Fast

All three operators execute serially and fail fast:
- Process elements one at a time in order
- Stop immediately on first error
- Check for context cancellation between elements

For parallel execution, use `Map` + `InParallel` instead (see [docs/patterns.md](../../docs/patterns.md#collection-processing-patterns)).

### Functional Composition

Collection operators return `Extract`, `Transform`, or `Consume`, making them fully composable:

```go
// Compose with From
flow.From(
    GetUserIDs,              // Extract[*State, []int]
    flow.Render(LoadUser),   // Transform[*State, []int, []User]
)                            // → Extract[*State, []User]

// Compose with Chain
flow.Chain(
    flow.Render(Parse),      // Transform[*State, []string, []Data]
    flow.Render(Validate),   // Transform[*State, []Data, []ValidData]
)

// Compose with Feed
flow.Feed(
    flow.Render(Transform),  // Transform[*State, []In, []Out]
    flow.Apply(Save),        // Consume[*State, []Out]
)
```

### The `ErrExhausted` Sentinel

`Collect` uses `flow.ErrExhausted` to signal completion:

```go
func FetchNextPage(ctx context.Context, state *State) (Page, error) {
    if state.hasMorePages == false {
        return Page{}, flow.ErrExhausted  // Signal completion
    }
    // ... fetch and return next page
}
```

This enables clean iterator patterns without special collection types or boolean flags.

### When to Use Collection Operators vs. Map

| Use Case | Pattern | Why |
|----------|---------|-----|
| Need parallel execution | `Map` + `InParallel` | Can run elements concurrently |
| Serial-only processing | `Collect`/`Render`/`Apply` | Cleaner composition, no orchestrator needed |
| Might parallelize later | `Map` + `InSerial` | Easy to switch to `InParallel` |
| Functional pipeline | `Collect`/`Render`/`Apply` | Composes with `From`, `Chain`, `Feed`, `Pipeline` |

See the [Collection Processing](../../docs/guide.md#collection-processing) section of the guide for the complete decision guide.

## Code Structure

```
example.go
├─ Example 1: Complete ETL Pipeline
│  └─ Collect → Render → Render → Apply
│
├─ Example 1b: Streaming ETL Pipeline
│  └─ Stream → Expand → Via → Via → DrainParallel
│
├─ Example 2: Queue Processing
│  └─ Collect → Apply
│
├─ Example 3: Batch Transform
│  └─ Extract → Render → Render → Apply
│
└─ Example 4: Error Handling
   └─ Demonstrates fail-fast behavior
```

## Learn More

- **[Feature Guide](../../docs/guide.md)** - Complete documentation of all features including collection operators
- **[Design Philosophy](../../docs/design.md)** - Understanding the principles behind the library
