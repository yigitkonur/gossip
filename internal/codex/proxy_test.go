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

func readEnvelope(t *testing.T, conn *websocket.Conn, timeout time.Duration) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
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

func TestProxy_DefersBufferedServerRequestsUntilThreadResume(t *testing.T) {
	p := NewProxy(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":7,"method":"item/fileChange/requestApproval","params":{"threadId":"thread-A","file":"test.ts"}}`))

	srv := httptest.NewServer(p)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	p.mu.Lock()
	pendingBeforeResume := len(p.pendingRequests)
	serverBeforeResume := len(p.serverRequests)
	p.mu.Unlock()
	if pendingBeforeResume != 1 {
		t.Fatalf("pendingRequests before resume = %d, want 1", pendingBeforeResume)
	}
	if serverBeforeResume != 0 {
		t.Fatalf("serverRequests before resume = %d, want 0", serverBeforeResume)
	}

	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}
	p.trackPendingClientRequest(100000, current.id, protocol.MethodThreadResume, json.RawMessage(`{"threadId":"thread-A"}`))
	p.completePendingClientRequest(100000, protocol.Envelope{
		Result: json.RawMessage(`{"thread":{"id":"thread-A"}}`),
	})

	env := readEnvelope(t, conn, time.Second)
	if env.Method != "item/fileChange/requestApproval" {
		t.Fatalf("method = %q", env.Method)
	}
	if string(env.ID) == "7" {
		t.Fatalf("replayed id = %s, want rewritten proxy id", env.ID)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendingRequests) != 0 {
		t.Fatalf("pendingRequests = %d, want 0", len(p.pendingRequests))
	}
	if len(p.serverRequests) != 1 {
		t.Fatalf("serverRequests = %d, want 1", len(p.serverRequests))
	}
}

func TestProxy_ThreadResumeDropsMismatchedBufferedServerRequests(t *testing.T) {
	p := NewProxy(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":8,"method":"item/commandExecution/requestApproval","params":{"threadId":"thread-A","command":"ls"}}`))

	srv := httptest.NewServer(p)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	p.mu.Lock()
	pendingBeforeMismatch := len(p.pendingRequests)
	serverBeforeMismatch := len(p.serverRequests)
	p.mu.Unlock()
	if pendingBeforeMismatch != 1 {
		t.Fatalf("pendingRequests before mismatched resume = %d, want 1", pendingBeforeMismatch)
	}
	if serverBeforeMismatch != 0 {
		t.Fatalf("serverRequests before mismatched resume = %d, want 0", serverBeforeMismatch)
	}

	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}
	p.trackPendingClientRequest(100000, current.id, protocol.MethodThreadResume, json.RawMessage(`{"threadId":"thread-B"}`))
	p.completePendingClientRequest(100000, protocol.Envelope{
		Result: json.RawMessage(`{"thread":{"id":"thread-B"}}`),
	})

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendingRequests) != 0 {
		t.Fatalf("pendingRequests = %d, want 0", len(p.pendingRequests))
	}
	if len(p.serverRequests) != 0 {
		t.Fatalf("serverRequests = %d, want 0", len(p.serverRequests))
	}
}

func TestProxy_ThreadStartClearsBufferedServerRequests(t *testing.T) {
	p := NewProxy(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":9,"method":"item/permissions/requestApproval","params":{"threadId":"thread-A","permission":"network"}}`))

	srv := httptest.NewServer(p)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitForConnectionCount(t, p, 1)
	p.mu.Lock()
	pendingBeforeStart := len(p.pendingRequests)
	serverBeforeStart := len(p.serverRequests)
	p.mu.Unlock()
	if pendingBeforeStart != 1 {
		t.Fatalf("pendingRequests before thread/start = %d, want 1", pendingBeforeStart)
	}
	if serverBeforeStart != 0 {
		t.Fatalf("serverRequests before thread/start = %d, want 0", serverBeforeStart)
	}

	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected primary TUI connection")
	}
	p.trackPendingClientRequest(100001, current.id, protocol.MethodThreadStart, nil)
	p.completePendingClientRequest(100001, protocol.Envelope{
		Result: json.RawMessage(`{"thread":{"id":"thread-new"}}`),
	})

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendingRequests) != 0 {
		t.Fatalf("pendingRequests = %d, want 0", len(p.pendingRequests))
	}
	if len(p.serverRequests) != 0 {
		t.Fatalf("serverRequests = %d, want 0", len(p.serverRequests))
	}
}

func TestProxy_RequeuesInFlightServerRequestsOnDisconnectUntilThreadResume(t *testing.T) {
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
	waitForConnectionCount(t, p, 1)

	p.HandleUpstreamMessage(ctx, []byte(`{"jsonrpc":"2.0","id":70,"method":"item/permissions/requestApproval","params":{"threadId":"thread-A","permission":"network"}}`))

	first := readEnvelope(t, conn1, time.Second)
	if first.Method != "item/permissions/requestApproval" {
		t.Fatalf("method = %q", first.Method)
	}
	if err := conn1.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close primary: %v", err)
	}
	waitForConnectionCount(t, p, 0)

	p.mu.Lock()
	pendingAfterDisconnect := len(p.pendingRequests)
	serverAfterDisconnect := len(p.serverRequests)
	p.mu.Unlock()
	if pendingAfterDisconnect != 1 {
		t.Fatalf("pendingRequests after disconnect = %d, want 1", pendingAfterDisconnect)
	}
	if serverAfterDisconnect != 0 {
		t.Fatalf("serverRequests after disconnect = %d, want 0", serverAfterDisconnect)
	}

	conn2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")
	waitForConnectionCount(t, p, 1)
	p.mu.Lock()
	pendingBeforeResume := len(p.pendingRequests)
	serverBeforeResume := len(p.serverRequests)
	p.mu.Unlock()
	if pendingBeforeResume != 1 {
		t.Fatalf("pendingRequests before reconnect resume = %d, want 1", pendingBeforeResume)
	}
	if serverBeforeResume != 0 {
		t.Fatalf("serverRequests before reconnect resume = %d, want 0", serverBeforeResume)
	}

	current, ok := p.currentConn()
	if !ok {
		t.Fatal("expected replacement primary TUI connection")
	}
	p.trackPendingClientRequest(100002, current.id, protocol.MethodThreadResume, json.RawMessage(`{"threadId":"thread-A"}`))
	p.completePendingClientRequest(100002, protocol.Envelope{
		Result: json.RawMessage(`{"thread":{"id":"thread-A"}}`),
	})

	replayed := readEnvelope(t, conn2, time.Second)
	if replayed.Method != "item/permissions/requestApproval" {
		t.Fatalf("method = %q", replayed.Method)
	}
	if string(replayed.ID) == "70" {
		t.Fatalf("replayed id = %s, want rewritten proxy id", replayed.ID)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendingRequests) != 0 {
		t.Fatalf("pendingRequests after replay = %d, want 0", len(p.pendingRequests))
	}
	if len(p.serverRequests) != 1 {
		t.Fatalf("serverRequests after replay = %d, want 1", len(p.serverRequests))
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
