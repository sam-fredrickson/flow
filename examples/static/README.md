# Static Workflow Example

This example demonstrates a **static workflow**—one where all steps are known at compile time.

## The Workflow

This is a database provisioning workflow that:

1. Creates the database instance
2. Configures networking
3. Creates databases in parallel (`app`, `analytics`, `cache`)
4. Creates standard users sequentially (using `From` + `Map` with a static list)
5. Conditionally sets up replication (only if `IsReplica()` returns true)
6. Creates admin user with retry logic
7. Creates monitoring user with best-effort (`IgnoreError`)
8. Validates the final configuration

## Key Features Demonstrated

### Sequential and Parallel Execution

```go
flow.Do(
    CreateDatabaseInstance(),
    ConfigureNetworking(),

    // Parallel execution
    flow.InParallel(flow.Steps(
        CreateDatabase("app"),
        CreateDatabase("analytics"),
        CreateDatabase("cache"),
    )),

    // More sequential steps...
)
```

### From + Map with Static Data

```go
flow.From(
    func(_ context.Context, spec *DatabaseSpec) ([]string, error) {
        return []string{"app_user", "analytics_user", "readonly_user"}, nil
    },
    flow.Map(func(username string) flow.Step[*DatabaseSpec] {
        return flow.Do(
            CreateUser(username),
            GrantDatabaseAccess(username, username+"_db"),
        )
    }),
)
```

Note that while this uses the `From` + `Map` pattern (typically associated with dynamic workflows), the list of users is **static**—it's hardcoded in the function, not fetched from external data. This is still a static workflow because the structure doesn't vary at runtime based on external state.

### Conditional Logic

```go
flow.When(
    IsReplica(),
    flow.Do(
        CreateUser("replicator"),
        SetBinlogRetention(72),
        GrantReplicationPrivileges("replicator"),
        GrantGlobalReadAccess("replicator"),
    ),
)
```

### Retry with Backoff

```go
flow.Retry(
    flow.Do(
        CreateUser("admin"),
        GrantAllPrivileges("admin"),
    ),
    flow.UpTo(3),
    flow.ExponentialBackoff(100*time.Millisecond),
)
```

### Best-Effort Operations

```go
flow.IgnoreError(
    flow.Do(
        CreateUser("monitor"),
        GrantMetricsAccess("monitor"),
    ),
)
```

## When to Use Static Workflows

Static workflows are appropriate when:
- You know all the steps when writing the code
- The workflow structure doesn't vary based on runtime data
- You want maximum clarity—the workflow reads like a script

See the [Workflow Patterns](../../docs/guide.md#workflow-patterns) section of the guide for comparison with other patterns.

## Running the Example

```bash
cd examples/static
go run example.go
```

Since this is a demonstration, all database operations are stubbed out. In a real implementation, you would execute actual SQL commands, call cloud provider APIs, etc.

## Related Examples

- **Configuration-Driven**: See [examples/config-driven/](../config-driven/) for a pattern where components declare their needs in configuration
- **Feature Guide**: See [docs/guide.md](../../docs/guide.md) for complete documentation of all features and patterns
