# Configuration-Driven Orchestration Example

This example demonstrates the **configuration-driven orchestration pattern**—one of the most powerful use cases for the `flow` library.

## The Problem

When setting up complex environments with multiple services, each service has its own setup requirements:

- **webapp** needs a database on the `main` instance
- **analytics** needs a database on the `data-mart` instance with special binlog retention
- **reporting** needs a database on `data-mart` AND read access to the analytics database
- **background-jobs** needs databases on BOTH `main` and `cache` instances

### The Traditional Approach

Traditionally, you might write imperative setup code:

```go
// Open connection to main instance
mainDB := openConnection("main")
createDatabase(mainDB, "webapp_db")
createUser(mainDB, "webapp_user")
grantAccess(mainDB, "webapp_user", "webapp_db")

// ... repeat for each service

// Open connection to data-mart instance
dataMartDB := openConnection("data-mart")
createDatabase(dataMartDB, "analytics_db")
// ... etc
```

**Problems:**
- Connections opened/closed repeatedly (inefficient)
- Hard to parallelize (which operations can run concurrently?)
- Service setup scattered across codebase (hard to understand what each service needs)
- Difficult to add new services or modify requirements
- No clear separation between "what" and "how"

## The Configuration-Driven Solution

This example shows a better approach: **services declare their needs in configuration, and the orchestration layer optimizes execution**.

### 1. Services Declare Their Needs

Each service declares what databases it needs and what setup steps to run:

```go
var services = ServicesConfig{
    Configs: map[ServiceName]ServiceConfig{
        "webapp": {
            Databases: []ServiceDbConfig{
                {
                    Instance: "main",
                    Setup: flow.Do(
                        CreateDatabase("webapp_db"),
                        CreateUser("webapp_user"),
                        GrantReadWrite("webapp_user", "webapp_db"),
                    ),
                },
            },
        },
        "analytics": {
            Databases: []ServiceDbConfig{
                {
                    Instance: "data-mart",
                    Setup: flow.Do(
                        CreateDatabase("analytics_db"),
                        CreateUser("analytics_user"),
                        GrantReadWrite("analytics_user", "analytics_db"),
                        SetBinlogRetention(72), // Service-specific requirement
                    ),
                },
            },
        },
        "reporting": {
            Databases: []ServiceDbConfig{
                {
                    Instance: "data-mart",
                    Setup: flow.Do(
                        CreateDatabase("reporting_db"),
                        CreateUser("reporting_user"),
                        GrantReadWrite("reporting_user", "reporting_db"),
                        // Cross-service dependency
                        GrantReadOnly("reporting_user", "analytics_db"),
                    ),
                },
            },
        },
        // ... more services
    },
}
```

**Key insight:** `Setup` is a `flow.Step[*ServiceDbSetup]`—a first-class value embedded in configuration.

### 2. Orchestration Gathers and Groups

The orchestration layer gathers all setup steps and groups them by database instance:

```go
func GatherDbSetup(ctx context.Context, setup *EnvironmentSetup) ([]DbInstanceSetup, error) {
    stepsByInstance := map[DatabaseInstance][]flow.Step[*ServiceDbSetup]{}

    // Gather steps from each service config
    for serviceName, cfg := range setup.Services.Configs {
        for _, db := range cfg.Databases {
            if db.Setup == nil {
                continue
            }

            // Wrap with service name for error attribution
            wrappedStep := flow.Named(string(serviceName), db.Setup)
            stepsByInstance[db.Instance] = append(stepsByInstance[db.Instance], wrappedStep)
        }
    }

    // Convert to setup structures (one per instance)
    var allSetups []DbInstanceSetup
    for instance, steps := range stepsByInstance {
        allSetups = append(allSetups, DbInstanceSetup{
            Connection: nil, // TODO: open connection
            Instance:   instance,
            Steps:      steps,
        })
    }

    return allSetups, nil
}
```

**Result:**
- All steps for the `main` instance grouped together
- All steps for the `data-mart` instance grouped together
- All steps for the `cache` instance grouped together

### 3. Optimized Execution

Now execute efficiently:

```go
func ConfigureDatabases() flow.Step[*EnvironmentSetup] {
    return flow.InParallel(flow.From(
        GatherDbSetup,               // Gather and group by instance
        flow.Map(RunDbSetup),    // Run each instance's setup
    ))
}
```

**Execution plan:**
1. **Parallel across instances**: `main`, `data-mart`, and `cache` all configure concurrently
2. **Parallel within instances**: All operations on a single instance run concurrently
3. **One connection per instance**: Connection opened once, shared across all operations
4. **Error attribution**: Each step wrapped with service name, so errors clearly identify which service failed

### 4. Type Safety with `Spawn`

Notice that service configs work with `*ServiceDbSetup` (has database connection), but the main workflow uses `*EnvironmentSetup`. How do we switch between state types?

Use `Spawn`:

```go
func RunDbSetup(instance DbInstanceSetup) flow.Step[*EnvironmentSetup] {
    return flow.Spawn(
        PrepareDbSetup(instance),                       // Extract[*EnvironmentSetup, *ServiceDbSetup]
        flow.InParallel(flow.Steps(instance.Steps...)), // Step[*ServiceDbSetup]
    )                                                   // → Step[*EnvironmentSetup]
}

func PrepareDbSetup(instance DbInstanceSetup) flow.Extract[*EnvironmentSetup, *ServiceDbSetup] {
    return func(ctx context.Context, env *EnvironmentSetup) (*ServiceDbSetup, error) {
        return &ServiceDbSetup{DB: instance.Connection}, nil
    }
}
```

