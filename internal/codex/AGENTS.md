# AGENTS.md

This file applies to `internal/codex/`.

## What this folder is
- This folder is the Codex runtime adapter.
- It starts Codex, keeps the app-server WebSocket alive, tracks turns, and proxies the Codex TUI.

## When editing here
- Keep Codex-specific lifecycle and translation logic here.
- Read the tests before changing proxy or turn behavior.
- Be extra careful with reconnect, approval, and ID-rewrite behavior.

## Keep these rules true
- Only the current TUI connection should receive live upstream traffic.
- Server approval requests must not be lost or double-responded.
- Turn state must stay correct even when turns overlap.
- Finished agent messages should prefer authoritative completed content over partial fragments.
