package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
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

	waitForConnectionCount(t, p, 1)
}

func waitForConnectionCount(t *testing.T, p *Proxy, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := p.ConnectionCount(); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := p.ConnectionCount(); got != want {
		t.Fatalf("ConnectionCount = %d, want %d", got, want)
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

func TestProxy_ClientResponseRestoresOriginalRequestID(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}

	p.forwardToUpstream(ctx, current, []byte(`{"jsonrpc":"2.0","id":42,"method":"thread/list","params":{}}`))
	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":100000,"result":{"threads":[]}}`))

	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal restored response: %v", err)
	}
	if string(env.ID) != "42" {
		t.Fatalf("restored response id = %s, want 42", env.ID)
	}
}

func TestProxy_ThreadResumeDropsOrphanPendingClientRequests(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}

	p.forwardToUpstream(ctx, current, []byte(`{"jsonrpc":"2.0","id":10,"method":"turn/start","params":{"threadId":"thread-old"}}`))
	p.forwardToUpstream(ctx, current, []byte(`{"jsonrpc":"2.0","id":11,"method":"thread/resume","params":{"threadId":"thread-new"}}`))

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":100001,"result":{"thread":{"id":"thread-new"}}}`))

	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read thread/resume response: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal thread/resume response: %v", err)
	}
	if string(env.ID) != "11" {
		t.Fatalf("thread/resume response id = %s, want 11", env.ID)
	}

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":100000,"result":{"turn":{"id":"turn_1"}}}`))

	readCtx, cancelRead := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelRead()
	_, _, err = conn.Read(readCtx)
	if err == nil {
		t.Fatal("orphan turn/start response unexpectedly reached the client after thread/resume")
	}
}

func TestProxy_PatchesRateLimitsErrorsWithMockSuccess(t *testing.T) {
	p := NewProxy(nil)
	srv := httptest.NewServer(p)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}

	p.forwardToUpstream(ctx, current, []byte(`{"jsonrpc":"2.0","id":21,"method":"thread/list","params":{}}`))
	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":100000,"error":{"message":"startup rateLimits unavailable"}}`))

	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read patched response: %v", err)
	}

	var decoded struct {
		ID     int64 `json:"id"`
		Result struct {
			RateLimits struct {
				LimitID   any `json:"limitId"`
				LimitName any `json:"limitName"`
				Primary   struct {
					UsedPercent        int `json:"usedPercent"`
					WindowDurationMins int `json:"windowDurationMins"`
				} `json:"primary"`
			} `json:"rateLimits"`
			RateLimitsByLimitID any `json:"rateLimitsByLimitId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal patched response: %v", err)
	}
	if decoded.ID != 21 {
		t.Fatalf("patched response id = %d, want 21", decoded.ID)
	}
	if decoded.Result.RateLimits.Primary.UsedPercent != 0 || decoded.Result.RateLimits.Primary.WindowDurationMins != 60 {
		t.Fatalf("unexpected patched primary rate limits: %+v", decoded.Result.RateLimits.Primary)
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

func TestProxy_PrimaryTUIKeepsUpstreamOwnershipWhenPickerConnects(t *testing.T) {
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

	readCtx1, cancel1 := context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	_, payload1, err := conn1.Read(readCtx1)
	if err != nil {
		t.Fatalf("primary connection read failed: %v", err)
	}
	if !strings.Contains(string(payload1), `"turn/started"`) {
		t.Fatalf("unexpected payload: %s", payload1)
	}

	readCtx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	_, _, err = conn2.Read(readCtx2)
	if err == nil {
		t.Fatal("picker connection unexpectedly received primary upstream notification")
	}
}

func TestProxy_PickerConnectionGetsDedicatedAppServerSocket(t *testing.T) {
	var appServerConnections atomic.Int32
	accepted := make(chan struct{}, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		appServerConnections.Add(1)
		select {
		case accepted <- struct{}{}:
		default:
		}
		go func() {
			defer conn.Close(websocket.StatusNormalClosure, "")
			_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"jsonrpc":"2.0","method":"picker/ready","params":{}}`))
			for {
				if _, _, err := conn.Read(context.Background()); err != nil {
					return
				}
			}
		}()
	})

	appSrv := &http.Server{Handler: mux}
	go appSrv.Serve(ln)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = appSrv.Shutdown(shutdownCtx)
	})

	port := ln.Addr().(*net.TCPAddr).Port
	p := NewProxy(&Client{proc: NewProcess(ProcessOptions{Port: port})})
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

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for picker app-server websocket")
	}

	if got := appServerConnections.Load(); got != 1 {
		t.Fatalf("app-server connection count = %d, want 1 dedicated picker socket", got)
	}

	readCtx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	_, payload2, err := conn2.Read(readCtx2)
	if err != nil {
		t.Fatalf("picker read failed: %v", err)
	}
	if !strings.Contains(string(payload2), `"picker/ready"`) {
		t.Fatalf("unexpected picker payload: %s", payload2)
	}

	readCtx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()
	_, _, err = conn1.Read(readCtx1)
	if err == nil {
		t.Fatal("primary connection unexpectedly received picker app-server traffic")
	}
}
