# Gossip — Project Rules

## Git Workflow

- Never push directly to `master`.
- Use focused feature/fix/docs branches and merge through PRs.
- Prefer squash merge.
- Keep PRs small enough to review clearly.

## Collaboration

- Do not send replies while Codex is in an active turn; the busy guard will reject them.
- Prefer a fresh Codex session over resume when the TUI is unstable.
- Use `gossip codex` to attach the Codex TUI.
- When testing a PR, test from that PR's actual branch/worktree.

## Development

- The current runtime is Go.
- Keep CLI glue in `cmd/gossip/` and real behavior in `internal/`.
- Before merging, run the current Go checks:
  - `go test ./...`
  - `go vet ./...`
  - `go build ./...`
  - `make check`

## Progress notes

- Keep local scratch notes out of git.
- Update docs when command names, setup, or runtime behavior change.
