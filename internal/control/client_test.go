package control

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
