package control

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

type testHandler struct {
	mu          sync.Mutex
	connects    int
	disconnects []string
	replies     []protocol.BridgeMessage
}

func (h *testHandler) OnClaudeConnect() { h.mu.Lock(); h.connects++; h.mu.Unlock() }
func (h *testHandler) OnClaudeDisconnect(reason string) {
	h.mu.Lock()
	h.disconnects = append(h.disconnects, reason)
	h.mu.Unlock()
}
func (h *testHandler) OnClaudeToCodex(_ context.Context, msg protocol.BridgeMessage, _ bool) (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.replies = append(h.replies, msg)
	return true, ""
}
func (h *testHandler) Snapshot() Status { return Status{BridgeReady: true} }

func TestServer_BuffersBroadcastUntilAttach(t *testing.T) {
	h := &testHandler{}
	s := NewServer(h)
	s.Broadcast(context.Background(), protocol.BridgeMessage{ID: "m1", Source: protocol.SourceCodex, Content: "hello"})

	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_connect"}`)); err != nil {
		t.Fatalf("attach: %v", err)
	}

	seenCodex := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancelRead := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, payload, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			continue
		}
		var msg ServerMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Type == ServerMsgCodexToClaude && msg.Message != nil && msg.Message.Content == "hello" {
			seenCodex = true
			break
		}
	}
	if !seenCodex {
		t.Fatal("buffered codex message was not replayed on attach")
	}
}

func TestServer_OlderAttachedBridgeCannotSendAfterReplacement(t *testing.T) {
	h := &testHandler{}
	s := NewServer(h)
	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn1, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial1: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")
	conn2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")

	if err := conn1.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_connect"}`)); err != nil {
		t.Fatalf("attach1: %v", err)
	}
	if err := conn2.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_connect"}`)); err != nil {
		t.Fatalf("attach2: %v", err)
	}
	_ = conn1.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_to_codex","requestId":"r1","message":{"id":"x","source":"claude","content":"old","timestamp":1}}`))

	time.Sleep(200 * time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.replies) != 0 {
		t.Fatalf("stale attached bridge unexpectedly reached handler: %+v", h.replies)
	}
}
