# Flow Guide

A comprehensive guide to building type-safe workflows with `flow`.

## Table of Contents

1. [Getting Started](#getting-started)
2. [When to Use Flow](#when-to-use-flow)
3. [Essential Features](#essential-features)
4. [Functional Composition](#functional-composition)
5. [Collection Processing](#collection-processing)
6. [Workflow Patterns](#workflow-patterns)
7. [Advanced Topics](#advanced-topics)
   - [State Transformation with Spawn](#state-transformation-with-spawn)
   - [Logging](#logging)
   - [Workflow-Scoped Values](#workflow-scoped-values)
   - [Resource Cleanup](#resource-cleanup)
   - [Debugging](#debugging)
   - [Mega-Wrappers: Factoring Out Common Patterns](#mega-wrappers-factoring-out-common-patterns)
   - [Thread Safety in Parallel Execution](#thread-safety-in-parallel-execution)
   - [Common Pitfalls](#common-pitfalls)

---

## Getting Started

### Core Concepts

The `flow` library provides three key abstractions:

- **`Step[T]`** - A function that operates on state `T`: `func(context.Context, T) error`
- **Composition** - Combine steps using `Do`, `InParallel`, `Retry`, `When`, etc.
- **Type safety** - Steps must operate on the same state type

Composition comes in two interchangeable styles:

- **Function style**: package-level combinators like `flow.Do(a, b)`,
  `flow.Retry(step)`, `flow.Pipeline(extract, transform, consume)`
- **Fluent style**: the same combinators as methods, reading left-to-right:
  `a.Then(b)`, `step.Retry()`, `extract.Via(transform).To(consume)`

Both produce identical results — every method delegates to its package-level
counterpart. The conventions used throughout this guide:

- `flow.Do(...)` for sequences of three or more steps; `.Then` for a step
  with one follow-up
- Trailing methods for decorators: `.Retry(...)`, `.When(...)`,
  `.IgnoreError()`, `.Named("x")`, `.Scoped()`
- `.Via`/`.Then`/`.To` for data pipelines, where the fluent style shines
  most (see [Functional Composition](#functional-composition))

### Step Constructors: The Recommended Pattern

Use step constructors by default, even for steps without parameters:

```go
func ValidateConfig() flow.Step[*Config] {
    return func(ctx context.Context, cfg *Config) error { ... }
}

func StartServer() flow.Step[*Config] {
    return func(ctx context.Context, cfg *Config) error { ... }
}

flow.Do(
    ValidateConfig(),
    StartServer(),
)
```

This gives you a place to apply decorators like `Retry`, `Named`/`AutoNamed`, or `IgnoreError`:

```go
func CreateDatabase(name string) flow.Step[*Config] {
    return flow.Retry(func(ctx context.Context, cfg *Config) error {
        // CREATE DATABASE logic
        return nil
    })
}
```

---

## When to Use Flow

### Flow is most valuable when you have:

- **Multiple steps that need coordination** (parallel, sequential, conditional)
- **Retries with backoff** that you don't want to reimplement correctly
- **Dynamic workflows** where steps are determined at runtime
- **Complex orchestration** that's obscuring business logic
- **Configuration-driven execution** where components declare their needs

### Flow is probably overkill for:

- **Simple sequential scripts** (3-5 operations with no branching or error handling needs)
- **One-off automation** where you'll never modify or extend the workflow
- **Code where "how it happens" is the point** (e.g., teaching concurrency patterns)

### Incremental Adoption

Since `Step[T]` is a function type (`func(context.Context, T) error`) — any function or closure with that signature is directly assignable to it — there's no lock-in or runtime magic:

- **Use it for specific problems**: Add `flow.Retry()` around one flaky API call without changing anything else
- **Mix with existing code**: Flow steps are just functions—call them from anywhere, call anything from them
- **No all-or-nothing**: Use the parallelism helpers without rewriting your sequential code
- **No special runtime**: No global state, no background goroutines, no reflection, just functions

This is a toolkit, not a framework. Use as much or as little as makes your code clearer.

---

## Essential Features

### Sequential Execution

`Do()` runs steps one at a time, handling errors consistently. By default, it stops at the first error:

```go
flow.Do(
    Step1(), Step2(), Step3(),
)
```

To collect all errors instead:

```go
flow.DoWith(
    flow.Options{JoinErrors: true},
    Step1(), Step2(), Step3(),
)
```

### Parallel Execution

`InParallel()` runs steps concurrently, handling all the synchronization and error collection:

```go
flow.InParallel(
    flow.Steps(
        FetchUserData(),
        FetchPreferences(),
        FetchNotifications(),
    ),
)
```

Control concurrency limits and error handling:

```go
flow.InParallelWith(
    flow.ParallelOptions{
        Limit:      5,        // Max 5 concurrent goroutines
        JoinErrors: true,     // Collect all errors
    },
    flow.Steps(
        ProcessItem1(),
        ProcessItem2(),
        ProcessItem3(),
    ),
)
```

**Thread Safety:** When using parallel execution, ensure your state type `T` is thread-safe. See [Thread Safety](#thread-safety-in-parallel-execution) for details.

### Conditional Execution

`When` and `Unless` make conditional logic explicit and composable:

```go
flow.Do(
    ValidateConfig(),
    EnableMonitoring().When(IsProduction()),
    RequireAuthentication().Unless(IsDevelopment()),
    StartServer(),
)
```

The method forms `step.When(pred)` and `step.Unless(pred)` read "do this,
under this condition"; the function forms `flow.When(pred, step)` put the
condition first. Use whichever reads better in context. Predicates also
compose with `.And`, `.Or`, and `.Not`:

```go
EnableMonitoring().When(IsProduction().And(IsHealthy()))
```

Predicates can fail and have access to your state:

```go
func IsProduction() flow.Predicate[*Config] {
    return func(ctx context.Context, cfg *Config) (bool, error) {
        env, err := fetchEnvironment(ctx)
        if err != nil {
            return false, err
        }
        return env == "production", nil
    }
}
```

### Retry Logic

`Retry` lets you declaratively specify retry policies with composable predicates:

```go
flow.Retry(
    CallExternalAPI(),
    flow.OnlyIf(isTransientError),
    flow.UpTo(5),
    flow.ExponentialBackoff(100 * time.Millisecond),
)
```

Built-in retry predicates:
- `UpTo(n)` limits retry attempts
- `FixedBackoff(d)` waits a fixed duration between retries
- `ExponentialBackoff(base)` increases delays exponentially
- `OnlyIf(check)` retries only for certain errors

Default behavior (3 retries with exponential backoff):

```go
flow.Retry(CallExternalAPI())
```

Or as a trailing method on the step:

```go
CallExternalAPI().Retry(
    flow.OnlyIf(isTransientError),
    flow.UpTo(5),
)
```

### Error Handling

The library provides composable error handling patterns:

**Ignore errors for best-effort operations:**

```go
flow.IgnoreError(SendMetrics())
// or
SendMetrics().IgnoreError()
```

**Recover from panics:**

```go
flow.RecoverPanics(
    CallUnsafeCode(),
)
```

**Custom error handling with fallbacks:**

```go
flow.OnError(
    CallPrimaryAPI(),
    func(ctx context.Context, state *State, err error) (flow.Step[*State], error) {
        if errors.Is(err, ErrTimeout) {
            return CallBackupAPI(), nil
        }
        return nil, fmt.Errorf("unrecoverable: %w", err)
    },
)
```

Or use the simpler `FallbackTo` helper:

```go
CallPrimaryAPI().OnError(flow.FallbackTo(CallBackupAPI()))
```

---

## Functional Composition

Sometimes you need to pass data between steps rather than storing everything in shared state. The library provides three types for building functional pipelines:

```go
type Extract[T, U any] func(context.Context, T) (U, error)
type Transform[T, In, Out any] func(context.Context, T, In) (Out, error)
type Consume[T, U any] func(context.Context, T, U) error
```

- **Extract[T, U]** reads a value of type `U` from state `T`
- **Transform[T, In, Out]** converts an `In` value to an `Out` value using state `T`
- **Consume[T, U]** processes a value `U` for side effects using state `T`

### Composition Patterns

These compose together to form complete pipelines:

**From: Extract + Transform → Extract**
```go
extract := flow.From(
    getUserID,              // Extract[*State, int]
    formatUserID,           // Transform[*State, int, string]
)                           // → Extract[*State, string]
```

**Feed: Transform + Consume → Consume**
```go
consume := flow.Feed(
    validateData,           // Transform[*State, Data, CleanData]
    saveToDatabase,         // Consume[*State, CleanData]
)                           // → Consume[*State, Data]
```

**Pipeline: Extract + Transform + Consume → Step**
```go
step := flow.Pipeline(
    loadConfig,             // Extract[*State, Config]
    validateConfig,         // Transform[*State, Config, ValidConfig]
    applyConfig,            // Consume[*State, ValidConfig]
)                           // → Step[*State]
```

**With: Extract + Consume → Step**
```go
step := flow.With(
    getCurrentUser,         // Extract[*State, User]
    logUserAction,          // Consume[*State, User]
)                           // → Step[*State]
```

**Chain: Transform + Transform → Transform**
```go
transform := flow.Chain(
    parseJSON,              // Transform[*State, string, Data]
    enrichData,             // Transform[*State, Data, EnrichedData]
)                           // → Transform[*State, string, EnrichedData]
```

For longer transformation chains, use the `Then` method, which chains to any
length: `parseJSON.Then(enrichData).Then(normalize)`.

### Fluent Pipelines

Each composition pattern above has a method form, and this is where the
fluent style pays off most: pipelines read left-to-right in data-flow order
instead of inside-out.

| Function form | Method form |
|---------------|-------------|
| `From(e, t)` | `e.Via(t)` |
| `Chain(t1, t2)` | `t1.Then(t2)` |
| `Feed(t, c)` | `t.To(c)` |
| `With(e, c)` | `e.To(c)` |
| `Pipeline(e, t, c)` | `e.Via(t).To(c)` |
| `Spawn(e, s)` | `e.Spawn(s)` |

The methods that change the value type (`Via`, `Then`) are generic methods,
so multi-stage pipelines stay fully type-inferred:

```go
// Function style: reads inside-out
flow.Pipeline(
    GetRawConfig,
    flow.Chain(Parse, Validate),
    Save,
)

// Fluent style: reads in data-flow order
GetRawConfig.Via(Parse).Via(Validate).To(Save)
```

**Starting a chain**: values returned by flow combinators and step
constructors already have the defined types, so methods chain directly. A
plain declared function (`func GetRawConfig(ctx, *State) (Config, error)`)
has no method set, so to *start* a chain from one, convert it first:

```go
flow.Extract[*State, Config](GetRawConfig).Via(Parse).To(Save)
```

In argument position no conversion is needed — plain functions are
assignable wherever an `Extract`/`Transform`/`Consume` parameter is
expected. In practice, chains almost always start from a combinator result
(`flow.Collect(...)`, a constructor call), so the conversion rarely comes up.

---

## Collection Processing

When processing collections of items, you have two approaches depending on whether you need parallel execution flexibility.

### Serial Collection Operators

Use `Collect`, `Render`, and `Apply` when execution will be serial and you want clean functional composition.

#### Collect - Repeated Extraction

`Collect` repeatedly calls an `Extract` until it returns `flow.ErrExhausted`, collecting all results:

```go
func Collect[T, U any](f Extract[T, U]) Extract[T, []U]
```

Example - iterator pattern:

```go
func NextQueueItem(ctx context.Context, state *State) (Item, error) {
    if state.queue.IsEmpty() {
        return Item{}, flow.ErrExhausted
    }
    return state.queue.Pop(), nil
}

flow.Pipeline(
    Collect(NextQueueItem),     // Extract[*State, []Item]
    Render(TransformItem),      // Process each item
    Apply(SaveItem),            // Save each item
)
```

#### Render - Transform Each Element

`Render` applies a `Transform` to each element in a slice, producing a new slice:

```go
func Render[T, In, Out any](f Transform[T, In, Out]) Transform[T, []In, []Out]
```

Example - multi-stage transformation, in both styles:

```go
flow.Pipeline(
    GetRawRecords,              // Extract[*State, [][]byte]
    flow.Chain(
        Render(ParseJSON),      // [][]byte → []Record
        Render(ValidateRecord), // []Record → []Record
        Render(EnrichRecord),   // []Record → []EnrichedRecord
    ),
    Apply(SaveRecord),          // Save each enriched record
)

// Fluent equivalent (GetRawRecords must have the Extract type,
// e.g. a combinator result or converted plain function):
GetRawRecords.
    Via(Render(ParseJSON)).
    Via(Render(ValidateRecord)).
    Via(Render(EnrichRecord)).
    To(Apply(SaveRecord))
```

Note that `Render` and `Apply` themselves stay package-level functions: a
method whose signature mentions its receiver type at derived type arguments
(`Transform[T, []In, []Out]` from within `Transform[T, In, Out]`) would
trigger an infinite instantiation cycle, which the compiler rejects.

#### Apply - Consume Each Element

`Apply` applies a `Consume` to each element in a slice:

```go
func Apply[T, U any](f Consume[T, U]) Consume[T, []U]
```

Example - simple serial iteration:

```go
flow.With(
    GetPendingOrders,           // Extract[*State, []Order]
    Apply(ProcessOrder),        // Consume[*State, Order]
)
```

### Parallel-Capable Collection Processing

Use `ForEach` with `InSerial` or `InParallel` when you want flexibility to choose execution strategy.

#### The From + ForEach Pattern

The most common dynamic pattern is: fetch a list at runtime, create a step for each item.

```go
func GetServices(ctx context.Context, cfg *Config) ([]string, error) {
    return cfg.Services, nil
}

// Step constructor: takes a parameter and returns a Step
func DeployService(name string) flow.Step[*Config] {
    return func(ctx context.Context, cfg *Config) error {
        return deployService(ctx, cfg, name)
    }
}

// Deploy each service sequentially
flow.InSerial(
    flow.ForEach(
        GetServices,
        DeployService,
    ),
)

// Deploy each service in parallel
flow.InParallel(
    flow.ForEach(
        GetServices,
        DeployService,
    ),
)
```

**Note:** `DeployService` is a *step constructor*—it takes a parameter and returns a `Step[*Config]`. This is the pattern to use when parameterizing steps.

### Streaming Collection Processing

`Collect` pulls every item before anything downstream runs, so the whole
collection sits in memory at once. When a collection is large, unbounded, or
arrives in pages, stream it instead: pull items one at a time and process
each as it arrives.

#### Sources

A `Source[T, U]` is an `Extract[T, U]` that follows the same iterator
protocol `Collect` uses: each call returns the next item, and
`flow.ErrExhausted` signals the end. `Stream` marks an extract as a source:

```go
func Stream[T, U any](next Extract[T, U]) Source[T, U]
```

`Source.Via` transforms each item as it is pulled. `Expand` turns a source of
batches, such as pages, into a source of items, buffering at most one batch
at a time. It is the streaming counterpart of `Collect` + `Flatten`:

```go
func Expand[T, U any](next Source[T, []U]) Source[T, U]
```

#### Drain and DrainParallel

`Drain` pulls every item from a source and consumes it, serially. It is the
streaming counterpart of `Collect` + `Apply`. `DrainParallel` does the same
with a pool of workers:

```go
// One record at a time
flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
    Via(ValidateRecord).
    Drain(SaveRecord)

// Up to 8 records saved at once
flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
    Via(ValidateRecord).
    DrainParallel(SaveRecord, flow.ParallelOptions{Limit: 8})
```

Both are also package-level functions: `flow.Drain(source, consume)` and
`flow.DrainParallel(source, consume, opts)`. Per-item errors are wrapped in
`IndexedError`, indexed by the order items were pulled.

`DrainParallel` fails fast by default. With `JoinErrors`, consumer errors are
collected and the pool keeps going, but an error from the source still stops
pulling, since the source is presumed broken. Beyond that:

- **Pulls are serialized.** Only one pull runs at a time, so a source needs
  no locking. That includes `Expand`, which keeps its current batch in a
  closure. Consumers do run concurrently; see
  [Thread Safety in Parallel Execution](#thread-safety-in-parallel-execution).
- **`Limit <= 0` means `runtime.GOMAXPROCS` workers**, not unlimited, since a
  pull-based loop needs some bound.
- **Consumption order is unspecified.** Items go to whichever worker is free.

#### Prefetching

By default, `DrainParallel` pulls an item only when a worker is ready for it.
With a paginated source, the next page is fetched only once the current one
runs out, and every worker sits idle until the fetch returns. Setting
`Prefetch` lets a dedicated goroutine pull up to that many items ahead of the
workers, so the next page is fetched while they are still busy:

```go
flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
    DrainParallel(SaveRecord, flow.ParallelOptions{
        Limit:    8,
        Prefetch: 100, // about one page
    })
```

About one batch is a good starting value. See
[examples/drain-parallel](../examples/drain-parallel/) for a timeline of the
difference it makes.

Prefetching is opt-in because if the drain stops early, on an error or
cancellation, up to `Prefetch` pulled items are discarded without being
consumed. That's harmless for reads like pagination, but avoid it for sources
where pulling removes or commits the item, like popping from a list or
auto-acknowledging queue receives. Queues that are acknowledged after
consuming are fine: discarded items are never acknowledged, so the queue
redelivers them.

With or without prefetching, the source is never called after
`DrainParallel` returns.

#### One-Shot Sources

Sources built by `Expand`, and any source that keeps its position in a
closure, are one-shot: once drained, they stay exhausted. Build a fresh one
for each run, for example by returning the pipeline from a step constructor:

```go
func SyncRecords() flow.Step[*State] {
    return flow.Expand(flow.Stream(FetchNextPage).Via(ExtractRecords)).
        DrainParallel(SaveRecord, flow.ParallelOptions{Limit: 8})
}
```

A source that keeps its position in `T`, like a pager storing its cursor on
the state, is as reusable as that state allows.

When a later stage needs the whole collection after all, `Source.Collect`
turns a source back into an `Extract[T, []U]`.

### Decision Guide: Which Pattern?

```
Processing a collection?
├─ Is it large, unbounded, or paginated, so you'd rather not hold it all in memory?
│  └─ Yes → Stream it: Stream/Expand + Drain/DrainParallel
│           - At most one item or batch buffered at a time
│           - Serial (Drain) or a worker pool (DrainParallel)
│
├─ Do you need parallel execution flexibility?
│  ├─ Yes → Use ForEach + InSerial/InParallel
│  │        - Define with ForEach
│  │        - Choose serial/parallel at orchestration level
│  │
│  └─ No → Continue...
│
└─ Do you want functional composition?
   ├─ Yes → Use Collect/Render/Apply
   │        - Compose with From, Chain, Feed, Pipeline, With
   │        - Serial execution only
   │
   └─ Either pattern works
           - Collect/Render/Apply is simpler if you don't need parallel option
           - ForEach if you might change your mind later
```

### Comparison

| Aspect | ForEach Pattern | Collect/Render/Apply | Streaming |
|--------|----------------|----------------------|-----------|
| **Execution** | Serial OR parallel | Serial only | Serial (`Drain`) or worker pool (`DrainParallel`) |
| **Usage** | Requires orchestrator (`InSerial`/`InParallel`) | Direct composition in pipelines | Direct composition, ending in a `Step` |
| **Flexibility** | Easy to switch between serial/parallel | Fixed serial execution | Switch `Drain` ↔ `DrainParallel` |
| **Composition** | Less composable (returns `StepsProvider`) | Highly composable (returns Extract/Transform/Consume) | Per-item transforms with `Source.Via` |
| **Memory** | Whole list | Whole slice | One item or batch at a time |
| **Best for** | Independent items, parallel potential | Ordered processing, functional pipelines | Large, unbounded, or paginated collections |

---

## Workflow Patterns

The library supports three main workflow patterns. Choose based on when you know what steps to execute.

### Pattern Overview

| Pattern | Steps Known At | Configuration Source | Best For |
|---------|---------------|---------------------|----------|
| **Static Workflows** | Compile time | Hardcoded in workflow definition | Fixed processes with known steps |
| **Configuration-Driven** | Compile time (templates) | Code-based configuration structures | Multi-component systems where components declare needs |
| **Dynamic Workflows** | Runtime | Computed from runtime data | Processes where steps depend on external data |

### Pattern 1: Static Workflows

Steps are hardcoded in the workflow definition. You know exactly what will execute when you write the code.

**When to use:**
- The workflow structure is fixed and well-understood
- Steps don't vary based on configuration or runtime data
- You want maximum clarity: the workflow reads like a script

**Example:**

```go
var ProvisionDatabase = flow.Do(
    CreateDatabaseInstance(),
    ConfigureNetworking(),

    // Parallel database creation
    flow.InParallel(flow.Steps(
        CreateDatabase("app"),
        CreateDatabase("analytics"),
        CreateDatabase("cache"),
    )),

    // Conditional replication setup
    flow.Do(
        CreateUser("replicator"),
        SetBinlogRetention(72),
        GrantReplicationPrivileges("replicator"),
    ).When(IsReplica()),

    // Retry-wrapped admin user creation
    CreateUser("admin").
        Then(GrantAllPrivileges("admin")).
        Retry(flow.UpTo(3), flow.ExponentialBackoff(100*time.Millisecond)),

    ValidateConfiguration(),
)
```

**Common use cases:** Single-service deployments, database migrations, build/test/deploy pipelines, data processing with fixed stages, API request/response handling.

See [examples/static/](../examples/static/) for a complete example.

### Pattern 2: Configuration-Driven Orchestration

Components embed `Step[T]` values directly in configuration structures to declare their setup needs. The orchestration layer gathers these declarations, groups them intelligently, and optimizes execution.

**When to use:**
- Multiple components with overlapping or varying setup requirements
- Optimization opportunities exist (grouping by resource, parallelization, deduplication)
- Components should declare their needs rather than directly calling shared setup code
- Configuration should be composable and testable in isolation

**The Pattern:**

1. Embed steps in configuration:

```go
type ServiceConfig struct {
    Databases []ServiceDbConfig
}

type ServiceDbConfig struct {
    Instance string                    // Which resource to use
    Setup    flow.Step[*ServiceDbSetup] // Steps to run on that resource
}
```

2. Components populate the configuration:

```go
var services = ServicesConfig{
    "webapp": {
        Databases: []ServiceDbConfig{
            {Instance: "main", Setup: flow.Do(
                CreateDatabase("webapp_db"),
                CreateUser("webapp_user"),
            )},
        },
    },
    "analytics": {
        Databases: []ServiceDbConfig{
            {Instance: "data-mart", Setup: flow.Do(
                CreateDatabase("analytics_db"),
                SetBinlogRetention(72),
            )},
        },
    },
}
```

3. Orchestration gathers, groups, and executes:

```go
func ConfigureDatabases() flow.Step[*EnvironmentSetup] {
    return flow.InParallel(flow.ForEach(
        GatherDbSetupsByInstance,   // Group steps by instance
        RunInstanceSetup,           // Execute each instance in parallel
    ))
}
```

**Why this is powerful:**
- **Separation of concerns**: Services declare needs, orchestration optimizes execution
- **Optimization**: Group by resource, parallelize across and within resources, deduplicate operations
- **Composability**: Add services or change requirements without touching orchestration code
- **Type safety**: Each step sees only the state type it needs

See [examples/config-driven/](../examples/config-driven/) for a complete example with detailed explanation.

### Pattern 3: Dynamic Runtime Workflows

Steps are generated entirely at runtime based on external data (API responses, database queries, file contents, etc.).

**When to use:**
- Steps depend on data fetched at runtime (list of servers, pending jobs, etc.)
- The number or type of operations varies based on external state
- You're building a generic executor that adapts to different scenarios

**Option A: Custom StepsProvider**

```go
func DeployActiveServers() flow.StepsProvider[*DeploymentState] {
    return func(ctx context.Context, state *DeploymentState) ([]flow.Step[*DeploymentState], error) {
        // Fetch server list from API at runtime
        servers, err := fetchActiveServers(ctx, state.Region)
        if err != nil {
            return nil, err
        }

        // Generate a step for each server
        var steps []flow.Step[*DeploymentState]
        for _, server := range servers {
            steps = append(steps, DeployToServer(server))
        }

        return steps, nil
    }
}

flow.Do(DeployActiveServers())
```

**Option B: ForEach pattern (recommended for simple cases)**

```go
func GetActiveServers(ctx context.Context, state *DeploymentState) ([]string, error) {
    return fetchActiveServers(ctx, state.Region)
}

func DeployToServer(serverID string) flow.Step[*DeploymentState] {
    return func(ctx context.Context, state *DeploymentState) error {
        return deploy(ctx, state, serverID)
    }
}

// Sequential deployment
flow.InSerial(flow.ForEach(
    GetActiveServers,
    DeployToServer,
))

// Parallel deployment
flow.InParallel(flow.ForEach(
    GetActiveServers,
    DeployToServer,
))
```

**Common use cases:** Processing queues or batches, operating on dynamic resource lists, multi-stage pipelines where each stage determines the next, executing user-provided task lists.

### Combining Patterns

You can mix patterns! For example:

```go
var DeployEnvironment = flow.Do(
    ValidateConfig(),                  // Static

    flow.InParallel(flow.ForEach(      // Dynamic: deploy to runtime-determined servers
        GetTargetServers,
        DeployToServer,
    )),

    flow.When(                         // Static conditional
        IsProduction(),
        ConfigureMonitoring(),
    ),

    RunHealthChecks(),                 // Static
)
```

---

## Advanced Topics

### State Transformation with Spawn

Complex workflows often involve different concerns with different state requirements. `Spawn` lets you transform state, execute steps on a different state type, and then return to the original:

```go
func Spawn[Parent, Child any](
	derive Extract[Parent, Child],
	step Step[Child],
) Step[Parent]
```

**Example:** Database setup with type-safe connection state:

```go
type Environment struct {
    Services *ServicesConfig
}

type DatabaseSetup struct {
    Connection *sql.DB
}

// Extract database connection from environment
func OpenDatabase(instance string) flow.Extract[*Environment, *DatabaseSetup] {
    return func(ctx context.Context, env *Environment) (*DatabaseSetup, error) {
        conn, err := sql.Open("mysql", instance)
        if err != nil {
            return nil, err
        }
        return &DatabaseSetup{Connection: conn}, nil
    }
}

// Steps that operate on DatabaseSetup
func CreateSchema() flow.Step[*DatabaseSetup] {
    return func(ctx context.Context, db *DatabaseSetup) error {
        _, err := db.Connection.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS myapp")
        return err
    }
}

// Use Spawn to run database steps within environment workflow
flow.Do(
    ValidateEnvironment(),
    OpenDatabase("main").Spawn(flow.Do(
        CreateSchema(),
        CreateTables(),
    )),
    DeployServices(),
)
```

The method form `derive.Spawn(step)` reads "derive the child state, then run
this step with it". The function form `flow.Spawn(derive, step)` is
equivalent.

The database setup steps only see `*DatabaseSetup` with a properly-typed connection, while the main workflow continues to work with `*Environment`.

### Logging

The library provides built-in logging decorators that work seamlessly with step names for observability.

#### Basic Logging with log.Logger

`WithLogging` wraps a step with automatic logging that prints when the step starts and finishes, including execution duration:

```go
step := flow.Named("process",
    flow.WithLogging(
        flow.Named("parse",
            flow.WithLogging(parseStep))))
```

Output:
```
[process] starting step
[process.parse] starting step
[process.parse] finished step (took 5ms)
[process] finished step (took 10ms)
```

Configure a custom logger with `WithLogger`:

```go
myLogger := log.New(os.Stdout, "workflow: ", log.LstdFlags)
step := flow.WithLogger(myLogger,
    flow.Named("process",
        flow.WithLogging(processStep)))
```

#### Structured Logging with slog.Logger

`WithSlogging` provides structured logging with attributes:

```go
step := flow.Named("process",
    flow.WithSlogging(slog.LevelInfo,
        flow.Named("parse",
            flow.WithSlogging(slog.LevelDebug, parseStep))))
```

Output (JSON):
```json
{"level":"info","msg":"starting step","name":"process"}
{"level":"debug","msg":"starting step","name":"process.parse"}
{"level":"debug","msg":"finished step","name":"process.parse","duration_ms":5}
{"level":"info","msg":"finished step","name":"process","duration_ms":10}
```

Configure a custom structured logger with `WithSlogger`:

```go
myLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
step := flow.WithSlogger(myLogger,
    flow.Named("process",
        flow.WithSlogging(slog.LevelInfo, processStep)))
```

#### Custom Logging

For custom logging formats, use `Names()` to retrieve the name stack and `Logger()`/`Slogger()` to access the configured logger:

```go
func CustomLogging[T any](step flow.Step[T]) flow.Step[T] {
    return func(ctx context.Context, t T) error {
        logger := flow.Logger(ctx)
        names := flow.Names(ctx)
        fullName := strings.Join(names, ".")

        logger.Printf(">>> Starting: %s\n", fullName)
        err := step(ctx, t)
        if err != nil {
            logger.Printf("<<< Failed: %s - %v\n", fullName, err)
        } else {
            logger.Printf("<<< Completed: %s\n", fullName)
        }
        return err
    }
}
```

#### How Logging Interacts with Named/AutoNamed

Logging decorators use the step name stack from the context (built by `Named`/`AutoNamed`). This means:

1. **Names compose automatically** - Each `Named` call adds to the stack, creating dotted paths like "process.parse.validate"
2. **Logging sees the full path** - `WithLogging`/`WithSlogging` always show the complete step hierarchy
3. **Order matters** - Apply `Named` before `WithLogging` so the logger knows the step name:

```go
// ✅ Correct: Named first, so logger sees the name
flow.Named("parse",
    flow.WithLogging(parseStep))

// ❌ Wrong: Logger won't see "parse" name
flow.WithLogging(
    flow.Named("parse", parseStep))
```

### Workflow-Scoped Values

Flow provides a typed key-value store scoped to the entire workflow. This is useful for cross-cutting configuration—feature flags, dry-run mode, tags—that needs to be visible everywhere, including across `Spawn` boundaries where the state type changes.

#### API

Create a key with `NewKey`, set it with the key's `Set` method, and read it
with `Get`:

```go
// Package-level key declaration (each NewKey call produces a unique key)
var DryRun = flow.NewKey[bool]("dry-run")

// Set the value at the workflow root
step := DryRun.Set(true,
    flow.Named("deploy", func(ctx context.Context, s *State) error {
        if dry, ok := DryRun.Get(ctx); ok && dry {
            log.Println("dry-run: skipping deploy")
            return nil
        }
        return realDeploy(ctx, s)
    }),
)
```

The package-level forms `flow.WithValue(key, val, step)` and
`flow.Lookup(ctx, key)` are equivalent.

Key properties:
- **Unique identity**: Two calls to `NewKey[string]("env")` produce distinct keys. Names are for debugging only.
- **Shared mutation**: Values set anywhere are visible everywhere in the workflow. The store is a shared pointer, not cloned per step.
- **Thread-safe**: The underlying store is protected by a read-write mutex.
- **Self-bootstrapping**: `WithValue` works without an outer `Named` or `Traced` wrapper.

#### Cross-Spawn Propagation

Values propagate across `Spawn` boundaries because the key store is shared:

```go
var Region = flow.NewKey[string]("region")

workflow := Region.Set("us-west-2",
    flow.Spawn(
        DeriveChildState,  // Extract[*Parent, *Child]
        func(ctx context.Context, child *Child) error {
            region, _ := Region.Get(ctx) // "us-west-2"
            return deployTo(ctx, child, region)
        },
    ),
)
```

#### Recipe: Tag-Based Selective Execution

A common pattern is using workflow-scoped values to implement tags that control which steps run:

```go
var ActiveTags = flow.NewKey[[]string]("active-tags")

func WithTags[T any](tags []string, step flow.Step[T]) flow.Step[T] {
    return ActiveTags.Set(tags, step)
}

func Tagged[T any](tag string, step flow.Step[T]) flow.Step[T] {
    return func(ctx context.Context, t T) error {
        tags, hasTags := ActiveTags.Get(ctx)
        if hasTags && !slices.Contains(tags, tag) {
            return nil // skip
        }
        return step(ctx, t)
    }
}

// Usage
workflow := flow.Do(
    Tagged("db", setupDatabase),
    Tagged("cache", warmCache),
)
WithTags([]string{"db"}, workflow) // only runs db steps
```

See `examples/tags/` for a complete runnable example.

### Resource Cleanup

Go's `defer` works at function scope, but flow steps are closures composed via `Do` — there's no natural place to `defer` cleanup for resources acquired mid-pipeline. `Scope` and `Manage` solve this by providing structured, composable resource cleanup.

#### The Scope/Manage Pattern (Primary)

`Manage` pairs an acquire `Step` with a cleanup `Step` and registers the cleanup with the enclosing `Scope`. When the scope exits, all registered cleanups run in LIFO order:

```go
workflow := flow.Scope(
    flow.Do(
        flow.Manage(OpenAndStoreConn, CloseConn),   // 1st acquired, 2nd cleaned
        flow.Manage(BeginAndStoreTx, RollbackTx),    // 2nd acquired, 1st cleaned
        DoWork,
    ),
)
```

The acquire step typically stores the resource in state `T`; the cleanup step reads it back. Reusable cleanup steps like `CloseConn` can be shared across workflows. `Manage` returns `ErrNoScope` if no enclosing `Scope` is active.

**Note**: `Manage` requires a pointer type for state (enforced at compile time). The cleanup closure captures the pointer at registration time; a value type would silently operate on a copy, missing mutations made after registration.

For early resource cleanup, use nested scopes — the inner scope's cleanups run when it exits:

```go
flow.Scope(flow.Do(
    flow.Scope(flow.Do(
        flow.Manage(OpenDB, CloseDB),
        UseDB,
    )),
    // DB is already cleaned up here
    MoreWork,
))
```

#### Cleanup Timeouts

By default, cleanup uses the step's context. If the step was cancelled (e.g., timeout), cleanup also gets a cancelled context. Use `WithCleanupTimeout` to give cleanup an independent timeout with a context detached from parent cancellation:

```go
flow.Do(
    flow.Manage(OpenConn, CloseConn),
    DoWork,
).Scoped().WithCleanupTimeout(10 * time.Second)
```

The method chain reads inner-to-outer, left to right: scope the step, then
give its cleanup a timeout. The nested function form
`flow.WithCleanupTimeout(10*time.Second, flow.Scope(...))` is equivalent.

The timeout is inherited through nested scopes and can be overridden by a closer `WithCleanupTimeout`:

```go
flow.WithCleanupTimeout(5*time.Second,
    flow.Scope(flow.Do(
        // Inner scope overrides to 1s
        flow.WithCleanupTimeout(1*time.Second,
            flow.Scope(innerWorkflow),
        ),
        outerStep,
    )),
)
```

#### Nested Scopes

Each `Scope` creates an independent cleanup scope. Inner scopes clean up their resources when they exit; outer scopes clean up theirs when they exit:

```go
flow.Scope(flow.Do(                          // outer scope
    flow.Manage(OpenConn, CloseConn),
    flow.Scope(flow.Do(                      // inner scope
        flow.Manage(BeginTx, RollbackTx),
        InsertRecords,
    )),
    // inner cleanup (RollbackTx) has already run here
    MoreOuterWork,
))
// outer cleanup (CloseConn) runs here
```

#### Thread Safety with InParallel

`Manage` and `Acquire` are safe to call concurrently from within `InParallel`. The cleanup scope uses a mutex for registration. Registration order (and therefore cleanup order) is non-deterministic with concurrent goroutines, but all cleanups will execute:

```go
flow.Scope(
    flow.InParallel(
        flow.Steps(
            flow.Manage(OpenConn1, CloseConn),
            flow.Manage(OpenConn2, CloseConn),
        ),
    ),
)
```

See `examples/cleanup/` for a complete runnable example.

### Debugging

When a workflow fails deep in a nested composition, you need context. All major types have `Named*` variants that add debug labels:

```go
step := flow.Named("validate-config", ValidateConfig())
extract := flow.NamedExtract("load-user", LoadUser)
transform := flow.NamedTransform("enrich-data", EnrichData)
consume := flow.NamedConsume("save-results", SaveResults)
```

Or use `AutoNamed` which uses reflection to determine the step name:

```go
func SomeStep() flow.Step[*Thing] {
    return flow.AutoNamed(flow.Do(
        SubStep1(),
        SubStep2(),
    ))
}
```

#### Tracing Workflow Execution

For deeper debugging and performance analysis, use `Traced` to record execution events for all `Named` steps:

```go
workflow := flow.Do(
    flow.Named("validate", ValidateConfig()),
    flow.Named("connect", ConnectDB()),
    flow.Named("migrate", RunMigrations()),
)

// Trace the workflow and write to stdout
err := flow.Spawn(
    flow.Traced(workflow),
    flow.WriteTextTo(os.Stdout),
)(ctx, state)
```

Example output:
```
validate (45ms)
connect (120ms)
migrate (2.3s)
  create-tables (1.8s)
  create-indexes (500ms)
```

**Filtering and Analysis:**

Traces can be filtered to focus on specific events:

```go
_, trace, _ := flow.Traced(workflow)(ctx, state)

// Find slow steps
slowSteps := trace.Filter(flow.MinDuration(time.Second))

// Find errors
errors := trace.Filter(flow.HasError())

// Combine filters
criticalIssues := trace.Filter(
    flow.MinDuration(500 * time.Millisecond),
    flow.HasError(),
)

// Output results
slowSteps.WriteText(os.Stderr)
```

**Available Filters:**
- `MinDuration(d)`, `MaxDuration(d)` - Filter by duration
- `HasError()`, `NoError()` - Filter by error status
- `NameMatches(pattern)` - Match step names with glob patterns
- `PathMatches(pattern)` - Match full dotted paths
- `DepthEquals(n)`, `DepthAtMost(n)` - Filter by nesting depth
- `TimeRange(start, end)` - Filter by start time window
- `ErrorMatches(pattern)` - Match error messages

**Streaming Traces:**

For real-time monitoring or to preserve traces if the process crashes, stream events to a file as they complete:

```go
f, _ := os.Create("trace.jsonl")
defer f.Close()

trace, err := flow.Traced(workflow, flow.WithStreamTo(f))(ctx, state)

// Events are written to file as JSON Lines during execution
// Trace is still available in memory for post-execution analysis
slowSteps := trace.Filter(flow.MinDuration(time.Second))
```

This writes events as [JSON Lines](https://jsonlines.org/) format (one JSON object per line), which can be processed with tools like `jq`:

```bash
# Find slow steps
cat trace.jsonl | jq 'select(.duration > 1000000000)'

# Count errors
cat trace.jsonl | jq 'select(.error != null)' | wc -l
```

**Parallel Workflows:**

For parallel workflows where tree indentation may be misleading, use `WriteFlatText`:

```go
// Flat chronological output
trace.WriteFlatText(os.Stdout)

// Or sort events by start time for precise chronological order
events := trace.Events
sort.Slice(events, func(i, j int) bool {
    return events[i].Start.Before(events[j].Start)
})
```

**Testing with Traces:**

Tracing is useful in tests for asserting timing constraints and execution paths:

```go
func TestWorkflowPerformance(t *testing.T) {
    _, trace, err := flow.Traced(myWorkflow)(ctx, state)
    if err != nil {
        t.Fatal(err)
    }

    // Assert no step took longer than 100ms
    slow := trace.Filter(flow.MinDuration(100 * time.Millisecond))
    if len(slow.Events) > 0 {
        t.Errorf("steps exceeded timeout:\n")
        slow.WriteText(os.Stderr)
    }

    // Assert specific steps executed
    migrations := trace.Filter(flow.NameMatches("migrate*"))
    if len(migrations.Events) == 0 {
        t.Error("no migration steps executed")
    }
}
```

**Performance Considerations:**

- Tracing is opt-in with minimal overhead when not used
- When enabled, tracing adds ~2-3x overhead
- Streaming adds ~3.4x additional overhead on top of tracing
- Combined overhead (~7-10x) becomes negligible in IO-bound workflows where network/disk delays dominate
- All `Named`, `NamedExtract`, `NamedTransform`, `NamedConsume`, and `AutoNamed` variants automatically record events when a trace is present
- For production use, trace only critical workflow sections or enable only when debugging
- See `examples/tracing/` for complete examples

### Mega-Wrappers: Factoring Out Common Patterns

When you find yourself repeatedly wrapping steps with the same decorators (e.g., `AutoNamed(WithLogging(Retry(...)))`), you can create custom "mega-wrapper" functions to reduce boilerplate.

#### The Problem

Without mega-wrappers, common patterns become repetitive:

```go
func ProcessOrder() flow.Step[*State] {
    return flow.AutoNamed(
        flow.WithLogging(
            flow.Retry(
                func(ctx context.Context, s *State) error {
                    // Process order
                    return nil
                })))
}

func ProcessPayment() flow.Step[*State] {
    return flow.AutoNamed(
        flow.WithLogging(
            flow.Retry(
                func(ctx context.Context, s *State) error {
                    // Process payment
                    return nil
                })))
}

// ... repeat for every step
```

#### The Solution: SkipCaller

Create a wrapper function and use `SkipCaller(1)` to adjust the call stack depth so `AutoNamed` sees the correct calling function:

```go
// megaWrapper applies common decorators: naming, logging, and retry
func megaWrapper[T any](step flow.Step[T]) flow.Step[T] {
    return flow.AutoNamed(
        flow.WithLogging(
            flow.Retry(step)),
        flow.SkipCaller(1))  // Skip 1 level to see the actual caller
}

// Now your steps are much cleaner
func ProcessOrder() flow.Step[*State] {
    return megaWrapper(func(ctx context.Context, s *State) error {
        // Process order
        return nil
    })
}

func ProcessPayment() flow.Step[*State] {
    return megaWrapper(func(ctx context.Context, s *State) error {
        // Process payment
        return nil
    })
}
```

With `SkipCaller(1)`, `AutoNamed` skips the `megaWrapper` function in the call stack and extracts the name from `ProcessOrder` or `ProcessPayment` instead.

#### How SkipCaller Works

By default, `AutoNamed` extracts the name from the function that directly calls it. But when you wrap `AutoNamed` in another function, you need to skip additional call stack frames:

```
Call Stack:
├─ ProcessOrder()           ← We want this name
│  └─ megaWrapper()         ← Skip this with SkipCaller(1)
│     └─ AutoNamed()        ← AutoNamed looks here by default
```

- **`SkipCaller(0)`** - Default behavior, uses the direct caller
- **`SkipCaller(1)`** - Skip 1 level, used in mega-wrappers
- **`SkipCaller(2)`** - Skip 2 levels, for nested wrappers

#### Advanced: Parameterized Mega-Wrappers

You can create mega-wrappers that accept configuration:

```go
// Mega-wrapper with configurable retry attempts
func retriableStep[T any](maxRetries int, step flow.Step[T]) flow.Step[T] {
    return flow.AutoNamed(
        flow.WithLogging(
            flow.Retry(
                step,
                flow.UpTo(maxRetries))),
        flow.SkipCaller(1))
}

func FlakyAPICall() flow.Step[*State] {
    return retriableStep(5, func(ctx context.Context, s *State) error {
        // Call flaky API with 5 retry attempts
        return callAPI(ctx)
    })
}

func QuickOperation() flow.Step[*State] {
    return retriableStep(2, func(ctx context.Context, s *State) error {
        // Quick operation with just 2 retry attempts
        return quickOp(ctx)
    })
}
```

#### When to Use Mega-Wrappers

**Good fit:**
- You have a standard set of decorators applied to most steps
- The pattern repeats across many step constructors
- You want to enforce consistent error handling/logging/retry policies

**Not recommended:**
- Steps have varying decorator requirements
- The wrapper adds more complexity than it saves
- You only have a few steps

### Thread Safety in Parallel Execution

When using `InParallel`, be careful about concurrent access to shared state.
The same goes for the consumer passed to `DrainParallel`, which runs on
several workers at once (though the source it pulls from does not):

```go
// ❌ Dangerous: Multiple goroutines modifying state concurrently
func IncrementCounter() flow.Step[*State] {
    return func(ctx context.Context, state *State) error {
        state.counter++  // Data race!
        return nil
    }
}
```

**Solutions:**

1. Use synchronization primitives:
```go
func IncrementCounter() flow.Step[*State] {
    return func(ctx context.Context, state *State) error {
        state.mu.Lock()
        defer state.mu.Unlock()
        state.counter++
        return nil
    }
}
```

2. Use atomic operations:
```go
func IncrementCounter() flow.Step[*State] {
    return func(ctx context.Context, state *State) error {
        atomic.AddInt64(&state.counter, 1)
        return nil
    }
}
```

3. Design for read-only access in parallel steps:
```go
// Better: Parallel steps only read, serial steps write
flow.Do(
    InitializeState(),
    flow.InParallel(
        flow.ForEach(GetItems, ProcessItem),  // Only reads from state
    ),
    AggregateResults(),  // Writes results after parallel work completes
)
```

For more advanced concurrency patterns (channels, immutable data structures, testing with -race), consult the
[Go Memory Model](https://golang.org/ref/mem) and [Effective Go](https://golang.org/doc/effective_go) documents.

### Common Pitfalls

#### Confusing Do() vs InSerial() vs InParallel()

**`Do(steps...)`** - Takes static `Step[T]` values:
```go
flow.Do(
    ValidateConfig(),      // Step[*State]
    StartDatabase(),       // Step[*State]
)
```

**`InSerial(providers...)`** - Takes dynamic `StepsProvider[T]` values:
```go
flow.InSerial(
    flow.ForEach(GetServices, DeployService),  // StepsProvider[*State]
)
```

**`InParallel(providers...)`** - Takes dynamic `StepsProvider[T]` values, executes in parallel:
```go
flow.InParallel(
    flow.ForEach(GetServices, DeployService),  // StepsProvider[*State]
)
```

**Rule of thumb:**
- Static steps you write directly → `Do`
- Dynamic steps from `ForEach`, etc. → `InSerial` or `InParallel`
- Want parallel execution → `InParallel`

#### Type Mismatches in Composition

Each combinator has specific type requirements:

```go
// ❌ Wrong: With expects Extract and Consume, not Transform
flow.With(
    GetUsers,          // Extract[*State, []User] ✓
    TransformUsers,    // Transform[*State, []User, []ProcessedUser] ✗
)

// ✅ Correct: Use Pipeline for Extract → Transform → Consume
flow.Pipeline(
    GetUsers,          // Extract[*State, []User]
    TransformUsers,    // Transform[*State, []User, []ProcessedUser]
    SaveUsers,         // Consume[*State, []ProcessedUser]
)
```

**Quick reference:**
- `With(extract, consume)` / `extract.To(consume)` → `Extract + Consume → Step`
- `From(extract, transform)` / `extract.Via(transform)` → `Extract + Transform → Extract`
- `Feed(transform, consume)` / `transform.To(consume)` → `Transform + Consume → Consume`
- `Pipeline(extract, transform, consume)` / `extract.Via(transform).To(consume)` → `Extract + Transform + Consume → Step`
- `Chain(transform1, transform2)` / `transform1.Then(transform2)` → `Transform + Transform → Transform`

---

## Next Steps

- **[Design Philosophy](design.md)** - Understanding the patterns and principles behind the library
- **[Complete Examples](../examples/)** - Runnable examples showing real-world usage
- **Package documentation** - Run `go doc github.com/sam-fredrickson/flow` or visit [pkg.go.dev](https://pkg.go.dev/github.com/sam-fredrickson/flow)
