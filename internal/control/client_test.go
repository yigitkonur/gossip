package control

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestClient_RejectedDuplicateStopsReconnectLoop(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}}
	s := NewServer(h)
	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client1 := NewClient(ClientOptions{URL: wsURL})
	if err := client1.Connect(ctx); err != nil {
		t.Fatalf("client1 connect: %v", err)
	}
	defer client1.Disconnect()
	if err := client1.AttachClaude(ctx); err != nil {
		t.Fatalf("client1 attach: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		connects := h.connects
		h.mu.Unlock()
		if connects == 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	var rejected atomic.Bool
	rejectedCh := make(chan struct{}, 1)
	rejectedInfo := make(chan struct {
		code   int
		reason string
	}, 1)
	client2 := NewClient(ClientOptions{
		URL: wsURL,
		OnRejected: func(code int, reason string, uptime time.Duration) {
			rejected.Store(true)
			select {
			case rejectedInfo <- struct {
				code   int
				reason string
			}{code: code, reason: reason}:
			default:
			}
			select {
			case rejectedCh <- struct{}{}:
			default:
			}
		},
		ShouldReconnect: func() bool { return !rejected.Load() },
	})
	defer client2.Disconnect()

	done := make(chan error, 1)
	go func() {
		done <- client2.RunWithReconnect(ctx)
	}()

	select {
	case <-rejectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rejected signal")
	}

	info := <-rejectedInfo
	if info.code != int(CloseCodeReplaced) {
		t.Fatalf("rejection code = %d, want %d", info.code, CloseCodeReplaced)
	}
	if info.reason != "another Claude session is already connected" {
		t.Fatalf("rejection reason = %q", info.reason)
	}

	time.Sleep(250 * time.Millisecond)
	h.mu.Lock()
	connects := h.connects
	h.mu.Unlock()
	if connects != 1 {
		t.Fatalf("connect count = %d, want 1 active attach after rejection", connects)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithReconnect() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunWithReconnect to exit")
	}
}

func TestClient_SendReplyBlocking_RoundTrip(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}, blockReply: "codex says hi", blockOK: true}
	s := NewServer(h)
	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := NewClient(ClientOptions{URL: wsURL})
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Disconnect()
	if err := c.AttachClaude(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		attached := h.connects == 1
		h.mu.Unlock()
		if attached {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	text, received, errMsg := c.SendReplyBlocking(ctx, protocol.BridgeMessage{
		ID:      "blk_1",
		Source:  protocol.SourceClaude,
		Content: "review this",
	}, true, 2000)

	if !received {
		t.Errorf("received = false, want true")
	}
	if text != "codex says hi" {
		t.Errorf("text = %q, want %q", text, "codex says hi")
	}
	if errMsg != "" {
		t.Errorf("errMsg = %q, want empty", errMsg)
	}
	h.mu.Lock()
	got := len(h.blocking)
	h.mu.Unlock()
	if got != 1 {
		t.Errorf("handler saw %d blocking sends, want 1", got)
	}
}

func TestReconnectBackoff_IsExponentialAndCapped(t *testing.T) {
	tests := []struct {
		attempt int
		max     time.Duration
		want    time.Duration
	}{
		{attempt: 0, max: 500 * time.Millisecond, want: 500 * time.Millisecond},
		{attempt: 0, max: 30 * time.Second, want: time.Second},
		{attempt: 1, max: 30 * time.Second, want: 2 * time.Second},
		{attempt: 2, max: 30 * time.Second, want: 4 * time.Second},
		{attempt: 5, max: 30 * time.Second, want: 30 * time.Second},
		{attempt: 8, max: 45 * time.Second, want: 30 * time.Second},
	}

	for _, tt := range tests {
		if got := reconnectBackoff(tt.attempt, tt.max); got != tt.want {
			t.Fatalf("reconnectBackoff(%d, %s) = %s, want %s", tt.attempt, tt.max, got, tt.want)
		}
	}
}

func TestReconnectCooldown_UsesThirtySecondWindowWithinMaxBackoff(t *testing.T) {
	tests := []struct {
		name string
		max  time.Duration
		want time.Duration
	}{
		{name: "smaller ceiling wins", max: 5 * time.Second, want: 5 * time.Second},
		{name: "default ceiling stays at thirty seconds", max: 30 * time.Second, want: 30 * time.Second},
		{name: "larger ceiling still waits thirty seconds", max: 45 * time.Second, want: 30 * time.Second},
	}

	for _, tt := range tests {
		if got := reconnectCooldown(tt.max); got != tt.want {
			t.Fatalf("%s: reconnectCooldown(%s) = %s, want %s", tt.name, tt.max, got, tt.want)
		}
	}
}

func TestClient_RunWithReconnect_StopsAfterDisconnectWhenGateCloses(t *testing.T) {
	h := &testHandler{status: Status{BridgeReady: true}}
	s := NewServer(h)
	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var allowReconnect atomic.Bool
	allowReconnect.Store(true)

	var client *Client
	client = NewClient(ClientOptions{
		URL:        wsURL,
		MaxBackoff: 10 * time.Millisecond,
		OnStatus: func(Status) {
			allowReconnect.Store(false)
			client.Disconnect()
		},
		ShouldReconnect: func() bool { return allowReconnect.Load() },
	})

	done := make(chan error, 1)
	go func() {
		done <- client.RunWithReconnect(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithReconnect() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunWithReconnect to exit after disconnect")
	}

	time.Sleep(50 * time.Millisecond)
	h.mu.Lock()
	connects := h.connects
	h.mu.Unlock()
	if connects != 1 {
		t.Fatalf("connect count = %d, want 1", connects)
	}
}
