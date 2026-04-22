# AGENTS.md

This file applies to `plugins/gossip/`.

## What this folder is
- This is the current plugin bundle for the Go rewrite.
- It connects Claude Code to `gossip claude`.

## When editing here
- Keep this folder minimal and metadata-driven.
- The real runtime behavior should stay in Go code under `cmd/` and `internal/`.

## Keep these rules true
- Plugin config should point at the current Go binary.
- Do not copy archived plugin complexity back into this folder.

## Script quirks

### `scripts/health-check.sh`
- The script reads JSON from stdin (`INPUT="$(cat 2>/dev/null || true)"`) but never references `$INPUT` — it exists to drain the pipe Claude Code's hook machinery passes in. Removing that line will leave the hook writer blocked.
- Shellcheck will emit SC2034 (`INPUT appears unused`). That's expected; do not silence it by deleting the read. If you need to quiet shellcheck for this file specifically, use `# shellcheck disable=SC2034` above the assignment — don't broaden the repo-level shellcheck config.
