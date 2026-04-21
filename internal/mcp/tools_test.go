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

func TestServer_ReplyTool_PendingMessageHintMatchesContract(t *testing.T) {
	s := NewServer(ServerOptions{
		ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) ReplyResult {
			return ReplyResult{Success: true}
		},
	})
	s.queue = []protocol.BridgeMessage{{ID: "m1", Source: protocol.SourceCodex, Content: "already here"}}

	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params: json.RawMessage(`{
			"name": "reply",
			"arguments": {"text": "hello codex", "chat_id": "chat_1"}
		}`),
	}
	raw, _ := json.Marshal(req)
	var out strings.Builder
	s.writer = &out
	s.handleRequest(context.Background(), raw)

	if !strings.Contains(out.String(), "Note: 1 unread Codex message(s) already waiting — call get_messages to read them.") {
		t.Fatalf("expected pending-message hint, got %q", out.String())
	}
}

func TestServer_GetMessagesIncludesChatIDHeader(t *testing.T) {
	s := NewServer(ServerOptions{ChatIDProvider: func() string { return "thread_123" }})
	s.queue = []protocol.BridgeMessage{{ID: "m1", Source: protocol.SourceCodex, Content: "hello", Timestamp: 1}}

	var out strings.Builder
	s.writer = &out
	s.handleGetMessagesTool(json.RawMessage(`1`))

	if !strings.Contains(out.String(), "chat_id: thread_123") {
		t.Fatalf("expected chat_id header, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Codex: hello") {
		t.Fatalf("expected message body, got %q", out.String())
	}
}

func TestServer_GetMessagesIncludesExternalDroppedDelta(t *testing.T) {
	externalDropped := 2
	s := NewServer(ServerOptions{DroppedCountProvider: func() int { return externalDropped }})
	s.queue = []protocol.BridgeMessage{{ID: "m1", Source: protocol.SourceCodex, Content: "hello", Timestamp: 1}}

	var out strings.Builder
	s.writer = &out
	s.handleGetMessagesTool(json.RawMessage(`1`))

	if !strings.Contains(out.String(), "(2 older message(s) were dropped due to queue overflow)") {
		t.Fatalf("expected overflow notice, got %q", out.String())
	}

	out.Reset()
	s.handleGetMessagesTool(json.RawMessage(`2`))
	if strings.Contains(out.String(), "were dropped due to queue overflow") {
		t.Fatalf("did not expect overflow notice to repeat without new drops, got %q", out.String())
	}
}