`Spawn` creates a new state type (`*ServiceDbSetup`), runs steps on it, then returns to the original type (`*EnvironmentSetup`).

## Why This Pattern Is Powerful

### Separation of Concerns

**Services declare WHAT they need:**
```go
"webapp": {
    Databases: []ServiceDbConfig{
        {Instance: "main", Setup: ...},
    },
}
```

**Orchestration determines HOW and WHEN:**
```go
flow.InParallel(flow.From(
    GatherDbSetup,
    flow.Map(RunDbSetup),
))
```

No coupling between service configuration and execution strategy.

### Optimization Opportunities

The orchestration layer can:
- **Group operations** by resource (one connection per instance)
- **Parallelize** across resources (all instances concurrently)
- **Parallelize** within resources (all operations on one instance concurrently)
- **Deduplicate** redundant operations
- **Cache** expensive computations
- **Retry** transient failures
- **Log/trace** for observability

Services don't need to know about any of this.

### Easy to Evolve

**Adding a new service:**
```go
"new-service": {
    Databases: []ServiceDbConfig{
        {Instance: "main", Setup: ...},
    },
}
```

Done. Orchestration automatically includes it.

**Changing requirements:**
```go
// analytics now needs an additional operation
Setup: flow.Do(
    CreateDatabase("analytics_db"),
    CreateUser("analytics_user"),
    GrantReadWrite("analytics_user", "analytics_db"),
    SetBinlogRetention(72),
    ConfigureReplication(), // NEW
),
```

No changes needed to orchestration code.

### Type Safety

The compiler ensures:
- Service setup steps can only access `*ServiceDbSetup` (with database connection)
- Main workflow steps can only access `*EnvironmentSetup` (without database connection)
- Can't accidentally pass the wrong state to a step

### Testability

Each service config can be tested in isolation:

```go
func TestWebappDatabaseSetup(t *testing.T) {
    config := serviceConfigs["webapp"]
    setup := &ServiceDbSetup{DB: testDB}

    err := config.Databases[0].Setup(context.Background(), setup)

    assert.NoError(t, err)
    // verify database, user, and grants were created
}
```

No need to test the entire environment setup to verify one service's configuration.

## Code Walkthrough

### Key Types

```go
// Services configuration (what)
type ServicesConfig struct {
    Configs map[ServiceName]ServiceConfig
}

type ServiceConfig struct {
    Databases []ServiceDbConfig
}

type ServiceDbConfig struct {
    Instance DatabaseInstance           // Which instance to use
    Setup    flow.Step[*ServiceDbSetup] // What to do on that instance
}

// Orchestration structure (how)
type DbInstanceSetup struct {
    Connection       *sql.DB
    DatabaseInstance DatabaseInstance
    Steps            []flow.Step[*ServiceDbSetup]
}

// State types
type EnvironmentSetup struct {
    Services *ServicesConfig
}

type ServiceDbSetup struct {
    DB *sql.DB
}
```

### Execution Flow

```
SetupEnvironment()
  └─ Phase1() - infrastructure setup
       ├─ CreateAwsAccount()
       ├─ CreateTerraformWorkspace()
       ├─ GenerateTerraformFiles()
       ├─ RunTerraformPlan()
       └─ ApplyTerraformPlan()
  └─ Phase2() - service configuration
       └─ ConfigureDatabases()
            └─ InParallel(From(GatherDbSetup, Map(RunDbSetup)))
                 ├─ GatherDbSetup() - groups steps by instance
                 └─ Map(RunDbSetup)
                      ├─ RunDbSetup("main") - executes all steps for main instance in parallel
                      ├─ RunDbSetup("data-mart") - executes all steps for data-mart instance in parallel
                      └─ RunDbSetup("cache") - executes all steps for cache instance in parallel
```

## Running the Example

```bash
cd examples/config-driven
go run example.go
```

Since this is a demonstration, all the actual database operations are stubbed out. In a real implementation, you would:

1. Actually open database connections in `GatherDbSetup`
2. Execute real SQL commands in the step constructors (`CreateDatabase`, `CreateUser`, etc.)
3. Add proper error handling and logging
4. Add configuration for connection strings, credentials, etc.

## When to Use This Pattern

**Good fit:**
- Multi-component systems (microservices, multi-tenant, plugins)
- Components have varying/overlapping requirements
- Optimization opportunities exist (grouping, parallelization)
- Configuration should be composable and testable

**Not a good fit:**
- Single component with fixed requirements
- No optimization opportunities
- The indirection adds more complexity than it saves

## Related Patterns

- **Static Workflows**: See [examples/static/](../static/) for workflows with fixed steps
- **Dynamic Workflows**: See the [Workflow Patterns](../../docs/guide.md#workflow-patterns) section of the guide for runtime-determined steps

## Learn More

- **[Feature Guide](../../docs/guide.md)** - Complete documentation of all features and patterns
- **[Design Philosophy](../../docs/design.md)** - The principles behind the library
