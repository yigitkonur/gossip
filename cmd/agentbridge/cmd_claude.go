package main

import (
	"context"
	"os"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/control"
	"github.com/raysonmeng/agent-bridge/internal/daemon"
	"github.com/raysonmeng/agent-bridge/internal/mcp"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
	"github.com/spf13/cobra"
)

const claudeInstructions = `Codex is an AI coding agent (OpenAI) running in a separate session on the same machine.

## Message delivery
Messages from Codex may arrive as <channel source="agentbridge" chat_id="..." user="Codex" ...> tags.
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
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: 4502, Logger: logToStderr})

			ctx := cmd.Context()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			var srv *mcp.Server
			cc := control.NewClient(control.ClientOptions{
				URL: lc.ControlWsURL(),
				OnCodexMsg: func(msg protocol.BridgeMessage) {
					if srv != nil {
						srv.PushMessage(msg)
					}
				},
				Logger: logToStderr,
			})

			srv = mcp.NewServer(mcp.ServerOptions{
				Name:         "agentbridge",
				Version:      version,
				Instructions: claudeInstructions,
				ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) mcp.ReplyResult {
					ok, errMsg := cc.SendReply(ctx, msg, requireReply)
					return mcp.ReplyResult{Success: ok, Error: errMsg}
				},
			})

			startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
			defer startCancel()
			if err := lc.EnsureRunning(startCtx); err != nil {
				return err
			}
			go cc.RunWithReconnect(ctx)

			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}
}
