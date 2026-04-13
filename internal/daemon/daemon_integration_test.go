package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/raysonmeng/agent-bridge/internal/control"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
)

func TestDaemon_FullRoundTrip_RealCodex(t *testing.T) {
	if os.Getenv("CODEX_E2E") != "1" {
		t.Skip("set CODEX_E2E=1 to run with a real codex binary")
	}

	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	d := New(Options{StateDir: sd, AppPort: 45510, ProxyPort: 45511, ControlPort: 45512, Logger: func(msg string) { t.Log(msg) }})
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	time.Sleep(2 * time.Second)

	tuiConn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:45511", nil)
	if err != nil {
		t.Fatalf("proxy connect: %v", err)
	}
	defer tuiConn.Close(websocket.StatusNormalClosure, "")

	cc := control.NewClient(control.ClientOptions{URL: "ws://127.0.0.1:45512/ws", Logger: func(msg string) { t.Log(msg) }})
	if err := cc.Connect(ctx); err != nil {
		t.Fatalf("control connect: %v", err)
	}
	if err := cc.AttachClaude(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	success, errMsg := cc.SendReply(ctx, protocol.BridgeMessage{ID: "test_1", Source: protocol.SourceClaude, Content: "say hello", Timestamp: time.Now().UnixMilli()}, false)
	if !success {
		t.Errorf("send reply failed: %s", errMsg)
	}

	cancel()
	<-errCh
}
