# Contributing to Secondary Scheduler Operator

## Getting Started

This operator deploys custom Kubernetes schedulers built with the [scheduler plugin framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/) on OpenShift clusters.

**New contributors**: Start by reading [AGENTS.md](./AGENTS.md) for a comprehensive overview of the repository structure, architecture, and development patterns.

## Development Setup

### Prerequisites

- Go 1.24+ (check `go.mod` for current version)
- Access to an OpenShift cluster (for testing)
- `oc` CLI tool
- `podman` or `docker` (for building images)

### Build and Run Locally

```bash
# Build the operator binary
make build

# Install CRD and sample resources to your cluster
make install-local

# Run the operator locally (points to cluster via KUBECONFIG)
make run-local
```

### Running Tests

```bash
# Run unit tests
make test-unit

# Run E2E tests (requires OpenShift cluster)
make test-e2e

# Run verification checks (gofmt, dependencies, etc.)
make verify
```

## Making Changes

### Code Generation

If you modify API types in `pkg/apis/secondaryscheduler/v1/types.go`:

```bash
# Regenerate clientsets, informers, and listers
make generate-clients
```

### Development Workflow

1. **Create a branch** from `main`
2. **Make your changes**
3. **Run tests**: `make test-unit && make verify`
4. **Test locally**: `make build && make run-local`
5. **Commit your changes** with a clear commit message
6. **Push and open a Pull Request**

### Commit Message Guidelines

Follow conventional commit style:

```
<type>: <short summary>

<optional body with details>
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`

Example:
```
feat: add nodeSelector support for HA topology

Allow users to target specific nodes for secondary scheduler
replicas in HighlyAvailable mode by specifying nodeSelector
in the topology configuration.

Fixes #123
```

## What NOT to Do

- **Do not edit generated files** (`zz_generated.*`, files under `pkg/generated/`)
- **Do not edit vendored files** (managed by `go mod`)
- **Do not modify OWNERS or OWNERS_ALIASES** without approval
- **Do not skip `make verify`** - CI will reject PRs that fail verification

## Pull Request Process

1. **Ensure all tests pass** locally before opening a PR
2. **Update documentation** if you're adding features or changing behavior
3. **Keep PRs focused** - one feature or fix per PR
4. **Respond to review feedback** promptly

### PR Checklist

- [ ] Tests pass (`make test-unit && make verify`)
- [ ] Code follows existing patterns and conventions
- [ ] Documentation updated (README.md, AGENTS.md, or comments)
- [ ] Commit messages are clear and follow guidelines
- [ ] PR description explains what and why, not just what

## Code Style

- Follow standard Go conventions (effective Go, Go proverbs)
- Run `gofmt` (enforced by `make verify`)
- Keep functions focused and testable
- Add comments for non-obvious logic (why, not what)

## Testing Guidelines

- **Unit tests**: Test individual functions and reconciliation logic
- **E2E tests**: Test full operator lifecycle on OpenShift
  - Use the OTE framework (OpenShift Tests Extension)
  - Tests must be idempotent and clean up resources
  - Mark disruptive tests with `[Serial]` tag

## Architecture and Technical Details

For deeper understanding of the operator's architecture:

- [AGENTS.md](./AGENTS.md) - Repository structure, build workflow, patterns
- [ARCHITECTURE.md](./ARCHITECTURE.md) - Controllers, reconciliation flow, design decisions
- [README.md](./README.md) - User-facing documentation and deployment

## Getting Help

- **Questions about contributing?** Open a discussion or issue
- **Found a bug?** Open an issue with reproduction steps
- **Have a feature idea?** Open an issue to discuss before implementing
- **Need architecture clarification?** Check ARCHITECTURE.md or ask in an issue

## Community Guidelines

- Be respectful and constructive in all interactions
- Assume good intent from other contributors
- Help newcomers get started
- Review others' PRs when you can

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0 (see [LICENSE](./LICENSE)).
