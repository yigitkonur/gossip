package codex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

// fakeAppServer implements the bare minimum of the Codex app-server protocol for integration tests.
type fakeAppServer struct {
	srv *httptest.Server

	mu    sync.Mutex
	turns []string
}

func newFakeAppServer(t *testing.T) *fakeAppServer {
	f := &fakeAppServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			w.WriteHeader(http.StatusOK)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		for {
			_, payload, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if err := json.Unmarshal(payload, &env); err != nil {
				continue
			}
			switch env.Method {
			case "initialize":
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":`+string(env.ID)+`,"result":{}}`))
			case "thread/start":
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":`+string(env.ID)+`,"result":{"thread":{"id":"fake_thread"}}}`))
			case "turn/start":
				var p protocol.TurnStartParams
				_ = json.Unmarshal(env.Params, &p)
				f.mu.Lock()
				if len(p.Input) > 0 {
					f.turns = append(f.turns, p.Input[0].Text)
				}
				f.mu.Unlock()
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":`+string(env.ID)+`,"result":{"turn":{"id":"turn_1"}}}`))
				time.Sleep(50 * time.Millisecond)
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"fake_thread","turnId":"turn_1"}}`))
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAppServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http")
}

func (f *fakeAppServer) sentTurns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.turns...)
}
