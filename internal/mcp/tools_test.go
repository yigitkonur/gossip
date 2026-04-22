package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestServer_ConsultCodex_InvokesHandler(t *testing.T) {
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
			"name": "consult_codex",
			"arguments": {"text": "hello codex", "require_reply": true}
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
	if !strings.HasPrefix(got.ID, "consult_") {
		t.Errorf("got.ID = %q, want consult_<ts> auto-generated", got.ID)
	}
	if !gotRequire {
		t.Error("require_reply should be true")
	}
}

func TestServer_ConsultCodex_DropsChatIDFromSchema(t *testing.T) {
	s := NewServer(ServerOptions{})
	req := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"}
	raw, _ := json.Marshal(req)
	var out strings.Builder
	s.writer = &out
	s.handleRequest(context.Background(), raw)

	body := out.String()
	if !strings.Contains(body, `"consult_codex"`) {
		t.Fatalf("tool list missing consult_codex: %s", body)
	}
	if strings.Contains(body, `"reply"`) {
		t.Fatalf("tool list still contains legacy reply entry: %s", body)
	}
	if strings.Contains(body, `"chat_id"`) {
		t.Fatalf("tool list still exposes chat_id: %s", body)
	}
}

func TestServer_ConsultCodex_PendingInboxHint(t *testing.T) {
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
			"name": "consult_codex",
			"arguments": {"text": "hello codex"}
		}`),
	}
	raw, _ := json.Marshal(req)
	var out strings.Builder
	s.writer = &out
	s.handleRequest(context.Background(), raw)

	body := out.String()
	if !strings.Contains(body, "Sent.") {
		t.Fatalf("expected declarative Sent. confirmation, got %q", body)
	}
	if !strings.Contains(body, "1 Codex message(s) in your inbox.") {
		t.Fatalf("expected inbox count, got %q", body)
	}
	if strings.Contains(body, "call get_messages") {
		t.Fatalf("response still contains imperative 'call get_messages' hint: %q", body)
	}
}

func TestServer_UnknownToolName_ReplyRemoved(t *testing.T) {
	s := NewServer(ServerOptions{
		ReplyHandler: func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) ReplyResult {
			return ReplyResult{Success: true}
		},
	})
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"reply","arguments":{"text":"x"}}`),
	}
	raw, _ := json.Marshal(req)
	var out strings.Builder
	s.writer = &out
	s.handleRequest(context.Background(), raw)
	if !strings.Contains(out.String(), "unknown tool: reply") {
		t.Fatalf("expected unknown-tool error for legacy reply name, got %q", out.String())
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
