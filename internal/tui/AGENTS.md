# AGENTS.md

This file applies to `internal/tui/`.

## What this folder is
- This folder tracks the Codex TUI connection state.
- It answers the important question: “Is it safe for Claude to send a reply right now?”

## When editing here
- Keep this package focused on state transitions and grace windows.
- Do not add daemon orchestration or filtering logic here.

## Keep these rules true
- Short disconnects should not look like real failures.
- `CanReply()` should only become true when the bridge is ready and the TUI is usable.
- Reconnect and persistent-disconnect callbacks should stay predictable.
