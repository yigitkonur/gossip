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
- [The completion loop](#the-completion-loop)
- [Commands](#commands)
- [Configuration](#configuration)
- [Environment variables](#environment-variables)
- [MCP tools exposed to Claude](#mcp-tools-exposed-to-claude)
- [Uninstall](#uninstall)
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

This downloads the right prebuilt binary for your OS and architecture, drops it at `/usr/local/bin/gossip`, verifies the sha256 checksum, and runs `gossip version`. Re-running upgrades in place; if the target version is already installed it exits cleanly without touching anything.

```bash
# install a specific version
curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash -s -- --version v0.2.0

# install to a user-writable dir (no sudo)
curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | bash -s -- --install-dir "$HOME/.local/bin"

# uninstall
curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash -s -- --uninstall
```

> **Prefer not to pipe to sudo?** Inspect the script first: `curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh -o install.sh && less install.sh`. Or [build from source](#building-from-source).

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

`gossip init` does three things:

1. Writes `.gossip/config.json` and `.gossip/collaboration.md` — the project-local configuration and the shared Claude↔Codex collaboration rules.
2. Merges a `gossip` MCP server entry into `.mcp.json` so Claude Code auto-loads the bridge whenever it opens this directory.
3. Merges Stop + SessionStart hooks into `.claude/settings.json` so the autonomous [completion loop](#the-completion-loop) runs without any extra setup.

All three merges are additive: existing MCP servers, hook entries, and settings are preserved. Re-running init is idempotent.

**4. Start Claude Code.** Either run Claude as usual and let it launch `gossip claude` via the MCP config — or start it directly:

```bash
claude  # Claude Code will spawn gossip claude in stdio mode
```

**5. Attach the Codex TUI.** In a second terminal, from the same project:

```bash
gossip codex
```

The first call starts the background daemon, waits for the proxy to be ready, then launches the Codex TUI pointed at the gossip proxy (`--remote ws://127.0.0.1:<port>`). Either terminal (`gossip claude` or `gossip codex`) can be started first — the daemon is a singleton that the second side just attaches to.

**6. Collaborate.** Ask Claude to do a task that benefits from Codex review:

> "Claude, implement the new rate limiter and have Codex review it before we're done."

Claude works normally. When it believes the task is complete, it ends its reply with the tag `[COMPLETION]` — the Stop hook then automatically sends its summary to Codex, injects Codex's review as Claude's next turn, and loops until Codex approves (via `[COMPLETED]`) or a safety cap stops the loop. See [The completion loop](#the-completion-loop) for the full protocol.

For one-off sends that are not part of a review cycle — "Codex, what's the stack trace on X?" — Claude can also call the `consult_codex` MCP tool directly.

## How it works

Gossip is five layers wearing one binary:

1. **Claude bridge** (`gossip claude`)
   Foreground stdio process. Speaks [MCP](https://modelcontextprotocol.io/) to Claude Code, forwards traffic over a WebSocket to the daemon. Claude sees gossip as "just another MCP server" that exposes `consult_codex` and `get_messages` tools.

2. **Daemon** (`gossip daemon`, auto-started)
   Long-lived background supervisor. Owns the Codex subprocess lifecycle, buffers messages during brief Claude disconnects, multiplexes one Codex TUI at a time, and runs a FIFO outbound queue that blocks for `[IMPORTANT]`-marked Codex replies (the engine behind the completion loop). Survives Claude Code restarts and session switches.

3. **Codex proxy**
   A local WebSocket endpoint that the Codex TUI connects to (`codex --remote`). Rewrites request IDs, replays cached `initialize` / `thread/start` results when a duplicate TUI re-initialises, and rewrites `userAgent` strings so Codex thinks it's talking to a real app-server.

4. **Codex app-server connection**
   The daemon keeps one WebSocket open to `codex app-server`. That's the upstream authority on threads, turns, and tool calls.

5. **Hook layer** (`gossip hook …`, wired into `.claude/settings.json` by `gossip init`)
   Short-lived subprocesses Claude Code spawns at `SessionStart` and `Stop`. They read the live session transcript, detect the `[COMPLETION]` tag, and drive the autonomous review cycle with Codex via the daemon's blocking queue. No state of their own; all state lives in the daemon or in `<state-dir>/loop-state.json` (per-user, not per-project — see the State directory bullet below).

Message path, visualised:

```
Claude Code ─┬─▶ gossip claude (MCP stdio)          ──┐
             │       │                                 │ consult_codex (explicit)
             │       ▼                                 │
             ├─▶ gossip hook stop (fires on [COMPLETION]) ─┤ gossip bridge send (blocking)
             │       │                                 │
             │       ▼                                 ▼
             │   gossip daemon ◀── TUI state, killed sentinel,
             │       │              loop queue, message buffer, thread ID
             │       ▼
             │   gossip codex proxy ◀──── Codex TUI (ws://127.0.0.1:N)
             │       │
             │       ▼
             └── codex app-server (upstream)
```

### Key runtime rules

- **Readiness gating.** Claude can only `consult_codex` when the TUI is attached *and* a Codex thread exists. Otherwise the daemon returns a "Codex is not ready" error instead of silently dropping the message.
- **Single current TUI.** Only the most-recent Codex TUI connection receives live upstream traffic. Stale TUIs are isolated.
- **Buffered replay.** A short Claude disconnect doesn't lose messages — the daemon buffers up to `max-buffered-messages` and replays on reconnect.
- **Blocking queue for loop sends.** Completion-loop pushes go through an in-memory FIFO on the daemon. If the TUI has not attached yet when the hook fires, the send waits (up to `loop.perTurnTimeoutMs`) and flushes automatically when Codex connects — so `gossip claude` and `gossip codex` can be started in either order without losing a loop round.
- **Killed sentinel.** `gossip kill` writes a file that prevents auto-reconnect until you explicitly restart. This is what lets you reliably park the system.
- **State directory.** Runtime files live in a per-user directory (`~/Library/Application Support/Gossip` on macOS, `~/.local/state/gossip` on Linux) — `daemon.pid`, `daemon.lock`, `status.json`, `gossip.log`, `killed`, `codex-tui.pid`, `loop-state.json`.

## The completion loop

The completion loop is the core productivity feature of gossip: Claude does a piece of work, announces it is done with a **sentinel tag**, and Codex reviews. If Codex approves, Claude writes a one-line confirmation to the user and stops. If Codex pushes back, its feedback is injected as Claude's next turn and the cycle continues — up to a hard iteration cap. All of this happens **without you copy-pasting**, without Claude calling tools, and without a single extra human turn.

### The protocol

Two tags make the loop work. Everything else is mechanics.

| Who writes it | Tag            | Meaning                                                   |
| ------------- | -------------- | --------------------------------------------------------- |
| **Claude**    | `[COMPLETION]` | "I believe this task is done — please review."            |
| **Codex**     | `[COMPLETED]`  | "Approved. Looks correct and complete."                   |

Tags are matched case-insensitively on word boundaries. `[COMPLETION]`, `COMPLETION`, and `completion` all match. `COMPLETIONS` and `RECOMPLETION` do not. The defaults deliberately avoid generic words like `DONE` or `APPROVED` (which would match ordinary prose like "I'm done with the changes" or "not approved"). Additional aliases are configurable per project in `.gossip/config.json → loop.{completionTags,approvalTags}` — the default `LGTM` approval tag demonstrates that aliases work.

### The cycle

```
User: "Claude, refactor the rate limiter and have Codex review it."
│
▼
Claude: (plans, edits files, runs checks)
Claude: "Done. Token-bucket implementation in internal/ratelimit/, tests pass. [COMPLETION]"
│
▼ Stop hook reads the last assistant message, spots [COMPLETION],
  pushes a summary through the daemon loop queue to Codex.
│
▼
Codex (in the other terminal): (reads, inspects files, thinks)
Codex: "The bucket size should be configurable, not hard-coded at 100."
│
▼ Hook returns {decision: "block", reason: "Codex review: …"} which Claude
  receives as its next user turn.
│
▼
Claude: (reads the feedback, edits)
Claude: "Exposed bucketSize via constructor and updated tests. [COMPLETION]"
│
▼ Loop iteration 2 — same path through Codex.
│
▼
Codex: "Good. [COMPLETED]"
│
▼ Hook sees the approval tag, blocks one more time with reason
  "Codex approved. Write a one-line confirmation, no more revisions."
│
▼
Claude: "Rate limiter refactor complete; bucket size is now configurable and
         Codex signed off on the change."
│
▼ No [COMPLETION] in the last message → Stop hook exits silently →
  Claude stops for real. The user sees one clean summary.
```

### Termination rules

The loop always ends — never hangs the session. The Stop hook terminates with an instruction to Claude in one of four ways:

1. **Approved.** Codex's reply contains an approval tag → hook blocks once with "Codex approved — write a one-line confirmation". Loop state is reset to iteration 0.
2. **Iteration cap.** Default 5 rounds. When Claude hits the cap without approval, the hook escalates: "Loop cap reached; summarize current state for the user and hand back for a human decision." Loop state resets.
3. **No reply.** Codex is silent for `loop.perTurnTimeoutMs` (default 90 s) or its turn finishes without an `[IMPORTANT]`-marked reply → hook escalates: "Codex did not reply: &lt;reason&gt;. Ask the user whether to retry or abandon."
4. **Daemon unreachable.** Hook treats it as branch #3 with the specific error surfaced.

### When the hook does nothing

The Stop hook is deliberately conservative. It exits silently (no `decision: "block"`) in any of:

- `loop.enabled` is false, or `GOSSIP_LOOP_DISABLE=1` is set.
- The last assistant message has no completion tag.
- The last assistant message still contains a pending `tool_use` block — Claude is mid tool-loop, not "done".
- The transcript file cannot be read (fail-open).

This means the loop never interferes with normal conversation. It engages only when Claude explicitly announces completion.

### Loop configuration

The `loop` block in `.gossip/config.json`:

```jsonc
{
  "loop": {
    "enabled": true,               // master switch
    "maxIterations": 5,            // approval-less cap before the hook escalates
    "perTurnTimeoutMs": 90000,     // per-round wait for Codex's reply
    "completionTags": ["COMPLETION"],          // Claude → "review please"
    "approvalTags":   ["COMPLETED", "LGTM"]    // Codex → "approved"
  }
}
```

Setting `loop.enabled: false` (or `GOSSIP_LOOP_DISABLE=1` in the environment) makes `gossip hook stop` a no-op, regardless of what Claude writes. Useful for CI, scripted runs, or when you want Claude to converse without the review gate.

### What Codex sees

Every completion-loop push is wrapped with a short preamble so Codex understands the request regardless of what's in its own system prompt (Codex has no `--append-system-prompt` flag, so the prefix is the only reliable steering vector):

```
[gossip:review-request iter=1/max=5]
Claude believes the task below is complete. Please review and:
  - If correct and complete, include [COMPLETED] in your reply.
  - Otherwise, describe specifically what needs to change.
Task summary from Claude:
---
<Claude's final assistant message, [COMPLETION] tag included>
```

Codex is also instructed (via the existing `require_reply` mechanism) to reply with an `[IMPORTANT]` marker so the daemon force-forwards the answer without attention-window delay. The hook waits for that `[IMPORTANT]` message; a plain `[STATUS]` / `[FYI]` reply won't close the round.

### Startup-order independence

Either `gossip claude` or `gossip codex` can be started first. If Claude emits `[COMPLETION]` while Codex is not yet attached, the daemon's loop queue holds the send in memory and pumps it the moment Codex becomes ready (`EventThreadReady`). The hook gets the reply within `perTurnTimeoutMs` either way — so you can start Claude first, prompt a task, and open Codex minutes later.

### Under the hood

- `gossip hook session-start` — injects a short protocol primer into the session context so Claude knows about `[COMPLETION]` without you adding it to a CLAUDE.md file.
- `gossip hook stop` — the decision table above, with advisory-locked loop state at `<state-dir>/loop-state.json`.
- `gossip hook user-prompt` — reserved, no-op in v1.
- `gossip bridge send` — the CLI the Stop hook uses to dial the daemon's blocking queue; also useful for manual debugging.

All four are hidden from `gossip --help` by default because nothing calls them interactively. See the config keys above, `.claude/settings.json` for wiring, and `<state-dir>/loop-state.json` for live state.

## Commands

| Command                  | What it does                                                                        |
| ------------------------ | ----------------------------------------------------------------------------------- |
| `gossip init`            | Scaffold `.gossip/`, merge `.mcp.json` + `.claude/settings.json` hooks, install the Claude plugin. |
| `gossip init --uninstall`| Reverse the footprint: strip gossip hooks, strip gossip MCP entry, delete `.gossip/`. |
| `gossip claude`          | Run the MCP bridge (stdio). Claude Code invokes this automatically.                 |
| `gossip codex`           | Ensure daemon is up, wait for proxy, then launch `codex` attached to it.            |
| `gossip daemon`          | Run the background daemon in the foreground (usually auto-started).                 |
| `gossip status`          | Print daemon state, bridge readiness, TUI attachment, thread ID, queue size.        |
| `gossip kill`            | Stop the daemon and write the killed sentinel.                                      |
| `gossip bridge send`     | Blocking one-shot push into Codex via the daemon. Used by `gossip hook stop`.       |
| `gossip hook …`          | Directory-level hook handlers (hidden). Wired by `gossip init`.                     |
| `gossip version`         | Print the build version.                                                            |

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
  },
  "loop": {
    "enabled": true,
    "maxIterations": 5,
    "perTurnTimeoutMs": 90000,
    "completionTags": ["COMPLETION"],
    "approvalTags":   ["COMPLETED", "LGTM"]
  }
}
```

The TS-shape aliases (`codex.appPort`, `codex.proxyPort`, `claude.mode`) are still read when present. Gossip writes back in the Go-shape.

### Delivery modes

- **pull** *(default)* — Claude calls the `get_messages` tool whenever it wants to check for new Codex output. Keeps your Claude context window tight.
- **push** — The daemon streams `<channel source="gossip" ...>` tags into Claude's MCP stream as soon as Codex speaks. Chattier, lower-latency.

### Loop knobs

See [The completion loop](#the-completion-loop) for the full narrative. The `loop` block above is the whole surface; any legacy config (no `loop` key) inherits the defaults when the runtime loads it.

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
| `GOSSIP_LOOP_DISABLE`            | When non-empty, `gossip hook stop` exits silently and the completion loop is disabled for this process (takes precedence over `.gossip/config.json → loop.enabled`). |
| `GOSSIP_PLUGIN_BUNDLE_URL`       | Override the GitHub archive URL used as the last-resort plugin install source. Empty means "derive from the binary's build version; fall back to master on a dev build". |

## MCP tools exposed to Claude

Two tools live on the `gossip` MCP server:

- **`consult_codex`** — Send a message to Codex. Lands as a new user turn in Codex's session. Required: `text`. Optional: `require_reply` (bool) — when true, Codex is force-forwarded even through the attention-window filter. No `chat_id` needed; each project has one gossip daemon and one thread.
- **`get_messages`** — Drain any buffered messages from Codex. Returns "No new messages from Codex." if nothing is pending.

Example (Claude calling `consult_codex`):

```json
{
  "name": "consult_codex",
  "arguments": {
    "text": "Codex, please reproduce the failing test in internal/codex/proxy.go and paste the stack trace.",
    "require_reply": true
  }
}
```

For end-of-task review the hook layer is the primary path — ending your assistant message with `[COMPLETION]` does the same thing but without a tool call and with iteration control (see [The completion loop](#the-completion-loop)). Reserve `consult_codex` for targeted mid-turn consultations.

The bridge also injects a short system-instructions block describing turn coordination ("⏳ Codex is working"), collaboration roles, and thinking patterns. See `cmd/gossip/cmd_claude.go` for the exact prompt.

## Uninstall

To remove gossip from a project:

```bash
gossip init --uninstall
```

This strips gossip's hook entries from `.claude/settings.json`, removes the gossip MCP server from `.mcp.json`, and deletes `.gossip/`. Unrelated hooks and MCP servers are preserved; when a file was gossip-only, it is removed entirely. Re-run `gossip init` to restore the full install.

To remove the binary itself:

```bash
curl -fsSL https://raw.githubusercontent.com/yigitkonur/gossip/master/install.sh | sudo bash -s -- --uninstall
```

Deleting `.gossip/` alone is a **soft-disable**: all hook handlers short-circuit cleanly when their config file is missing, so `.claude/settings.json` entries become zero-behavior rather than erroring out. Useful when you want to pause gossip temporarily without running the explicit uninstall.

## Troubleshooting

**`gossip status` says "daemon: not running"** — the background daemon has exited (probably idle-shutdown). Run `gossip codex` or `gossip claude` to bring it back.

**"⛔ Gossip was stopped by `gossip kill`"** — the killed sentinel is set. Restart Claude Code, switch to a new conversation, or run `/resume` in Claude to clear it.

**"codex turn already in progress"** — Codex is still executing a prior turn. Wait for "✅ Codex finished", then retry.

**"Codex is not ready. Wait for TUI to connect and create a thread."** — Claude tried to `consult_codex` before the Codex TUI finished booting. Open `gossip codex`, wait a few seconds, try again. (The completion-loop hook path queues the send and waits instead of failing, so this error only surfaces on the direct MCP tool.)

**Loop never terminates / hits the iteration cap repeatedly.** Lower `loop.maxIterations` in `.gossip/config.json` or flip `loop.enabled: false` for a session where the review cycle is not helping. `GOSSIP_LOOP_DISABLE=1` also disables the hook globally for one invocation — useful when you want to ask Claude a question without triggering review.

**Claude says "Codex did not reply"** — the hook's per-turn timeout elapsed without an `[IMPORTANT]`-marked Codex message. The daemon delivers a clear continuation instruction instead of hanging; Claude will ask the user to decide. Increase `loop.perTurnTimeoutMs` if your Codex reviews routinely take longer than 90 s.

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
