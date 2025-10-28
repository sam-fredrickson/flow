# Contributing to Flow

Thank you for your interest in contributing to Flow! This document provides guidelines and information for contributing.

## Code of Conduct

We are committed to providing a welcoming and inspiring community for all. Please be respectful and constructive in all interactions.

## Reporting Issues

Before opening an issue, please:
1. Check existing issues to avoid duplicates
2. Provide a minimal reproducible example
3. Include your Go version (`go version`)
4. Describe the expected vs. actual behavior

### Security Issues

If you discover a security vulnerability, please email samfredrickson@gmail.com instead of using the issue tracker. Do not disclose the issue publicly until it has been addressed.

## Development Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/sam-fredrickson/flow.git
   cd flow
   ```

2. **Install dependencies:**
   ```bash
   go mod download
   ```

3. **Install tooling:**
   ```bash
   # Install just command runner (https://github.com/casey/just)
   # macOS: brew install just
   # Linux: curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to ~/.local/bin
   # Windows: choco install just
   ```

## Development Workflow

### Running Tests

```bash
just test          # Run all tests
just test-cover    # Run tests with coverage report
just view-coverage # View HTML coverage report
```

### Linting and Formatting

```bash
just lint          # Run linters and auto-fix issues
```

### Running Benchmarks

```bash
just bench         # Run benchmarks
```

### Viewing Documentation

```bash
just doc           # Start local godoc server at http://localhost:6060
```

## Pull Request Process

1. **Create a feature branch:**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes:**
    - Write clear, focused commits
    - Keep PRs focused on a single feature or fix
    - Add tests for new functionality
    - Update documentation as needed

3. **Ensure quality:**
   ```bash
   just lint   # Fix any linting issues
   just test   # All tests must pass
   ```

4. **Submit the PR:**
    - Link any related issues
    - Describe your changes clearly
    - Explain the rationale for design choices

5. **Review Process:**
    - Address review feedback promptly
    - Ensure CI checks pass (lint, test, coverage)
    - Maintainers will merge once approved

## Code Style

This project uses the Go community standards with enforcement via `golangci-lint`. See `.golangci.yaml` for the full configuration. Key principles:

- Follow the [Go Code Review Comments](https://go.dev/doc/effective_go)
- Use clear, descriptive names for variables and functions
- Keep functions focused and single-purposed
- Add doc comments for all exported types and functions
- Include examples in doc comments where helpful

## Testing Requirements

- All public functionality must have test coverage
- Aim for >95% overall coverage (current: 96%)
- Use table-driven tests for multiple cases
- Test concurrent behavior and context cancellation
- Include both happy path and error cases

Example test structure:
```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"case1", "input1", "output1", false},
        {"case2", "input2", "output2", false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("unexpected error: %v", err)
            }
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

## Commit Messages

- Use clear, descriptive commit messages
- Start with a verb: "Add", "Fix", "Refactor", "Update", etc.
- Keep the first line under 72 characters
- Reference related issues: "Fixes #123"

Example:
```
Add retry backoff strategies

Implement exponential and fixed backoff patterns for retry logic.
Includes WithJitter option for distributed retry spacing.

Closes #456
```

## Documentation Updates

When adding features:
1. Update godoc comments (required for exported APIs)
2. Update relevant guide in `docs/`
3. Add an example if it's a new pattern
4. Update examples if they're affected

## Getting Help

- Check the [documentation](../README.md) and guides in `docs/`
- Review existing examples in `examples/`
- Open a discussion issue if you have questions

Thank you for contributing!
