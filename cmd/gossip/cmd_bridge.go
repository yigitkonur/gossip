package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/protocol"
	"github.com/yigitkonur/gossip/internal/statedir"
)

// BridgeSendResult is the JSON document `gossip bridge send` prints to stdout.
// All three fields are always set so consumers can decide without branching on
// the presence of keys. Error is an empty string when received=true.
type BridgeSendResult struct {
	Received bool   `json:"received"`
	Text     string `json:"text"`
	Error    string `json:"error"`
}

var (
	bridgeNewClient = defaultBridgeNewClient
	bridgeStateDir  = func() *statedir.StateDir { return statedir.New("") }
)

type bridgeClient interface {
	Connect(ctx context.Context) error
	SendReplyBlocking(ctx context.Context, msg protocol.BridgeMessage, requireReply bool, waitMs int) (text string, received bool, errMsg string)
	Disconnect()
}

func defaultBridgeNewClient(url string) bridgeClient {
	return control.NewClient(control.ClientOptions{URL: url})
}

func newBridgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bridge",
		Short: "Low-level bridge utilities (primarily used by hooks)",
	}
	cmd.AddCommand(newBridgeSendCmd())
	return cmd
}

func newBridgeSendCmd() *cobra.Command {
	var (
		text         string
		requireReply bool
		waitMs       int
	)
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to Codex via the daemon and wait for an [IMPORTANT] reply",
		Long: `Send a blocking message to Codex through the running gossip daemon.

Dials the daemon control WebSocket, enqueues the message in the Claude→Codex
loop queue, and prints a JSON result on stdout once Codex replies with an
[IMPORTANT]-marked agentMessage, waitMs elapses, or the Codex turn completes
without a reply.

Exit codes:
  0  received=true (Codex replied)
  1  received=false (timeout, turn-completed-without-reply, or send error)
  2  daemon unreachable or local error

Stdin: if --text is empty and stdin is not a terminal, the prompt is read
from stdin. Useful for piping summaries from hook handlers.

Intended for use by the completion-loop Stop hook (` + "`gossip hook stop`" + `);
end users rarely invoke this directly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if text == "" {
				stdinBytes, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				text = string(stdinBytes)
			}
			if text == "" {
				return fmt.Errorf("missing --text and no stdin payload")
			}
			result, code := runBridgeSend(ctx, bridgeSendParams{
				Text:         text,
				RequireReply: requireReply,
				WaitMs:       waitMs,
			})
			writeResult(cmd.OutOrStdout(), result)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "Message body (falls back to stdin if empty)")
	cmd.Flags().BoolVar(&requireReply, "require-reply", true, "Force Codex to reply with [IMPORTANT] before returning")
	cmd.Flags().IntVar(&waitMs, "wait-ms", 90_000, "How long the daemon waits for Codex's reply before timing out")
	return cmd
}

type bridgeSendParams struct {
	Text         string
	RequireReply bool
	WaitMs       int
}

// runBridgeSend performs the bridge send and returns (result, exitCode).
// Exit code is 0 when Codex replied, 1 when no reply (timeout / failure),
// 2 when the daemon could not be reached at all. The caller writes the
// result JSON and decides whether to os.Exit — keeping this seam makes the
// function test-friendly.
func runBridgeSend(ctx context.Context, p bridgeSendParams) (BridgeSendResult, int) {
	sd := bridgeStateDir()
	_ = sd.Ensure()
	port := resolvedControlPort(sd)
	url := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: port}).ControlWsURL()

	c := bridgeNewClient(url)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Connect(dialCtx); err != nil {
		return BridgeSendResult{Received: false, Error: fmt.Sprintf("daemon unreachable: %v", err)}, 2
	}
	defer c.Disconnect()

	msg := protocol.BridgeMessage{
		ID:        fmt.Sprintf("bridge_send_%d", time.Now().UnixMilli()),
		Source:    protocol.SourceClaude,
		Content:   p.Text,
		Timestamp: time.Now().UnixMilli(),
	}
	text, received, errMsg := c.SendReplyBlocking(ctx, msg, p.RequireReply, p.WaitMs)
	code := 0
	if !received {
		code = 1
	}
	return BridgeSendResult{Received: received, Text: text, Error: errMsg}, code
}

func writeResult(out io.Writer, r BridgeSendResult) {
	body, _ := json.MarshalIndent(r, "", "  ")
	fmt.Fprintln(out, string(body))
}
