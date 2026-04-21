<div align="center">

```
   ____  ___   ____ ____ ___ ____
  / ___|/ _ \ / ___/ ___|_ _|  _ \
 | |  _| | | |\___ \___ \| || |_) |
 | |_| | |_| | ___) |__) | ||  __/
  \____|\___/ |____/____/___|_|
```

**Claude Code ⇄ Codex, whispering on the same machine.**

[![CI](https://github.com/yigitkonur/gossip/actions/workflows/ci.yml/badge.svg)](https://github.com/yigitkonur/gossip/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/yigitkonur/gossip)](go.mod)

</div>

---

Gossip is a small, single-binary bridge that lets **Claude Code** and **OpenAI Codex** collaborate on the same workstation — one as planner/reviewer, the other as implementer/executor — with messages flowing through a local daemon instead of you copy-pasting context.

```
┌─────────────┐   MCP stdio    ┌────────────┐   WebSocket   ┌───────────────┐   WS    ┌──────────────┐
│ Claude Code │ ◀────────────▶ │   Gossip   │ ◀────────────▶│ Codex TUI     │ ◀─────▶ │ codex app-   │
│             │                │   daemon   │               │ (via proxy)   │         │  server       │
└─────────────┘                └────────────┘               └───────────────┘         └──────────────┘
```

---

## Table of contents

- [Why Gossip exists](#why-gossip-exists)
- [Install in one line](#install-in-one-line)
- [Five-minute quick start](#five-minute-quick-start)
- [How it works](#how-it-works)
- [Commands](#commands)
- [Configuration](#configuration)
- [Environment variables](#environment-variables)
- [MCP tools exposed to Claude](#mcp-tools-exposed-to-claude)
- [Troubleshooting](#troubleshooting)
- [Building from source](#building-from-source)
- [Cross-compilation](#cross-compilation)
- [Contributing](#contributing)
- [License](#license)

---

## Why Gossip exists

Two things are true at once today:

1. **Claude Code** is great at reading large codebases, reasoning about designs, and catching regressions before you merge.
2. **Codex CLI** is great at turning concrete plans into working patches, running tests, and reproducing bugs.

Making the two agents cooperate requires a shared channel, a way to hand-off turns, and some mechanical plumbing (MCP on the Claude side, a JSON-RPC WebSocket on the Codex side). Gossip is that plumbing:

- One background daemon per machine.
- An MCP server Claude Code already knows how to talk to.
- A proxy in front of `codex app-server` so a single Codex TUI can attach, survive reconnects, and replay messages.

You run `gossip claude` in Claude Code and `gossip codex` in a second terminal — and they can talk.

## Install in one line

```bash
curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash
```

This downloads the right prebuilt binary for your OS and architecture, drops it at `/usr/local/bin/gossip`, and verifies the install by running `gossip version`. It is idempotent — re-running upgrades in place.

> **Prefer not to pipe to sudo?** Inspect the script first: `curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | less`. Or [build from source](#building-from-source).

Supported targets:

| OS      | Architectures      |
| ------- | ------------------ |
| macOS   | `arm64`, `amd64`   |
| Linux   | `amd64`, `arm64`   |

Gossip is POSIX-only: the daemon relies on Unix signals, PID files, and terminal state handling that does not exist on Windows.

## Five-minute quick start

**1. Install the gossip binary** (see above).

**2. Install Claude Code and Codex CLI.** You need both on your `PATH`. Check with:

```bash
claude --version   # requires 2.1.80 or newer
codex --version
```

**3. Initialize your project.**

```bash
cd your-project
gossip init
```

This writes `.gossip/config.json`, `.gossip/collaboration.md`, verifies your CLIs, and copies the Gossip plugin into Claude Code's plugin cache.

**4. Start Claude Code.** Either run Claude as usual and let it launch `gossip claude` via the MCP config — or start it directly:

```bash
claude  # Claude Code will spawn gossip claude in stdio mode
```

**5. Attach the Codex TUI.** In a second terminal, from the same project:

```bash
gossip codex
```

The first call starts the background daemon, waits for the proxy to be ready, then launches the Codex TUI pointed at the gossip proxy (`--remote ws://127.0.0.1:<port>`).

**6. Collaborate.** Ask Claude to brief Codex:

> "Codex, please reproduce the failing test in `internal/codex/proxy.go` and report the exact stack trace."

Codex receives the message, runs the work, replies. Claude sees the reply via the `get_messages` tool (pull mode) or an inline `<channel>` tag (push mode).

## How it works

Gossip is four layers wearing one binary:

1. **Claude bridge** (`gossip claude`)
   Foreground stdio process. Speaks [MCP](https://modelcontextprotocol.io/) to Claude Code, forwards traffic over a WebSocket to the daemon. Claude sees gossip as "just another MCP server" that exposes `reply` and `get_messages` tools.

2. **Daemon** (`gossip daemon`, auto-started)
   Long-lived background supervisor. Owns the Codex subprocess lifecycle, buffers messages during brief Claude disconnects, and multiplexes one Codex TUI at a time. Survives Claude Code restarts and session switches.

3. **Codex proxy**
   A local WebSocket endpoint that the Codex TUI connects to (`codex --remote`). Rewrites request IDs, replays cached `initialize` / `thread/start` results when a duplicate TUI re-initialises, and rewrites `userAgent` strings so Codex thinks it's talking to a real app-server.

4. **Codex app-server connection**
   The daemon keeps one WebSocket open to `codex app-server`. That's the upstream authority on threads, turns, and tool calls.

Message path, visualised:

```
Claude Code ─┬─▶ gossip claude (MCP stdio)
             │       │
             │       ▼
             │   gossip daemon ◀── TUI state, killed sentinel,
             │       │              message buffer, thread ID
             │       ▼
             │   gossip codex proxy ◀──── Codex TUI (ws://127.0.0.1:N)
             │       │
             │       ▼
             └── codex app-server (upstream)
```

### Key runtime rules

- **Readiness gating.** Claude can only `reply` when the TUI is attached *and* a Codex thread exists. Otherwise the daemon returns a "Codex is not ready" error instead of silently dropping the message.
- **Single current TUI.** Only the most-recent Codex TUI connection receives live upstream traffic. Stale TUIs are isolated.
- **Buffered replay.** A short Claude disconnect doesn't lose messages — the daemon buffers up to `max-buffered-messages` and replays on reconnect.
- **Killed sentinel.** `gossip kill` writes a file that prevents auto-reconnect until you explicitly restart. This is what lets you reliably park the system.
- **State directory.** Runtime files live in a per-user directory (`~/Library/Application Support/Gossip` on macOS, `~/.local/state/gossip` on Linux) — `daemon.pid`, `daemon.lock`, `status.json`, `gossip.log`, `killed`, `codex-tui.pid`.

## Commands

| Command          | What it does                                                                 |
| ---------------- | ---------------------------------------------------------------------------- |
| `gossip init`    | Scaffold `.gossip/` in the current project and install the Claude plugin.     |
| `gossip claude`  | Run the MCP bridge (stdio). Claude Code invokes this automatically.           |
| `gossip codex`   | Ensure daemon is up, wait for proxy, then launch `codex` attached to it.      |
| `gossip daemon`  | Run the background daemon in the foreground (usually auto-started).          |
| `gossip status`  | Print daemon state, bridge readiness, TUI attachment, thread ID, queue size. |
| `gossip kill`    | Stop the daemon and write the killed sentinel.                                |
| `gossip version` | Print the build version.                                                      |

Run `gossip --help` for the full Cobra-generated tree.

## Configuration

Gossip stores project-local config in `.gossip/config.json`. The Go runtime accepts both the **Go-shape** keys and the **TS-shape** keys for backwards compatibility.

```jsonc
{
  "daemon": {
    "port": 4599,       // control channel (Claude bridge ⇄ daemon)
    "proxyPort": 4600   // Codex TUI attaches here
  },
  "agents": {
    "claude": {
      "mode": "pull"    // "push" or "pull"
    }
  },
  "turnCoordination": {
    "attentionWindowSeconds": 45,
    "idleShutdownSeconds": 900
  }
}
```

The TS-shape aliases (`codex.appPort`, `codex.proxyPort`, `claude.mode`) are still read when present. Gossip writes back in the Go-shape.

### Delivery modes

- **pull** *(default)* — Claude calls the `get_messages` tool whenever it wants to check for new Codex output. Keeps your Claude context window tight.
- **push** — The daemon streams `<channel source="gossip" ...>` tags into Claude's MCP stream as soon as Codex speaks. Chattier, lower-latency.

## Environment variables

Env vars win over `.gossip/config.json`.

| Variable                         | Meaning                                         |
| -------------------------------- | ----------------------------------------------- |
| `GOSSIP_CONTROL_PORT`            | Daemon control port.                            |
| `GOSSIP_PROXY_PORT`              | Codex proxy port.                               |
| `GOSSIP_MODE`                    | Delivery mode: `push` or `pull`.                |
| `GOSSIP_MAX_BUFFERED_MESSAGES`   | Disconnect buffer size (default auto).          |
| `GOSSIP_ATTENTION_WINDOW_MS`     | Claude attention window, milliseconds.          |
| `GOSSIP_IDLE_SHUTDOWN_MS`        | Idle-shutdown timeout, milliseconds.            |
| `GOSSIP_IDLE_SHUTDOWN_SECONDS`   | Alt spelling in seconds — `0` disables.         |
| `GOSSIP_FILTER_MODE`             | Message filter mode.                            |
| `GOSSIP_STATE_DIR`               | Override the runtime state directory.           |
| `GOSSIP_LOG_LEVEL`               | `debug` / `info` / `warn` / `error`.            |

## MCP tools exposed to Claude

Two tools live on the `gossip` MCP server:

- **`reply`** — Send a message back to Codex. Required: `text`. Optional: `chat_id`, `require_reply` (bool).
- **`get_messages`** — Drain any buffered messages from Codex. Returns `[]` if nothing is pending.

Example (Claude calling `reply`):

```json
{
  "name": "reply",
  "arguments": {
    "text": "Codex, please reproduce the failing test and paste the stack trace.",
    "chat_id": "thr_abc123",
    "require_reply": true
  }
}
```

The bridge also injects a short system-instructions block describing turn coordination ("⏳ Codex is working"), collaboration roles, and thinking patterns. See `cmd/gossip/cmd_claude.go` for the exact prompt.

## Troubleshooting

**`gossip status` says "daemon: not running"** — the background daemon has exited (probably idle-shutdown). Run `gossip codex` or `gossip claude` to bring it back.

**"⛔ Gossip was stopped by `gossip kill`"** — the killed sentinel is set. Restart Claude Code, switch to a new conversation, or run `/resume` in Claude to clear it.

**"codex turn already in progress"** — Codex is still executing a prior turn. Wait for "✅ Codex finished", then retry.

**"Codex is not ready. Wait for TUI to connect and create a thread."** — Claude tried to `reply` before the Codex TUI finished booting. Open `gossip codex`, wait a few seconds, try again.

**Codex TUI exits immediately on `gossip codex`** — usually a stale daemon with an obsolete `codex app-server` handshake. Fix:

```bash
gossip kill
rm -f ~/Library/Application\ Support/Gossip/killed
gossip codex
```

**Ports in use** — adjust `daemon.port` / `daemon.proxyPort` in `.gossip/config.json` or set `GOSSIP_CONTROL_PORT` / `GOSSIP_PROXY_PORT`.

Logs live at `<state-dir>/gossip.log`. Tail them with:

```bash
tail -f "$(gossip status 2>/dev/null | grep -o '/[^"]*gossip.log')"
```

## Building from source

Requires Go 1.23+.

```bash
git clone https://github.com/yigitkonur/gossip.git
cd gossip
make build      # produces bin/gossip
./bin/gossip version
```

Common development targets:

```bash
make test       # go test -race -count=1 ./...
make vet        # go vet ./...
make check      # vet + test + build
make tidy       # go mod tidy
make clean      # remove bin/ and dist/
```

## Cross-compilation

Gossip ships as a single static binary. The `Makefile` bakes in targets for every supported platform:

```bash
make release-all     # build every OS/arch into dist/
make release-darwin  # macOS (arm64 + amd64)
make release-linux   # Linux (amd64 + arm64)
```

Output layout:

```
dist/
├── gossip_darwin_amd64/gossip
├── gossip_darwin_arm64/gossip
├── gossip_linux_amd64/gossip
└── gossip_linux_arm64/gossip
```

Each binary is trim-pathed and stripped (`-trimpath -ldflags="-s -w"`) and embeds the git-describe version at build time.

For shipping signed tarballs and checksums (what the `install.sh` downloader consumes), the project uses [goreleaser](https://goreleaser.com/) driven by `.github/workflows/publish.yml` on tagged releases.

## Contributing

Read [`CONTRIBUTING.md`](CONTRIBUTING.md). In short:

1. Branch off `master`. Never push directly to it.
2. Keep PRs small and focused. Squash-merge.
3. Match existing style. Don't refactor adjacent code.
4. Run `make check` locally before opening the PR.

Package-level docs are in each `AGENTS.md` (e.g. `cmd/gossip/AGENTS.md`, `internal/codex/AGENTS.md`).

## Security

See [`SECURITY.md`](SECURITY.md) for the disclosure process. Gossip is a local-only bridge — it never opens network ports on anything except `127.0.0.1`.

## License

MIT. See [`LICENSE`](LICENSE).
