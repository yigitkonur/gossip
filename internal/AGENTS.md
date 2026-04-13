# AGENTS.md

This file applies to `internal/`.

## What this folder is
- This is the main Go implementation.
- Each subfolder should have one clear job and a clean boundary.

## When editing here
- Start by reading the nearest deeper `AGENTS.md` before changing code.
- Keep package responsibilities narrow.
- Avoid moving logic across package boundaries unless the boundary is clearly wrong.

## Keep these rules true
- `cmd/` is for CLI glue; `internal/` is for real behavior.
- Cross-package behavior should stay explicit and testable.
