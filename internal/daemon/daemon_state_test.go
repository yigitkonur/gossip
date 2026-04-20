package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
	"github.com/yigitkonur/gossip/internal/statedir"
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

func TestDaemon_MessageTemplatesSupportOverrides(t *testing.T) {
	d := New(Options{ProxyPort: 4601})
	if got := d.currentReadyMessage("thread_123"); got != "✅ Codex bridge ready (thread thread_123)" {
		t.Fatalf("currentReadyMessage() = %q", got)
	}
	if got := d.currentWaitingMessage(); got != "⏳ Waiting for Codex TUI to connect. Run in another terminal:\ncodex --enable tui_app_server --remote ws://127.0.0.1:4601" {
		t.Fatalf("currentWaitingMessage() = %q", got)
	}

	d.SetReadyMessageTemplate("ready {thread_id}")
	d.SetWaitingMessageTemplate("waiting")

	if got := d.currentReadyMessage("thread_456"); got != "ready thread_456" {
		t.Fatalf("overridden currentReadyMessage() = %q", got)
	}
	if got := d.currentWaitingMessage(); got != "waiting" {
		t.Fatalf("overridden currentWaitingMessage() = %q", got)
	}
}

func TestDaemon_WritesAndRemovesPortsFile(t *testing.T) {
	sd := statedir.New(t.TempDir())
	d := New(Options{StateDir: sd, AppPort: 4600, ProxyPort: 4601, ControlPort: 4602})

	d.writeStatusFile()
	d.writePortsFile()

	if _, err := os.Stat(sd.StatusFile()); err != nil {
		t.Fatalf("status file missing: %v", err)
	}
	if _, err := os.Stat(sd.PortsFile()); err != nil {
		t.Fatalf("ports file missing: %v", err)
	}

	portsPayload, err := os.ReadFile(sd.PortsFile())
	if err != nil {
		t.Fatalf("read ports file: %v", err)
	}
	if string(portsPayload) != "{\n  \"controlPort\": 4602,\n  \"appPort\": 4600,\n  \"proxyPort\": 4601\n}\n" {
		t.Fatalf("unexpected ports payload: %q", string(portsPayload))
	}

	d.removeStatusFile()
	d.removePortsFile()
	if _, err := os.Stat(sd.StatusFile()); !os.IsNotExist(err) {
		t.Fatalf("status file still exists: %v", err)
	}
	if _, err := os.Stat(sd.PortsFile()); !os.IsNotExist(err) {
		t.Fatalf("ports file still exists: %v", err)
	}
}
