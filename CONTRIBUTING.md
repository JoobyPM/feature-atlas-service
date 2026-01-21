# Contributing to feature-atlas-service

Thank you for your interest in contributing! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Development Setup](#development-setup)
- [Code Style](#code-style)
- [Git Workflow](#git-workflow)
- [Commit Messages](#commit-messages)
- [Pull Request Process](#pull-request-process)
- [Testing](#testing)

## Development Setup

### Prerequisites

- Go 1.22+ (1.25.6 recommended)
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2.8.0+
- Docker and Docker Compose (for containerized development)
- OpenSSL (for certificate generation)

### Quick Start

```bash
# Clone the repository
git clone https://github.com/JoobyPM/feature-atlas-service.git
cd feature-atlas-service

# Install dependencies
make deps

# Generate certificates for local development
make certs

# Run the service
make run
```

### Available Commands

```bash
make help          # Show all available commands
make build         # Build the service binary
make build-cli     # Build the CLI binary
make fmt           # Format code (gofumpt + gci)
make lint          # Run linter (golangci-lint)
make test          # Run tests
make test-cover    # Run tests with coverage report
```

## Code Style

We use [golangci-lint v2](https://golangci-lint.run/) with a comprehensive configuration. The linter configuration is in `.golangci.yml`.

### Formatting

Code formatting is enforced using:

- **gofumpt**: Stricter version of gofmt
- **gci**: Import grouping and ordering

Always run formatting before committing:

```bash
make fmt
```

### Import Order

Imports should be grouped in this order:

1. Standard library
2. External packages
3. Organization packages (`github.com/JoobyPM/*`)
4. Local module packages

Example:

```go
import (
    "context"
    "fmt"
    "net/http"

    "github.com/charmbracelet/bubbletea"
    "github.com/spf13/cobra"

    "github.com/JoobyPM/feature-atlas-service/internal/store"
)
```

### Linting

Before submitting a PR, ensure your code passes all linters:

```bash
make lint
```

Key linting rules:

- Use `any` instead of `interface{}`
- Handle all errors explicitly
- Use `errors.Is/As` for error comparison
- Pass context through call chains
- Use `http.MethodGet` instead of `"GET"`

## Git Workflow

### Branching Strategy

We follow GitFlow:

- `main` - Production-ready code
- `develop` - Integration branch (if used)
- `feat/<description>` - New features
- `fix/<description>` - Bug fixes
- `chore/<description>` - Maintenance tasks
- `docs/<description>` - Documentation updates

### Creating a Branch

```bash
# For a new feature
git checkout main
git pull origin main
git checkout -b feat/my-new-feature

# For a bug fix
git checkout -b fix/issue-description
```

## Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/).

### Format

```text
<type>(<scope>): <description>

[optional body]

[optional footer]
```

### Types

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix (in released code only) |
| `chore` | Maintenance tasks, including fixes during development |
| `docs` | Documentation changes |
| `style` | Formatting, whitespace (no code logic change) |
| `refactor` | Code restructuring (no behavior change) |
| `test` | Adding or updating tests |
| `perf` | Performance improvements |
| `ci` | CI/CD changes |
| `build` | Build system changes |

### Examples

```bash
# Feature
git commit -m "feat(api): add feature search endpoint

Implement LIKE search across feature ID, name, and summary fields.
Returns paginated results with configurable limit."

# Bug fix (released code)
git commit -m "fix(tls): handle expired client certificates gracefully"

# Development fix
git commit -m "chore(fix): correct middleware ordering in handler chain"

# Documentation
git commit -m "docs: add API usage examples to README"
```

### Atomic Commits

Each commit should:

- Be self-contained and pass all checks
- Focus on a single logical change
- Not break the build
- Include relevant tests if applicable

## Pull Request Process

### Before Submitting

1. **Ensure code quality**:
   ```bash
   make fmt
   make lint
   make test
   ```

2. **Update documentation** if your change affects:
   - API endpoints
   - Configuration options
   - Setup instructions

3. **Write meaningful commit messages** following our conventions

### PR Description Template

```markdown
## Description
Brief description of the changes.

## Type of Change
- [ ] New feature
- [ ] Bug fix
- [ ] Documentation update
- [ ] Refactoring
- [ ] Other (describe):

## Testing
Describe how you tested your changes.

## Checklist
- [ ] Code passes `make lint`
- [ ] Tests pass `make test`
- [ ] Documentation updated (if applicable)
- [ ] Commit messages follow conventions
```

### Review Process

1. Create a PR against `main` (or `develop` if used)
2. Ensure CI checks pass
3. Request review from maintainers
4. Address feedback
5. Squash commits if requested
6. Merge once approved

## Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests with race detection (default)
make test

# Run tests with coverage
make test-cover
```

### Writing Tests

- Place test files alongside the code they test (`*_test.go`)
- Use table-driven tests for multiple scenarios
- Name tests descriptively: `Test<Function>_<Scenario>_<ExpectedBehavior>`

Example:

```go
func TestSearchFeatures_EmptyQuery_ReturnsAllFeatures(t *testing.T) {
    // ...
}

func TestSearchFeatures_WithQuery_FiltersResults(t *testing.T) {
    // ...
}
```

### Integration Tests

For mTLS testing, use the generated certificates:

```bash
# Generate test certificates
make certs

# Run the service
make run

# In another terminal, test endpoints
make test-me-admin
make register-alice
make test-me-alice
```

## Questions?

If you have questions, please:

1. Check existing issues and documentation
2. Open a new issue with your question
3. Tag it with the `question` label

Thank you for contributing! 🎉
