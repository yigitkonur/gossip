package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestServer_ReplyTool_InvokesHandler(t *testing.T) {
	var got protocol.BridgeMessage
	var gotRequire bool
	s := NewServer(ServerOptions{
		ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) ReplyResult {
			got = msg
			gotRequire = requireReply
			return ReplyResult{Success: true}
		},
	})

	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params: json.RawMessage(`{
			"name": "reply",
			"arguments": {"text": "hello codex", "chat_id": "chat_1", "require_reply": true}
		}`),
	}
	raw, _ := json.Marshal(req)
	var out strings.Builder
	s.writer = &out
	s.handleRequest(context.Background(), raw)

	if got.Content != "hello codex" {
		t.Errorf("got.Content = %q", got.Content)
	}
	if got.Source != protocol.SourceClaude {
		t.Errorf("got.Source = %q", got.Source)
	}
	if got.ID != "chat_1" {
		t.Errorf("got.ID = %q", got.ID)
	}
	if !gotRequire {
		t.Error("require_reply should be true")
	}
}
