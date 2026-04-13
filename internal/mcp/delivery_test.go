package mcp

import (
	"testing"

	"github.com/raysonmeng/agent-bridge/internal/protocol"
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
