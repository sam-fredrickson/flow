# Design Philosophy

## Motivation: The Problem with Manual Orchestration

As automation workflows evolve from simple scripts to complex orchestration, the code becomes increasingly difficult to maintain. What starts as 20 lines of sequential operations grows into hundreds of lines managing goroutines, error channels, retry logic, and conditional branching.

### The Pain Points

**Repetitive boilerplate**: Every workflow that needs parallelism reinvents the same WaitGroup/error channel pattern. Every API call that needs retries reimplements exponential backoff. The mechanics of orchestration drown out the business logic.

**Scattered control flow**: Conditional logic, error handling, parallel execution, and retry logic all intertwine. A single workflow might have nested if-statements checking error types, goroutines with error channels, select statements for timeouts, and manual retry loops. Understanding what the workflow *does* requires parsing through the orchestration mechanics.

**Difficulty testing**: When the "what" (business logic) and "how" (execution mechanics) are coupled, testing becomes cumbersome. You can't test the workflow structure without also dealing with goroutines, channels, and timing.

**Growth complexity**: What starts as a simple sequential script becomes unwieldy as requirements grow. Adding parallelism, retries, or conditionals requires restructuring large portions of the code. Each change risks introducing subtle concurrency bugs or error handling gaps.

### The Alternative: Declarative Composition

Rather than manually orchestrating each workflow, this library lets you **compose workflows from simple steps** and **declare how they should execute**. The library handles the mechanics—goroutines, error collection, retry logic, context propagation—while your code focuses on the business logic.

Instead of writing:
```go
var wg sync.WaitGroup
errCh := make(chan error, 3)
for _, db := range databases {
    wg.Add(1)
    go func(name string) {
        defer wg.Done()
        if err := createDB(ctx, name); err != nil {
            errCh <- err
        }
    }(db)
}
wg.Wait()
close(errCh)
for err := range errCh {
    return err
}
```

You write:
```go
flow.InParallel(flow.ForEach(
    GetDatabases,
    CreateDB,
))
```

The mechanics are handled. Your code declares intent.

---

## Core Principles

### Composition over Manual Control Flow

Steps are just functions with the signature `func(context.Context, T) error`. This makes them trivially composable without special machinery.

The library provides combinators (`Do`, `InParallel`, `Retry`, `When`, etc.) that handle the mechanics of execution, error handling, and concurrency, letting you focus on *what* should happen rather than *how* to orchestrate it.

### Type Safety

Generics ensure type safety throughout your workflows. The compiler catches type mismatches at compile time.

**Steps must share state types:**

```go
var userFlow = SetupUser()        // Step[*User]
var configFlow = SetupConfig()    // Step[*Config]

// Compile error: cannot mix different state types
flow.Do(
    userFlow, configFlow,
)
```

**Type-safe state transformation:**

When you need to work with different state types, use `Spawn` for safe, explicit transformations:

```go
type Application struct {
    Config *Config
}

type DatabaseConnection struct {
    DB *sql.DB
}

// This won't compile - type mismatch
flow.Do(
    ValidateApp(),           // Step[*Application]
    MigrateSchema(),         // Step[*DatabaseConnection] - ERROR!
)

// Instead, use Spawn for explicit, type-safe transformation
flow.Do(
    ValidateApp(),           // Step[*Application]
    flow.Spawn(
        OpenDB(),            // Extract[*Application, *DatabaseConnection]
        flow.Do(
            MigrateSchema(), // Step[*DatabaseConnection] - OK!
            SeedData(),      // Step[*DatabaseConnection] - OK!
        ),
    ),
    StartApp(),              // Step[*Application] - back to original type
)
```

The type system ensures:
- You can't accidentally pass application state to database-specific steps
- The transformation from `*Application` to `*DatabaseConnection` is explicit and visible
- After the spawned steps complete, you're back to working with `*Application`

