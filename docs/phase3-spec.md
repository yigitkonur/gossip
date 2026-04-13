# Gossip Phase 3 Final State

## Status

Phase 3 is complete in the current Go codebase.

This document records the delivered Go runtime surface.

## Delivered surface

Phase 3 established Gossip as a CLI-first local product with:

- `gossip init`
- `gossip claude`
- `gossip codex`
- `gossip kill`
- `gossip status`
- `gossip version`
- project-local `.gossip/` config files
- a shared machine-local state directory through `internal/statedir`
- a background daemon coordinated through `internal/daemon`
- a Claude-facing MCP server in `internal/mcp`
- a Codex adapter and TUI proxy in `internal/codex`

## Runtime architecture

### Foreground Claude process

The foreground Claude entrypoint lives under `cmd/gossip/` and starts the MCP server used by Claude Code. It:

- serves MCP over stdio
- ensures the background daemon is running
- connects to the control WebSocket
- forwards Codex messages to Claude and Claude replies back to Codex

### Background daemon

The background daemon owns the long-lived coordination state. It:

- supervises the Codex app-server process
- exposes health, readiness, and control endpoints on localhost
- buffers messages while the foreground side is detached
- tracks TUI connectivity and turn readiness

### Shared helpers

- `internal/config` manages `.gossip/config.json` and `.gossip/collaboration.md`
- `internal/statedir` resolves the machine-local runtime state path
- `internal/control` defines the foreground/daemon WebSocket protocol
- `internal/protocol` defines shared wire types

## User commands

### `gossip init`
Creates `.gossip/config.json` and `.gossip/collaboration.md` if they do not already exist.

### `gossip claude`
Runs the foreground MCP server used by Claude Code.

### `gossip codex`
Ensures the daemon is running, waits for the local proxy, and launches the Codex TUI attached to that proxy.

### `gossip kill`
Writes the killed sentinel and stops the background daemon.

### `gossip status`
Prints the current daemon snapshot.

### `gossip version`
Prints the current build version.

## Runtime state

Project-local config lives under `.gossip/` in the current workspace.

Machine-local runtime state defaults to:

- macOS: `~/Library/Application Support/Gossip`
- Linux: `${XDG_STATE_HOME:-~/.local/state}/gossip`
- override: `GOSSIP_STATE_DIR`

Files stored there include:

- `daemon.pid`
- `daemon.lock`
- `status.json`
- `gossip.log`
- `killed`
- `codex-tui.pid`

## Environment variables

- `GOSSIP_STATE_DIR`
- `GOSSIP_CONTROL_PORT`
- `GOSSIP_DAEMON_ENTRY`
- `GOSSIP_MODE`
- `GOSSIP_MAX_BUFFERED_MESSAGES`
- `GOSSIP_FILTER_MODE`
- `GOSSIP_IDLE_SHUTDOWN_MS`
- `GOSSIP_ATTENTION_WINDOW_MS`
- `CODEX_PROXY_PORT`
