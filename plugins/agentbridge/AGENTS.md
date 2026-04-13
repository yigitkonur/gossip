# AGENTS.md

This file applies to `plugins/agentbridge/`.

## What this folder is
- This is the current plugin bundle for the Go rewrite.
- It connects Claude Code to `agentbridge claude`.

## When editing here
- Keep this folder minimal and metadata-driven.
- The real runtime behavior should stay in Go code under `cmd/` and `internal/`.

## Keep these rules true
- Plugin config should point at the current Go binary.
- Do not copy old TypeScript plugin complexity back into this folder.