This makes workflows both safer and more self-documenting. When you see `Spawn`, you know the workflow is switching to a different concern with its own state type.

### No Lock-In

Because `Step[T]` is just a type alias for a function signature, there's no dependency on special interfaces, base classes, or framework infrastructure. This has important implications:

**Steps are ordinary functions**: You can write a step without importing the library. Any function with signature `func(context.Context, T) error` is already a valid step.

**No runtime coupling**: There's no global registry, no background goroutines, no hidden state. The library provides pure functions that compose other functions.

**Interoperability**: Flow steps can call non-Flow code, and non-Flow code can call Flow steps. They're just functions.

**Incremental adoption**: You don't need to rewrite your codebase. Wrap one flaky API call with `flow.Retry()`. Use `flow.InParallel()` for one parallel operation. Leave everything else unchanged.

**Easy to replace**: If you outgrow the library or your needs change, flow steps are just functions. You can replace `flow.Do(a, b, c)` with sequential function calls, or `flow.InParallel()` with errgroup, and your individual steps remain unchanged.

This design philosophy—functions, not framework—means you're never locked in. Use the library where it helps, ignore it where it doesn't.

---

## Configuration as Code

One of the library's most powerful patterns is treating `Step[T]` as a first-class value that can be embedded in configuration structures. This enables a design philosophy where **components declare what they need**, and **orchestration determines how to execute it**.

### Steps as First-Class Values

In Go, functions are first-class values. Since `Step[T]` is just a function type, you can:
- Store them in struct fields
- Pass them as parameters
- Return them from functions
- Collect them in slices or maps

This enables configuration structures that don't just declare data, but also declare *behavior*:

```go
type ServiceConfig struct {
    Name      string
    Databases []DatabaseConfig
}

type DatabaseConfig struct {
    Instance string
    Setup    flow.Step[*DatabaseSetup]  // Behavior, not just data
}
```

### Separation of Declaration and Execution

Traditional approaches mix declaration with execution:

```go
// Service directly calls setup functions
func SetupService(ctx context.Context, svc *Service) error {
    // Service has to know about shared resources
    mainDB := openDatabase("main")
    defer mainDB.Close()

    createDatabase(mainDB, svc.DatabaseName)
    createUser(mainDB, svc.Username)

    // Every service repeats this pattern
}
```

Problems:
- Services couple to execution details (opening connections, calling functions)
- Can't optimize across services (each opens its own connection)
- Hard to change execution strategy (what if we want to parallelize?)

The configuration-driven approach separates concerns:

```go
// Service declares what it needs
var serviceConfig = ServiceConfig{
    Name: "webapp",
    Databases: []DatabaseConfig{
        {
            Instance: "main",
            Setup: flow.Do(
                CreateDatabase("webapp_db"),
                CreateUser("webapp_user"),
            ),
        },
    },
}

// Orchestration layer decides how to execute
// - Groups all operations by database instance
// - Opens one connection per instance
// - Runs all instance operations in parallel
flow.InParallel(flow.ForEach(
    GatherAndGroupByInstance,
    RunInstanceSetup,
))
```

Benefits:
- **Declaration is pure**: Services declare needs without coupling to execution
- **Optimization flexibility**: Orchestration can group, parallelize, deduplicate as needed
- **Easy evolution**: Add services by adding configuration; change execution by changing orchestration
- **Testability**: Test service declarations in isolation from orchestration

### Why This Works in Go

This pattern leverages Go's strengths:

**Closures capture context**: Step constructors can capture parameters, so `CreateDatabase("webapp_db")` produces a step that "remembers" the database name.

**Type safety**: Embedding `flow.Step[*DatabaseSetup]` in configuration means the compiler ensures each service's setup steps operate on the correct state type.

**Structural typing**: No special interfaces needed. Any function with the right signature works.

**Explicit over implicit**: The configuration explicitly shows what each service needs. The orchestration explicitly shows how execution is optimized. No hidden magic.

