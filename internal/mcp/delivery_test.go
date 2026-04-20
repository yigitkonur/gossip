package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestServer_DefaultDeliveryModeIsPull(t *testing.T) {
	s := NewServer(ServerOptions{})
	if s.opts.DeliveryMode != DeliveryPull {
		t.Fatalf("DeliveryMode = %q, want %q", s.opts.DeliveryMode, DeliveryPull)
	}
}

func TestServer_PullMode_QueuesMessages(t *testing.T) {
	s := NewServer(ServerOptions{DeliveryMode: DeliveryPull, MaxBufferedMessages: 3})
	for i := 0; i < 5; i++ {
		s.PushMessage(protocol.BridgeMessage{ID: "m", Source: protocol.SourceCodex, Content: "hi"})
	}
	msgs, dropped := s.drainQueue()
	if len(msgs) != 3 {
		t.Errorf("len(msgs) = %d, want 3", len(msgs))
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
}

func TestServer_PushFailureFallsBackToPullQueue(t *testing.T) {
	s := NewServer(ServerOptions{DeliveryMode: DeliveryPush, MaxBufferedMessages: 3})
	s.writer = errorWriter{err: io.ErrClosedPipe}

	s.PushMessage(protocol.BridgeMessage{ID: "m1", Source: protocol.SourceCodex, Content: "hello", Timestamp: time.Now().UnixMilli()})

	var out strings.Builder
	s.writer = &out
	s.handleGetMessagesTool(json.RawMessage(`1`))

	line := firstLine(out.String())
	if line == "" {
		t.Fatalf("no response written, got %q", out.String())
	}
	if !strings.Contains(out.String(), "[1 new message from Codex]") {
		t.Fatalf("expected queued message header, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Codex: hello") {
		t.Fatalf("expected queued message content, got %q", out.String())
	}
}

func TestServer_PushMessageBeforeServe_FlushesOnServe(t *testing.T) {
	var out safeBuffer
	s := NewServer(ServerOptions{DeliveryMode: DeliveryPush, MaxBufferedMessages: 2})
	s.PushMessage(protocol.BridgeMessage{ID: "m1", Source: protocol.SourceCodex, Content: "hello", Timestamp: time.Now().UnixMilli()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, io.NopCloser(strings.NewReader("")), &writeCloser{Writer: &out})
	}()

	select {
	case <-s.Ready():
	case <-time.After(time.Second):
		t.Fatal("server never became ready")
	}

	cancel()
	<-done

	line := firstLine(out.String())
	if line == "" {
		t.Fatalf("no notification written, got %q", out.String())
	}
	if !strings.Contains(line, `"method":"notifications/claude/channel"`) {
		t.Fatalf("expected channel notification, got %s", line)
	}
	if !strings.Contains(line, `"content":"hello"`) {
		t.Fatalf("expected buffered message content, got %s", line)
	}
}
