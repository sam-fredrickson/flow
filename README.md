# flow

A Go library for building complex, type-safe workflows from simple steps.

## Installation

```bash
go get github.com/sam-fredrickson/flow
```

Requires Go 1.27+ (the fluent API uses generic methods).

## Quick Example

```go
import "github.com/sam-fredrickson/flow"

type Config struct {
    DatabaseURL string
}

// Define steps that operate on *Config
setupDb := func(ctx context.Context, cfg *Config) error {
    return setupDatabase(ctx, cfg.DatabaseURL)
}

runMigrations := func(ctx context.Context, cfg *Config) error {
    return applyMigrations(ctx, cfg.DatabaseURL)
}

// Compose workflow
workflow := flow.Do(setupDb, runMigrations)

// Execute it
cfg := &Config{DatabaseURL: "postgres://..."}
if err := workflow(context.Background(), cfg); err != nil {
    log.Fatal(err)
}
```

## Key Features

**Automatic retry** with exponential backoff and jitter:

```go
flow.Retry(
    CallExternalAPI(),
    flow.UpTo(3),
    flow.ExponentialBackoff(100*time.Millisecond, flow.WithFullJitter()),
)
// Those are the default settings, so this can be simplified
flow.Retry(CallExternalAPI())
```

**Parallel execution** with automatic error handling and goroutine management:

```go
flow.InParallel(flow.Steps(
    DeployService("web"),
    DeployService("api"),
    DeployService("worker"),
))
```

**Dynamic workflows** where steps are determined at runtime:

```go
flow.InParallel(
    flow.ForEach(GetServices, DeployService),
)
```

**Fluent composition** for data pipelines and decorators, reading
left-to-right in data-flow order:

```go
// Collect pages from an API, extract and validate records, save each one.
flow.Collect(FetchNextPage).
    Via(flow.Render(ExtractRecords)).
    Via(flow.Flatten).
    Via(flow.Render(ValidateRecord)).
    To(flow.Apply(SaveRecord))

// Decorators chain as methods.
CreateUser("admin").
    Then(GrantAllPrivileges("admin")).
    Retry(flow.UpTo(3))
```

For more sophisticated patterns, see:

- **[Configuration-driven orchestration](examples/config-driven/)** — Components declare their needs, orchestration layer optimizes execution
- **[Data transformation pipelines](examples/data-pipeline/)** — Type-safe functional composition for processing collections

## Documentation

- **[Feature Guide](docs/guide.md)** — Complete guide covering all features and patterns
- **[Design Philosophy](docs/design.md)** — Understanding the principles and motivation behind the library
- **[Complete Examples](examples/)** — Runnable examples showing real-world usage
- **Package documentation:** Run `go doc github.com/sam-fredrickson/flow` or visit [pkg.go.dev](https://pkg.go.dev/github.com/sam-fredrickson/flow)
