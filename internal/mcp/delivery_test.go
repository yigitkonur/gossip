package mcp

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

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
