package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDialer_ConnectsAndReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","method":"turn/started","params":{}}`))
		time.Sleep(100 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgs := make(chan []byte, 1)
	d := NewDialer(wsURL, DialerOptions{
		OnMessage: func(b []byte) { msgs <- b },
	})
	if err := d.ConnectOnce(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer d.Close()

	select {
	case m := <-msgs:
		if !strings.Contains(string(m), `"turn/started"`) {
			t.Errorf("unexpected message: %s", m)
		}
	case <-ctx.Done():
		t.Fatal("no message received")
	}
}
