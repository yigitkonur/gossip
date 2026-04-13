package codex

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

func TestProxy_AcceptsConnection(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if got := p.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount = %d, want 1", got)
	}
}

func TestRewriteRestoreID_RoundTrip(t *testing.T) {
	orig := json.RawMessage(`42`)
	msg := []byte(`{"jsonrpc":"2.0","id":42,"method":"turn/start","params":{}}`)
	rewritten, err := rewriteID(msg, 100099)
	if err != nil {
		t.Fatal(err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(rewritten, &env); err != nil {
		t.Fatal(err)
	}
	if string(env.ID) != "100099" {
		t.Errorf("rewritten id = %s", env.ID)
	}
	resp := []byte(`{"jsonrpc":"2.0","id":100099,"result":{"ok":true}}`)
	restored, err := restoreID(resp, orig)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(restored, &env); err != nil {
		t.Fatal(err)
	}
	if string(env.ID) != "42" {
		t.Errorf("restored id = %s, want 42", env.ID)
	}
}

func TestProxy_ServerRequestBufferedUntilCurrentTUIConnects(t *testing.T) {
	p := NewProxy(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":7,"method":"item/fileChange/requestApproval","params":{}}`))

	srv := httptest.NewServer(p)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read buffered approval: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Method != "item/fileChange/requestApproval" {
		t.Fatalf("method = %q", env.Method)
	}
}

func TestProxy_OnlyCurrentTUIReceivesUpstreamNotification(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
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

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","method":"turn/started","params":{"turnId":"t1"}}`))

	readCtx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	_, payload2, err := conn2.Read(readCtx2)
	if err != nil {
		t.Fatalf("current connection read failed: %v", err)
	}
	if !strings.Contains(string(payload2), `"turn/started"`) {
		t.Fatalf("unexpected payload: %s", payload2)
	}

	readCtx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()
	_, _, err = conn1.Read(readCtx1)
	if err == nil {
		t.Fatal("stale connection unexpectedly received upstream notification")
	}
}
