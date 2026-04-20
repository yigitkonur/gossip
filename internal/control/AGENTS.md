# AGENTS.md

This file applies to `internal/control/`.

## What this folder is
- This folder carries messages between the foreground Claude bridge and the background daemon.
- It includes the daemon-side WebSocket server and the reconnecting bridge-side client.

## When editing here
- Think in terms of attach, detach, buffering, and replay.
- Keep the wire protocol small and explicit.

## Keep these rules true
- Only one Claude bridge session may be attached at a time.
- Detached bridge periods must not silently lose buffered Codex output.
- Status snapshots should reflect the daemon honestly.
