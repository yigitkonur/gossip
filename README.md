# Gossip

Gossip lets **Claude Code** and **Codex** work together on the same machine.

In simple terms:
- **Claude Code** is the reviewer and planner.
- **Codex** is the implementer and executor.
- **Gossip** is the local messenger that keeps them connected.

The **current supported runtime is written in Go**.

## How the system works

Think of Gossip as four connected layers:

1. **Claude bridge**
   - Runs in the foreground when Claude Code launches `gossip claude`.
   - Speaks MCP over standard input/output.

2. **Daemon**
   - Runs in the background.
   - Keeps the system alive even if the Claude-side process goes away for a moment.

3. **Codex proxy**
   - Accepts the Codex TUI connection.
   - Rewrites IDs and keeps request/response traffic lined up correctly.

4. **Codex app-server connection**
   - Talks to the real `codex app-server` process over WebSocket.

That means the real message path is:

**Claude Code → MCP bridge → daemon → Codex proxy → Codex app-server**

and back again.

## The commands you can run

The current CLI commands are:

- `gossip init`
- `gossip claude`
- `gossip codex`
- `gossip kill`
- `gossip status`
- `gossip version`

### What each command does

#### `gossip init`
Creates a local `.gossip/` folder in your project with starter files:
- `config.json`
- `collaboration.md`

Use this first if the current project has not been initialized yet.

#### `gossip claude`
Starts the Claude-facing bridge.

Claude Code uses this command to talk to Gossip over MCP.

#### `gossip codex`
Makes sure the background daemon is running, waits for the proxy to be ready, then launches the Codex TUI attached to that proxy.

#### `gossip kill`
Stops the background daemon and writes a **killed sentinel** file.

That sentinel matters: it tells the system **not** to automatically reconnect until you intentionally start it again.

#### `gossip status`
Prints the current daemon state, such as:
- whether the bridge is ready
- whether the TUI is connected
- current thread ID
- queue counts
- daemon PID

#### `gossip version`
Prints the current build version.

## Important runtime ideas

### 1. Readiness
Claude should only send replies when Codex is actually ready.

Gossip tracks this using the TUI state machine and thread readiness. If the system is not ready, reply attempts are rejected instead of being silently lost.

### 2. Current TUI ownership
Only the **current** Codex TUI connection is allowed to receive live upstream traffic.

This prevents stale or duplicate TUI sessions from replying to the wrong request.

### 3. Buffered replay
If Claude disconnects for a moment, the daemon can buffer Codex messages and replay them when Claude reconnects.

This keeps short disconnects from losing important output.

### 4. Killed sentinel
`gossip kill` writes a sentinel file in the state directory.

That sentinel means:
- do not auto-reconnect
- do not silently restart the background flow
- wait for the user to intentionally start it again

### 5. State directory
Runtime files are stored in a shared state directory.

On macOS, that is usually:
- `~/Library/Application Support/Gossip`

This directory holds things like:
- `daemon.pid`
- `daemon.lock`
- `status.json`
- `gossip.log`
- `killed`
- `codex-tui.pid`

## Folder guide

Here is the beginner-friendly map of the repo:

- `cmd/gossip/` — the real CLI commands
- `internal/protocol/` — wire types and method names
- `internal/jsonrpc/` — generic JSON-RPC engine
- `internal/codex/` — Codex subprocess, WebSocket, proxy, turn handling
- `internal/mcp/` — Claude-facing MCP server and tools
- `internal/control/` — daemon ↔ Claude bridge WebSocket protocol
- `internal/daemon/` — background supervisor and lifecycle
- `internal/filter/` — message importance rules and status summaries
- `internal/tui/` — TUI readiness and disconnect grace logic
- `internal/statedir/` — runtime file locations
- `internal/config/` — project-local `.gossip/` config
- `schema/` — vendored Codex protocol schema snapshot
- `scripts/` — maintenance helpers for schema/protocol work
- `plugins/gossip/` — current plugin metadata/config
- `docs/` — design notes and architecture history
- `ts-legacy/` — archived reference implementation

## How to develop safely

A good beginner workflow is:

1. Read the top-level `AGENTS.md`
2. Read the deeper `AGENTS.md` in the folder you want to change
3. Make the smallest change that solves the problem
4. Run targeted tests first
5. Then run the full checks:

```bash
rtk go test ./...
rtk go vet ./...
rtk go build ./...
```

If your change touches packaging or release behavior, also run:

```bash
rtk $(go env GOPATH)/bin/goreleaser build --snapshot --clean --single-target
```

## What is current vs legacy

### Current
- Go CLI in `cmd/gossip/`
- Go runtime in `internal/`
- Go CI in `.github/workflows/ci.yml`
- current plugin metadata in `plugins/gossip/`

### Legacy reference
- `ts-legacy/`
- archived reference scripts
- archived plugin layout in `ts-legacy/plugins/`
- older design docs that still describe the pre-Go architecture

## If you are new to this repo

Start in this order:

1. `README.md`
2. `AGENTS.md`
3. `cmd/gossip/AGENTS.md`
4. `internal/daemon/AGENTS.md`
5. `internal/codex/AGENTS.md`
6. `internal/mcp/AGENTS.md`

That gives you the fastest path to understanding the system from the outside in.
