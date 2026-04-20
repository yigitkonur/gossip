# Contributing

Thanks for contributing to Gossip.

## Prerequisites

- Go 1.23+
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
- [Codex CLI](https://github.com/openai/codex)

## Setup

```bash
go mod tidy
go build ./cmd/gossip
```

If you want the CLI on your PATH during local development:

```bash
go install ./cmd/gossip
```

## Development Workflow

1. Create a focused branch for one change.
2. Make the smallest coherent change that solves the problem.
3. Update documentation when setup, behavior, or limitations change.
4. Run validation locally before opening a pull request.
5. Use squash merge when landing the PR.

## Validation

Run these commands before submitting a PR:

```bash
go test ./...
go vet ./...
go build ./...
make check
```

If your change affects the local Claude/Codex flow, include manual reproduction steps in the PR description.

## Testing

- Unit and integration tests live beside the Go packages they verify.
- Use `t.TempDir()` for temporary filesystem state.
- Prefer focused tests first, then run the full repository checks.

## Pull Requests

- Keep PRs scoped to one problem.
- Never push directly to `master`.
- Explain the user-visible change and why it matters.
- Include the validation commands you ran.
- Update `README.md` and `README.zh-CN.md` together when setup or usage changes.

## Code Style

- Keep command handlers thin.
- Keep package boundaries explicit.
- Prefer small, readable functions over broad refactors.
- Avoid committing local machine config, secrets, logs, or generated noise.
