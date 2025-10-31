// SPDX-License-Identifier: Apache-2.0

// Package flow provides a library for building complex, type-safe workflows
// from simple steps—with built-in retry, parallelism, and configuration-driven
// orchestration.
//
// # The Problem
//
// As workflows grow from simple scripts to complex orchestration, the code
// becomes harder to maintain. Every workflow needs goroutines, WaitGroups,
// error channels, and retry logic with exponential backoff. Conditional logic,
// parallel execution, and error handling mix together, obscuring the business
// logic. What starts as a 10-line script becomes 100 lines of nested
// if-statements and goroutine coordination.
//
// Flow addresses these problems by letting you compose workflows from simple,
// reusable steps, while handling the mechanics of execution, error handling,
// and concurrency.
//
// # Core Concepts
//
// [Step] is the fundamental building block. A step is a function that accepts
// a context and state, returning an error:
//
//	type Step[T any] = func(context.Context, T) error
//
// Steps are composable: you can combine them to build complex workflows while
// maintaining type safety. Each step can read from and modify the shared state.
//
// Additional types support data transformation:
//
//   - [Extract] produces a value from the state
//
//   - [Transform] processes an input into an output
//
//   - [Consume] processes a value without producing an output
//
// The [Pipeline] function combines an [Extract], [Transform], and [Consume]
// into a complete [Step].
//
//	type Extract[T any, U any] = func(context.Context, T) (U, error)
//	type Transform[T any, In any, Out any] = func(context.Context, T, In) (Out, error)
//	type Consume[T any, U any] = func(context.Context, T, U) error
//
//	func Pipeline[T, A, B any](
//		extract Extract[T, A],
//		transform Transform[T, A, B],
//		consume Consume[T, B],
//	) Step[T]
//
// # Parallel Execution
//
// InParallel runs multiple workflows concurrently. This example shows deploying
// multiple services in parallel:
//
//	type DeploymentConfig struct {
//	    Services []string
//	    Region   string
//	}
//
//	deployService := func(name string) flow.Step[*DeploymentConfig] {
//	    return func(ctx context.Context, cfg *DeploymentConfig) error {
//	        return deploy(ctx, name, cfg.Region)
//	    }
//	}
//
//	workflow := flow.InParallel(
//	    flow.Steps(
//	        deployService("web"),
//	        deployService("api"),
//	        deployService("worker"),
//	    ),
//	)
//
//	cfg := &DeploymentConfig{Region: "us-west-2"}
//	if err := workflow(context.Background(), cfg); err != nil {
//	    log.Fatal(err)
//	}
//
// # Error Handling
//
// Flow provides several error handling strategies:
//
// Retry with exponential backoff:
//
//	type Config struct{ APIEndpoint string }
//
//	fetchData := func(ctx context.Context, cfg *Config) error {
//	    return callAPI(ctx, cfg.APIEndpoint)
//	}
//
//	workflow := flow.Retry(
//	    fetchData,
//	    flow.UpTo(3),
//	    flow.ExponentialBackoff(100*time.Millisecond),
//	)
//
//	cfg := &Config{APIEndpoint: "https://api.example.com"}
//	if err := workflow(context.Background(), cfg); err != nil {
//	    log.Fatal(err)
//	}
//
// Best-effort execution with IgnoreError:
//
//	workflow := flow.Do(
//	    criticalStep,
//	    flow.IgnoreError(sendMetrics), // Continue even if metrics fail
//	    finalStep,
//	)
//
// Fallback logic with OnError:
//
//	workflow := flow.OnError(
//	    tryPrimaryDatabase,
//	    useReadReplica, // Fallback if primary fails
//	)
//
// # Debugging with Traces
//
// Flow provides opt-in execution tracing to help debug workflows, understand
// performance bottlenecks, and visualize execution paths. [Traced] wraps a
// workflow and records timing and errors for all [Named] steps:
//
//	type Config struct{ DBPath string }
//
//	workflow := flow.Do(
//	    flow.Named("validate", ValidateConfig),
//	    flow.Named("connect", ConnectDB),
//	    flow.Named("migrate", RunMigrations),
//	)
//
//	// Trace the workflow and output results
//	err := flow.Spawn(
//	    flow.Traced(workflow),
//	    flow.WriteTextTo(os.Stderr),
//	)(ctx, cfg)
//
// This produces output like:
//
//	validate (45ms)
//	connect (120ms)
//	migrate (2.3s)
//
// Traces can be filtered to focus on specific issues:
//
//	trace, err := flow.Traced(workflow)(ctx, cfg)
//	slowSteps := trace.Filter(flow.MinDuration(time.Second))
//	slowSteps.WriteText(os.Stderr)
//
// Tracing supports JSON output for integration with analysis tools:
//
//	trace.WriteTo(os.Stdout) // JSON format
//
// All tracing is opt-in and has minimal overhead when not used. Only [Named]
// steps are traced—unnamed steps are ignored. See [Traced], [Named], and
// the examples/tracing/ directory for more details.
//
// # Naming Best Practices
//
// Use [Named] or [AutoNamed] liberally throughout your workflow hierarchy.
// Step names optimize how the library manages context values internally. Workflows
// that use [WithLogging], [WithSlogging], or [Traced] without corresponding [Named]
// steps may incur additional context lookup cost.
//
// # Configuration-Driven Workflows
//
// Flow excels at configuration-driven orchestration where different components
// declare their setup needs, and the orchestration layer gathers and optimizes
// execution. See the examples/config-driven/ directory for a complete example.
//
// # Additional Resources
//
// For more information, see:
//   - Usage guide: https://github.com/sam-fredrickson/flow/tree/main/docs/guide.md
//   - Pattern comparisons: https://github.com/sam-fredrickson/flow/tree/main/docs/patterns.md
//   - Design philosophy: https://github.com/sam-fredrickson/flow/tree/main/docs/design.md
//   - Runnable examples: https://github.com/sam-fredrickson/flow/tree/main/examples
//
// # Requirements
//
// Flow requires Go 1.24 or later and has minimal external dependencies.
package flow
