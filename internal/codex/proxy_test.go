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
