# Contributing

Thanks for contributing to AgentBridge.

## Prerequisites

- [Bun](https://bun.sh)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) v2.1.80+
- [Codex CLI](https://github.com/openai/codex)

## Setup

```bash
bun install
```

If you want to run the full local workflow, copy `.mcp.json.example` into your Claude MCP configuration and replace the absolute path with your local checkout path.

## Development Workflow

1. Create a focused branch for one change.
2. Make the smallest coherent change that solves the problem.
3. Update documentation when behavior, setup, or limitations change.
4. Run validation locally before opening a pull request.

## Validation

Run these commands before submitting a PR:

```bash
bun run typecheck
bun test src
```

If your change affects the local bridge flow, add manual reproduction steps in the PR description.

## Pull Requests

- Keep PRs small and scoped to one problem.
- Explain the user-visible change and the reason for it.
- Include validation results from `bun run typecheck` and `bun test src`.
- Link related issues when applicable.
- Update `README.md` and `README.zh-CN.md` together when setup or usage changes.

## Code Style

- Use TypeScript with strict typing.
- Prefer small, explicit functions over broad refactors.
- Preserve the current architecture unless the PR is intentionally structural.
- Avoid committing local machine config, secrets, logs, or generated noise.
- Keep comments short and only where they add real context.
