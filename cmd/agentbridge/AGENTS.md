# AGENTS.md

This file applies to `cmd/agentbridge/`.

## What this folder is
- This folder contains the real user-facing Go CLI.
- It is the front door into the system: `init`, `claude`, `codex`, `kill`, `status`, and `version`.

## When editing here
- Keep command files as thin glue.
- Put heavy logic in `internal/` packages, not in Cobra command handlers.
- Make help text simple enough for a first-time user.

## Keep these rules true
- Commands should describe what they do clearly.
- Hidden/internal commands should stay hidden unless a human asks otherwise.
- User-facing errors should be readable and actionable.
