# AGENTS.md

This file applies to `internal/jsonrpc/`.

## What this folder is
- This folder is the generic JSON-RPC engine used by the rest of the system.
- It knows how to send calls, notifications, and responses, then route replies back correctly.

## When editing here
- Keep this package transport-level and generic.
- Codex-specific behavior belongs in `internal/codex/`.
- MCP-specific behavior belongs in `internal/mcp/`.

## Keep these rules true
- Bridge-originated `Call` IDs must stay in their own safe range.
- Response matching must stay stable across valid JSON-RPC ID formats.
- Writers must remain safe for concurrent use.
