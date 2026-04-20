# AGENTS.md

This file applies to `internal/daemon/`.

## What this folder is
- This folder owns the background daemon and its lifecycle.
- It coordinates Codex, the TUI proxy, the control server, idle shutdown, status files, and killed-sentinel behavior.

## When editing here
- Read `daemon.go` and `lifecycle.go` together before changing behavior.
- Keep lifecycle logic boring and safe: start, reuse, stop, recover, and report state clearly.

## Keep these rules true
- Claude replies must be rejected until the system is truly ready.
- If `requireReply` is active, the daemon must force-forward the next Codex reply and warn when none arrives.
- Idle shutdown should only happen when both Claude and the TUI are gone.
- PID, lock, status, and killed-sentinel files must stay coordinated.
