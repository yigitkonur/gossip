package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

type testHandler struct {
	mu          sync.Mutex
	connects    int
	disconnects []string
	replies     []protocol.BridgeMessage
	status      Status
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
func (h *testHandler) Snapshot() Status { return h.status }

func TestServer_BuffersBroadcastUntilAttach(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}}
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

func TestServer_BufferOverflowTracksDroppedMessages(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}}
	s := NewServer(h)
	for i := 0; i < 102; i++ {
		s.Broadcast(context.Background(), protocol.BridgeMessage{ID: fmt.Sprintf("m%d", i), Source: protocol.SourceCodex, Content: "hello"})
	}
	if got := s.QueuedCount(); got != 100 {
		t.Fatalf("QueuedCount() = %d, want 100", got)
	}
	if got := s.DroppedCount(); got != 2 {
		t.Fatalf("DroppedCount() = %d, want 2", got)
	}
}

func TestServer_RejectsLateDuplicateWithCloseCode4001(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}}
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
	waitForControlMessage(t, conn1)

	if err := conn2.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_connect"}`)); err != nil {
		t.Fatalf("attach2: %v", err)
	}

	readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRead()
	for {
		_, _, err := conn2.Read(readCtx)
		if err == nil {
			continue
		}
		closeErr := websocket.CloseStatus(err)
		if closeErr != CloseCodeReplaced {
			t.Fatalf("duplicate close code = %v, want %v (err=%v)", closeErr, CloseCodeReplaced, err)
		}
		break
	}

	if err := conn1.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_to_codex","requestId":"r1","message":{"id":"x","source":"claude","content":"still attached","timestamp":1}}`)); err != nil {
		t.Fatalf("active client write failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.replies) != 1 || h.replies[0].Content != "still attached" {
		t.Fatalf("active client should remain attached, replies=%+v", h.replies)
	}
	if h.connects != 1 {
		t.Fatalf("connect count = %d, want 1", h.connects)
	}
}

func TestServer_HandleReady_SetsContentTypeOn503(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: false}}
	s := NewServer(h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/readyz", nil)

	s.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func waitForControlMessage(t *testing.T, conn *websocket.Conn) ServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read control message: %v", err)
		}
		var msg ServerMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		return msg
	}
}
