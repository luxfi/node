# Contributing to Lux Node

Thank you for your interest in contributing to Lux Node! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Process](#development-process)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [Security](#security)

## Code of Conduct

We are committed to fostering a welcoming and inclusive community. Please be respectful and considerate in all interactions.

### Our Standards

- Use welcoming and inclusive language
- Be respectful of differing viewpoints and experiences
- Gracefully accept constructive criticism
- Focus on what is best for the community
- Show empathy towards other community members

## Getting Started

### Prerequisites

- Go 1.21.12 or higher
- Git
- Make
- GCC/G++ compiler

### Setting Up Your Development Environment

1. **Fork the repository**
   ```bash
   # Visit https://github.com/luxfi/node and click "Fork"
   ```

2. **Clone your fork**
   ```bash
   git clone https://github.com/YOUR_USERNAME/node.git
   cd node
   ```

3. **Add upstream remote**
   ```bash
   git remote add upstream https://github.com/luxfi/node.git
   ```

4. **Install dependencies**
   ```bash
   go mod download
   ```

5. **Build the project**
   ```bash
   ./scripts/build.sh
   ```

6. **Run tests**
   ```bash
   go test ./...
   ```

## Development Process

### Branch Naming

Use descriptive branch names:
- `feature/add-new-api-endpoint`
- `fix/memory-leak-in-consensus`
- `docs/update-api-reference`
- `refactor/optimize-database-access`

### Commit Messages

Follow the conventional commits specification:

```
type(scope): subject

body

footer
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `test`: Test additions or changes
- `chore`: Maintenance tasks

**Examples:**
```
feat(api): add new health check endpoint

- Implement /health/ready endpoint
- Add comprehensive health checks
- Update documentation

Closes #123
```

## Pull Request Process

### Before Submitting

- [ ] Code compiles without warnings
- [ ] All tests pass
- [ ] New tests added for new functionality
- [ ] Documentation updated if needed
- [ ] Code follows project style guidelines

### PR Template

```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist
- [ ] Code follows style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
```

## Coding Standards

### Go Code Style

1. **Format code with gofmt**
   ```bash
   gofmt -w .
   ```

2. **Use golangci-lint**
   ```bash
   golangci-lint run
   ```

3. **Error Handling**
   ```go
   if err != nil {
       return fmt.Errorf("failed to process block: %w", err)
   }
   ```

## Testing Guidelines

### Test Structure

```go
func TestFunctionName(t *testing.T) {
    // Arrange
    input := createTestInput()
    expected := expectedOutput()
    
    // Act
    result, err := FunctionUnderTest(input)
    
    // Assert
    require.NoError(t, err)
    require.Equal(t, expected, result)
}
```

### Running Tests

```bash
# Unit tests
go test ./...

# With coverage
go test -cover ./...

# Benchmarks
go test -bench=. ./benchmarks/...
```

## Security

### Reporting Vulnerabilities

**DO NOT** create public issues for security vulnerabilities.

Email security@lux.network with:
- Description of the vulnerability
- Steps to reproduce
- Potential impact

## Getting Help

- [Discord Community](https://discord.gg/lux)
- [GitHub Discussions](https://github.com/luxfi/node/discussions)
- [Documentation](https://docs.lux.network)

## License

By contributing, you agree that your contributions will be licensed under the project's BSD 3-Clause License.