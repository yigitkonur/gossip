package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
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
	if got := d.currentReadyMessage("thread_123"); got != "✅ Codex TUI connected (thread_123). Bridge ready." {
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

func TestDaemon_ShouldEmitAttachStatus_UsesCooldownAndBufferedGate(t *testing.T) {
	d := New(Options{})
	now := time.Unix(100, 0)
	if !d.shouldEmitAttachStatus(now, 0) {
		t.Fatal("first attach should emit status")
	}
	if d.shouldEmitAttachStatus(now.Add(10*time.Second), 0) {
		t.Fatal("rapid reattach should not emit status")
	}
	if d.shouldEmitAttachStatus(now.Add(40*time.Second), 1) {
		t.Fatal("buffered replay should suppress attach status")
	}
	if !d.shouldEmitAttachStatus(now.Add(80*time.Second), 0) {
		t.Fatal("attach after cooldown with no buffered messages should emit status")
	}
}

func TestDaemon_OnClaudeConnect_AttachStatusCooldownSuppressesRapidReattach(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.tuiState.MarkBridgeReady()
	d.tuiState.HandleTUIConnected(1)

	srv := httptest.NewServer(d.control.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conn1 := attachControlClient(t, wsURL)
	messages := readControlMessages(t, conn1, 250*time.Millisecond)
	if !containsSystemMessage(messages, "system_ready") {
		t.Fatalf("first attach messages = %+v, want system_ready", messages)
	}
	_ = conn1.Close(websocket.StatusNormalClosure, "")
	waitForClaudeDetach(t, d)

	conn2 := attachControlClient(t, wsURL)
	messages = readControlMessages(t, conn2, 250*time.Millisecond)
	if containsSystemMessage(messages, "system_ready") || containsSystemMessage(messages, "system_waiting") {
		t.Fatalf("rapid reattach should suppress attach status, got %+v", messages)
	}
	_ = conn2.Close(websocket.StatusNormalClosure, "")
	waitForClaudeDetach(t, d)

	d.stateMu.Lock()
	d.lastAttachStatusSent = time.Now().Add(-(attachStatusCooldown + time.Second))
	d.stateMu.Unlock()

	conn3 := attachControlClient(t, wsURL)
	defer conn3.Close(websocket.StatusNormalClosure, "")
	messages = readControlMessages(t, conn3, 250*time.Millisecond)
	if !containsSystemMessage(messages, "system_ready") {
		t.Fatalf("attach after cooldown messages = %+v, want system_ready", messages)
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

func attachControlClient(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial control websocket: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"claude_connect"}`)); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
		t.Fatalf("attach control websocket: %v", err)
	}
	return conn
}

func readControlMessages(t *testing.T, conn *websocket.Conn, window time.Duration) []control.ServerMessage {
	t.Helper()
	deadline := time.Now().Add(window)
	messages := make([]control.ServerMessage, 0)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, payload, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			continue
		}
		var msg control.ServerMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}

func containsSystemMessage(messages []control.ServerMessage, prefix string) bool {
	for _, msg := range messages {
		if msg.Type != control.ServerMsgCodexToClaude || msg.Message == nil {
			continue
		}
		if strings.HasPrefix(msg.Message.ID, prefix+"_") {
			return true
		}
	}
	return false
}

func waitForClaudeDetach(t *testing.T, d *Daemon) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.stateMu.Lock()
		attached := d.claudeAttached
		d.stateMu.Unlock()
		if !attached {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Claude detach")
}
