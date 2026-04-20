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
Messages from Codex may arrive as <channel source="gossip" chat_id="..." user="Codex" ...> tags.
Use the reply tool to send messages back to Codex.
Use the get_messages tool to check for pending messages.

## Collaboration roles
- Claude: Reviewer, Planner, Hypothesis Challenger
- Codex: Implementer, Executor, Reproducer/Verifier

## Turn coordination
- When you see '⏳ Codex is working', do NOT call the reply tool — wait for '✅ Codex finished'.
- If the reply tool returns a busy error, Codex is still executing — wait and retry later.`

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
			pushSystem := func(id, content string) {
				if srv == nil {
					return
				}
				srv.PushMessage(protocol.BridgeMessage{ID: fmt.Sprintf("%s_%d", id, time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: content, Timestamp: time.Now().UnixMilli()})
			}

			var cc *control.Client
			cc = control.NewClient(control.ClientOptions{
				URL: lc.ControlWsURL(),
				OnCodexMsg: func(msg protocol.BridgeMessage) {
					if srv != nil {
						srv.PushMessage(msg)
					}
				},
				OnDisconnect: func(_ int, _ string, _ time.Duration) {
					if lc.WasKilled() {
						bridgeDisabled.Store(true)
						pushSystem("system_bridge_disabled", "⛔ Gossip was stopped by gossip kill. Bridge is staying idle until you restart with gossip codex.")
					}
				},
				ShouldReconnect: func() bool { return !bridgeDisabled.Load() && !lc.WasKilled() },
				Logger:          logToStderr,
			})

			srv = mcp.NewServer(mcp.ServerOptions{
				Name:         "gossip",
				Version:      version,
				Instructions: claudeInstructions,
				DeliveryMode: resolveClaudeDeliveryMode(cfg, logToStderr),
				ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) mcp.ReplyResult {
					if bridgeDisabled.Load() {
						return mcp.ReplyResult{Success: false, Error: "Gossip is disabled by gossip kill. Restart with gossip codex to reconnect."}
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
							pushSystem("system_bridge_recovered", "✅ Gossip daemon reconnected after the killed sentinel was cleared.")
							startReconnect()
						}
					}
				}()
			}

			if lc.WasKilled() {
				bridgeDisabled.Store(true)
				go func() {
					<-srv.Ready()
					startRecoveryPoller()
					pushSystem("system_bridge_disabled", "⛔ Gossip was stopped by gossip kill. Bridge is staying idle until you restart with gossip codex.")
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
