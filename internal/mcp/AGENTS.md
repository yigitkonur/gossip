# AGENTS.md

This file applies to `internal/mcp/`.

## What this folder is
- This folder is the Claude-facing MCP server.
- It speaks line-delimited JSON-RPC over stdio and exposes the `consult_codex` and `get_messages` tools.

## When editing here
- Keep this package about MCP, tools, and message delivery.
- Do not move Codex subprocess or proxy logic here.

## Keep these rules true
- `initialize` must advertise the `claude/channel` experimental capability.
- Push mode should reuse one session-scoped `chat_id`.
- Pull mode must keep bounded queues and report dropped messages honestly.
