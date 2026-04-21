// Package control defines the wire protocol between the gossip bridge
// (foreground MCP server) and daemon (background Codex proxy host).
package control

import (
	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

// ClientMessage is a bridge-to-daemon message.
type ClientMessage struct {
	Type         string                  `json:"type"`
	RequestID    string                  `json:"requestId,omitempty"`
	Message      *protocol.BridgeMessage `json:"message,omitempty"`
	RequireReply bool                    `json:"requireReply,omitempty"`
}

// ServerMessage is a daemon-to-bridge message.
type ServerMessage struct {
	Type      string                  `json:"type"`
	Message   *protocol.BridgeMessage `json:"message,omitempty"`
	RequestID string                  `json:"requestId,omitempty"`
	Success   bool                    `json:"success,omitempty"`
	Error     string                  `json:"error,omitempty"`
	Status    *Status                 `json:"status,omitempty"`
}

const (
	ClientMsgClaudeConnect    = "claude_connect"
	ClientMsgClaudeDisconnect = "claude_disconnect"
	ClientMsgClaudeToCodex    = "claude_to_codex"
	ClientMsgStatus           = "status"
)

const (
	ServerMsgCodexToClaude       = "codex_to_claude"
	ServerMsgClaudeToCodexResult = "claude_to_codex_result"
	ServerMsgStatus              = "status"
)

const CloseCodeReplaced websocket.StatusCode = 4001

// Status is a daemon snapshot.
type Status struct {
	BridgeReady         bool   `json:"bridgeReady"`
	TuiConnected        bool   `json:"tuiConnected"`
	ThreadID            string `json:"threadId"`
	QueuedMessageCount  int    `json:"queuedMessageCount"`
	DroppedMessageCount int    `json:"droppedMessageCount"`
	ProxyURL            string `json:"proxyUrl"`
	AppServerURL        string `json:"appServerUrl"`
	Pid                 int    `json:"pid"`
}
