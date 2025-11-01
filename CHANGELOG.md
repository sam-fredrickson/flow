# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Versioning and Stability

During the **v0.x series**, breaking API changes may occur between minor versions. This is normal for pre-v1.0 libraries as we gather real-world usage feedback.

- **v0.x.y**: Pre-release versions. Minor version bumps may include breaking changes.
- **v1.0.0**: Planned once the API has stabilized through real-world usage.

---

## [Unreleased]

### Added
- (None yet - features will be added in future releases)

### Changed
- (None yet)

### Deprecated
- (None yet)

### Removed
- (None yet)

### Fixed
- (None yet)

### Security
- (None yet)

---

## [0.2.0] - 2025-11-01

### Added

#### Step Construction and Composition
- `Value()` combinator for creating Extracts that return constant values

#### Execution Tracing
- `Traced()` function to wrap workflows and record execution events
- `Trace` result type containing events, timing, and error information
- `TraceEvent` type capturing step names, execution time, and errors
- `WithStreamTo()` option for real-time event streaming as JSON Lines
- Output methods: `WriteTo()` (JSON), `WriteText()` (tree view), `WriteFlatText()` (flat list)
- Step constructors for composability: `WriteJSONTo()`, `WriteTextTo()`, `WriteFlatTextTo()`
- `Filter()` method for querying traces with multiple filter types
- `FindEvent()` method for locating specific events
- 10+ filter functions: `MinDuration()`, `MaxDuration()`, `HasError()`, `NoError()`, `NameMatches()`, `NamePrefix()`, `PathMatches()`, `HasPathPrefix()`, `DepthEquals()`, `DepthAtMost()`, `TimeRange()`, `ErrorMatches()`
- `NamedError` type for error wrapping with step context
- Thread-safe concurrent tracing in parallel workflows
- Context accessor `Names()` for retrieving hierarchical step names

### Changed
- **BREAKING**: `StepNames()` renamed to `Names()` for consistency with naming decorator patterns
- `Named`, `NamedExtract`, `NamedTransform`, `NamedConsume`, and `AutoNamed*` variants now maintain names in unified context structure
- Unified `flowCtx` architecture consolidating trace, names, and loggers into single efficient context value

---

## [0.1.0] - 2025-10-27

### Added

#### Core Workflow Orchestration
- `Step[T]` type for building reusable workflow steps
- `Do()` function for sequential step execution
- `InSerial()` and `InSerialWith()` for explicit sequential composition
- `InParallel()` and `InParallelWith()` for concurrent step execution
- Support for unlimited parallelism with automatic goroutine management

#### Step Construction and Composition
- `Extract[T, U]` type for extracting values from context
- `Transform[T, In, Out]` type for transforming input to output
- `Consume[T, U]` type for consuming steps without producing output
- `Pipeline()` for type-safe multi-function composition
- `With()` for step composition with type safety
- `From()` for creating steps from Go functions
- `Chain()`, `Chain3()`, `Chain4()` for multi-step chains
- `Feed()` for feeding values through steps
- `Spawn()` for creating named, concurrent step groups

#### Conditional Execution
- `When()` for conditional step execution
- `Unless()` for negated conditionals
- `While()` for repeating steps while conditions hold
- Predicate functions: `Not()`, `And()`, `Or()` for combining conditions

#### Collection Operations
- `Collect()` for reducing collections through steps
- `Render()` for applying steps to all items in a collection
- `Apply()` for mapping steps across collections
- `Map()` for type-safe transformation of slices
- `ForEach()` for iterating with side effects
- `Flatten()` for flattening nested collections

#### Retry Logic
- `Retry()` for automatic retry with exponential backoff
- Backoff strategies: `FixedBackoff()`, `ExponentialBackoff()`
- `BackoffOption`: `WithMaxRetries()`, `WithInitialDelay()`, `WithMaxDelay()`, `WithJitter()`
- `OnlyIf()` for conditional retries

#### Error Handling
- `IgnoreError()` for best-effort execution
- `RecoverPanics()` for defensive panic recovery
- `OnError()` for fallback step execution
- `RecoveredPanic` type for inspecting panic details
- `IndexedError` for pinpointing errors in collections
- `ErrExhausted` sentinel error for retry exhaustion

#### Context and Cancellation
- Full context.Context support in all operations
- `WithTimeout()` for deadline-based execution
- `WithDeadline()` for absolute deadline enforcement
- `Sleep()` for context-aware delays

#### Logging and Debugging
- `WithLogging()` for structured logging with slog/log compatible interfaces
- `WithSlogging()` for structured logging with slog
- `Logger()` and `Slogger()` for extracting loggers from context
- `WithLogger()` and `WithSlogger()` for injecting loggers
- `Named()` for adding error context prefixes
- `AutoNamed()` for reflection-based step naming
- `SkipCaller()` for controlling call stack depth in logging
- `StepNames()` for introspecting step names in error chains

#### Testing Infrastructure
- 96% test coverage across all functionality
- Thread safety testing and verification
- Context cancellation testing
- Concurrent execution testing
- Panic recovery testing

### Documentation

- Comprehensive README with quick start
- Design philosophy in `docs/design.md`
- Usage guide with patterns in `docs/guide.md`
- Pattern comparisons in `docs/patterns.md`
- Three runnable examples:
    - Static pattern: `examples/static/` (database provisioning)
    - Config-driven pattern: `examples/config-driven/` (environment setup)
    - Data pipeline pattern: `examples/data-pipeline/` (collection processing)
- Benchmark analysis in `bench/`

### Development Infrastructure

- CI/CD workflow with lint, test, and coverage checks
- Test coverage enforcement (>90% minimum)
- Benchmark suite for performance tracking
- golangci-lint configuration with 16 linters
- just-based command runner for common tasks
- Support for Go 1.24 and 1.25+

---

## Future Roadmap (v0.2.0 and beyond)

The following are under consideration for future releases:

- Method adapter for common Go patterns
- Additional looping constructs (`Until`, `Repeat`, `Forever`)
- FirstSuccess race pattern
- Parallel collection operators
- Enhanced error context and formatting
- Thread-safety documentation enhancements

Note: v0.x releases may include breaking changes as the API stabilizes based on real-world usage.

---

## Security

For security vulnerabilities, please contact samfredrickson@gmail.com privately rather than using the public issue tracker.

---

[Unreleased]: https://github.com/sam-fredrickson/flow/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/sam-fredrickson/flow/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/sam-fredrickson/flow/releases/tag/v0.1.0
