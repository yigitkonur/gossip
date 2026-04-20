# AGENTS.md

This file applies to the whole repository unless a deeper `AGENTS.md` overrides it.

## What this project is
- Gossip is a local bridge between **Claude Code** and **Codex**.
- The **Go code is the real implementation now**.
- The `ts-legacy/` tree is an archived reference implementation.

## How to work here
- Start with `README.md` for the big picture.
- Then read the nearest deeper `AGENTS.md` for the folder you plan to change.
- Use `rtk` in shell commands because this repo expects it.
- Prefer small, focused changes that respect the current package boundaries.

## Repo-wide coding rules
- Keep CLI glue in `cmd/` and real behavior in `internal/`.
- Keep protocol shapes in `internal/protocol/` and generic JSON-RPC mechanics in `internal/jsonrpc/`.
- Treat `schema/` as a vendored snapshot, not a casual editing area.
- Treat `ts-legacy/` as reference-only unless a human explicitly asks for legacy edits.

## Verification
- Run the most targeted test you can first.
- Before calling work done, run the full Go checks:
  - `rtk go test ./...`
  - `rtk go vet ./...`
  - `rtk go build ./...`
- If the change affects the release path, also run the GoReleaser snapshot build.

## Beginner mental model
- `cmd/gossip` = the commands a human runs.
- `internal/codex` = talks to Codex and its TUI.
- `internal/mcp` = talks to Claude Code.
- `internal/control` + `internal/daemon` = keep the background bridge alive and coordinated.
