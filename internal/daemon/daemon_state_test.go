package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestDaemon_StateHandlers_NoDataRace(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})
	d.codex = codex.NewClient(codex.ClientOptions{})
	d.tuiState.MarkBridgeReady()
	d.tuiState.HandleTUIConnected(1)
	d.replyRequired = true

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); d.OnClaudeConnect() }()
		go func() { defer wg.Done(); d.OnClaudeDisconnect("test") }()
		go func(i int) {
			defer wg.Done()
			d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventAgentMessage, ThreadID: "t", TurnID: fmt.Sprintf("%d", i), Text: "hello"})
		}(i)
		go func(i int) {
			defer wg.Done()
			d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: fmt.Sprintf("%d", i)})
		}(i)
	}
	wg.Wait()
}

func TestDaemon_AttentionWindowPausesStatusFlushesUntilExpiry(t *testing.T) {
	flushed := make(chan protocol.BridgeMessage, 1)
	d := New(Options{FilterMode: filter.ModeFiltered, AttentionWindow: 50 * time.Millisecond})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(msg protocol.BridgeMessage) { flushed <- msg }, filter.StatusBufferOptions{FlushThreshold: 3, FlushTimeout: time.Hour})
	d.startAttentionWindow(42)

	for i := 0; i < 3; i++ {
		d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventAgentMessage, ThreadID: "thread_1", TurnID: fmt.Sprintf("turn_%d", i), Text: "[STATUS] compiling"})
	}

	select {
	case msg := <-flushed:
		t.Fatalf("STATUS should stay buffered during attention window, got %q", msg.Content)
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case msg := <-flushed:
		if msg.Content == "" {
			t.Fatal("expected flushed STATUS summary after attention window expiry")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for STATUS flush after attention window expiry")
	}
}