### When to Use This Pattern

Configuration-as-code makes sense when:
- Multiple components have similar but varying requirements
- Optimization opportunities exist (grouping, parallelization, deduplication)
- Components should be loosely coupled from execution strategy
- Configuration should be composable and testable

It's overkill when:
- Single component with fixed requirements
- No optimization opportunities
- The indirection adds more complexity than it removes

See [examples/config-driven/](../examples/config-driven/) for a complete implementation and detailed walkthrough.

---

## Use Case Archetypes

The library's design emerged from real-world automation challenges. Here are four archetypes that illustrate where it provides the most value.

### 1. Infrastructure Provisioning

**Scenario**: Provision an environment with multiple services, each needing cloud resources, databases, networking, and monitoring.

**Why flow helps**:
- **Multi-stage orchestration**: Infrastructure setup (Terraform), database configuration, service deployment, validation—each stage has dependencies
- **Parallel resource creation**: Provision independent resources concurrently (multiple databases, multiple cloud accounts)
- **Conditional configuration**: Production gets monitoring and replication, development doesn't
- **Retry-critical operations**: Cloud APIs throttle and fail transiently—automatic retry with backoff is essential
- **Configuration-driven**: Services declare their infrastructure needs; orchestration optimizes execution

**Pattern**: Mix of static workflow structure with configuration-driven database/service setup

### 2. Multi-Stage Deployment Pipeline

**Scenario**: Deploy an application through multiple environments (staging → canary → production) with health checks, rollback capability, and notifications.

**Why flow helps**:
- **Sequential stages with validation**: Deploy to staging, run tests, deploy to canary, monitor metrics, deploy to production
- **Parallel deployment within stages**: Deploy to multiple regions or availability zones concurrently
- **Conditional rollback**: If health checks fail, rollback to previous version
- **Best-effort notifications**: Slack notifications shouldn't block deployment if they fail
- **Dynamic targets**: Deploy to all active instances returned by service discovery

**Pattern**: Static outer structure (stages) with dynamic inner workflows (instances to deploy)

### 3. Data Processing Pipeline

**Scenario**: Fetch data from multiple sources, transform it, validate it, and load it into a data warehouse—all with error handling and retry logic.

**Why flow helps**:
- **Parallel fetching**: Retrieve data from multiple APIs concurrently
- **Transformation pipeline**: Parse → validate → enrich → transform, passing data through functional composition
- **Selective retry**: Retry transient API failures but not validation errors
- **Error collection**: Continue processing other sources even if one fails (use `JoinErrors`)
- **State isolation**: Each source's processing uses `Spawn` to work with source-specific state

**Pattern**: Parallel extraction with functional pipelines for transformation

### 4. Multi-Tenant Resource Setup

**Scenario**: Set up resources for multiple tenants, where each tenant has different requirements (databases, queues, storage, permissions).

**Why flow helps**:
- **Configuration-driven**: Each tenant declares their resource needs in configuration
- **Intelligent grouping**: Group operations by resource type (all database setups together, all queue setups together)
- **Parallel execution**: Provision all tenants concurrently, with resource operations for each tenant also parallelized
- **Type-safe resource contexts**: Database setup sees database state, queue setup sees queue state
- **Easy extensibility**: Adding a new tenant or new resource type is just configuration

**Pattern**: Configuration-driven orchestration with dynamic parallelization

### Common Threads

Across these archetypes, the library provides value when you need:
- **Multiple execution strategies** (sequential, parallel, conditional) in one workflow
- **Consistent error handling** and retry logic without boilerplate
- **Clear separation** between business logic and orchestration mechanics
- **Composability** so workflows can grow without becoming unmanageable
- **Type safety** to catch configuration errors at compile time

---

## Next Steps

- **[Feature Guide](guide.md)** - Comprehensive guide to all features and patterns
- **[Complete Examples](../examples/)** - Runnable examples showing real-world usage
