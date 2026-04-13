# AGENTS.md

This file applies to `internal/statedir/`.

## What this folder is
- This folder decides where Gossip stores runtime files on disk.
- It keeps PID files, lock files, logs, status files, and sentinels in one predictable place.

## When editing here
- Keep path rules simple and cross-platform.
- Do not put project config logic here.

## Keep these rules true
- Runtime artifact paths must stay under the resolved state directory.
- Platform defaults should stay predictable for beginners.
- `Ensure()` should remain enough to prepare the directory for writes.
