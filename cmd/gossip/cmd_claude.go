package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/mcp"
	"github.com/yigitkonur/gossip/internal/protocol"
	"github.com/yigitkonur/gossip/internal/statedir"
)

const claudeInstructions = `Codex is an AI coding agent (OpenAI) running in a separate session on the same machine.

## Message delivery
Messages from Codex may arrive in two ways depending on the connection mode:
- As <channel source="gossip" chat_id="..." user="Codex" ...> tags (push mode)
- Via the get_messages tool (pull mode)

## Collaboration roles
Default roles in this setup:
- Claude: Reviewer, Planner, Hypothesis Challenger
- Codex: Implementer, Executor, Reproducer/Verifier
- Expect Codex to provide independent technical judgment and evidence, not passive agreement.

## Thinking patterns (task-driven)
- Analytical/review tasks: Independent Analysis & Convergence
- Implementation tasks: Architect→Builder→Critic
- Debugging tasks: Hypothesis→Experiment→Interpretation

## Collaboration language
- Use explicit phrases such as "My independent view is:", "I agree on:", "I disagree on:", and "Current consensus:".

## How to interact
- Use the reply tool to send messages back to Codex — pass chat_id back.
- Use the get_messages tool to check for pending messages from Codex.
- In pull mode, get_messages returns a header like [N new message(s) from Codex], an optional overflow notice, a chat_id: line, and then the queued messages.
- After sending a reply, call get_messages to check for responses.
- When the user asks about Codex status or progress, call get_messages.

## Turn coordination
- When you see '⏳ Codex is working', do NOT call the reply tool — wait for '✅ Codex finished'.
- After Codex finishes a turn, you have an attention window to review and respond before new messages arrive.
- If the reply tool returns a busy error, Codex is still executing — wait and try again later.`

func newClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Run the MCP bridge (foreground stdio server) — invoked by Claude Code",
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			_ = sd.Ensure()
			cfg := config.NewService("").LoadOrDefault()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: controlPort(), Logger: logToStderr})

			ctx := cmd.Context()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			var srv *mcp.Server
			var bridgeDisabled atomic.Bool
			var reconnectRunning atomic.Bool
			var currentChatID atomic.Value
			var currentDisabledReason atomic.Value
			var currentDroppedCount atomic.Int64
			currentChatID.Store("")
			currentDisabledReason.Store(bridgeDisabledReasonNone)
			currentDisabled := func() bridgeDisabledReason {
				reason, _ := currentDisabledReason.Load().(bridgeDisabledReason)
				return reason
			}
			pushSystem := func(id, content string) {
				if srv == nil {
					return
				}
				srv.PushMessage(protocol.BridgeMessage{ID: fmt.Sprintf("%s_%d", id, time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: content, Timestamp: time.Now().UnixMilli()})
			}
			enterDisabledState := func(reason bridgeDisabledReason, id, content string) {
				bridgeDisabled.Store(true)
				currentDisabledReason.Store(reason)
				pushSystem(id, content)
			}

			var cc *control.Client
			cc = control.NewClient(control.ClientOptions{
				URL: lc.ControlWsURL(),
				OnCodexMsg: func(msg protocol.BridgeMessage) {
					if srv != nil {
						srv.PushMessage(msg)
					}
				},
				OnStatus: func(status control.Status) {
					currentChatID.Store(status.ThreadID)
					currentDroppedCount.Store(int64(status.DroppedMessageCount))
				},
				OnDisconnect: func(_ int, _ string, _ time.Duration) {
					if lc.WasKilled() {
						enterDisabledState(bridgeDisabledReasonKilled, "system_bridge_disabled", "⛔ Gossip was stopped by `gossip kill`. Bridge is staying idle. Restart Claude Code (`gossip claude`), switch to a new conversation, or run /resume to reconnect.")
					}
				},
				OnRejected: func(_ int, _ string, _ time.Duration) {
					enterDisabledState(bridgeDisabledReasonRejected, "system_bridge_replaced", "⚠️ Gossip daemon rejected this session — another Claude Code session is already connected. Close the other session first, or run `gossip kill` to reset.")
				},
				ShouldReconnect: func() bool { return !bridgeDisabled.Load() && !lc.WasKilled() },
				Logger:          logToStderr,
			})

			srv = mcp.NewServer(mcp.ServerOptions{
				Name:         "gossip",
				Version:      version,
				Instructions: claudeInstructions,
				ChatIDProvider: func() string {
					chatID, _ := currentChatID.Load().(string)
					return chatID
				},
				DroppedCountProvider: func() int {
					return int(currentDroppedCount.Load())
				},
				DeliveryMode: resolveClaudeDeliveryMode(cfg, logToStderr),
				ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) mcp.ReplyResult {
					if bridgeDisabled.Load() {
						return mcp.ReplyResult{Success: false, Error: disabledReplyError(currentDisabled())}
					}
					ok, errMsg := cc.SendReply(ctx, msg, requireReply)
					return mcp.ReplyResult{Success: ok, Error: errMsg}
				},
			})

			startReconnect := func() {
				reconnectRunning.Store(true)
				go func() { defer reconnectRunning.Store(false); _ = cc.RunWithReconnect(ctx) }()
			}

			startRecoveryPoller := func() {
				go func() {
					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							if lc.WasKilled() || !bridgeDisabled.Load() || reconnectRunning.Load() {
								continue
							}
							recoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
							healthy := lc.IsHealthy(recoveryCtx)
							cancel()
							if !healthy {
								continue
							}
							bridgeDisabled.Store(false)
							currentDisabledReason.Store(bridgeDisabledReasonNone)
							pushSystem("system_bridge_recovered", "✅ Gossip daemon reconnected after the disabled state cleared.")
							startReconnect()
						}
					}
				}()
			}

			if lc.WasKilled() {
				bridgeDisabled.Store(true)
				currentDisabledReason.Store(bridgeDisabledReasonKilled)
				go func() {
					<-srv.Ready()
					startRecoveryPoller()
					pushSystem("system_bridge_disabled", "⛔ Gossip was stopped by `gossip kill`. Bridge is staying idle. Restart Claude Code (`gossip claude`), switch to a new conversation, or run /resume to reconnect.")
				}()
				return srv.Serve(ctx, os.Stdin, os.Stdout)
			}

			startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
			defer startCancel()
			if err := lc.EnsureRunning(startCtx); err != nil {
				return err
			}
			go func() {
				<-srv.Ready()
				startRecoveryPoller()
				startReconnect()
			}()

			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}
}

func resolveClaudeDeliveryMode(cfg config.Config, logger func(string)) mcp.DeliveryMode {
	if mode, ok := parseDeliveryMode(os.Getenv("GOSSIP_MODE")); ok {
		return mode
	}
	if mode, ok := parseDeliveryMode(os.Getenv("AGENTBRIDGE_MODE")); ok {
		return mode
	}
	if agent, ok := cfg.Agents["claude"]; ok {
		if mode, ok := parseDeliveryMode(agent.Mode); ok {
			return mode
		}
	}
	if logger != nil {
		logger("Delivery mode defaulting to pull")
	}
	return mcp.DeliveryPull
}

func parseDeliveryMode(raw string) (mcp.DeliveryMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(mcp.DeliveryPush):
		return mcp.DeliveryPush, true
	case string(mcp.DeliveryPull):
		return mcp.DeliveryPull, true
	default:
		return "", false
	}
}
