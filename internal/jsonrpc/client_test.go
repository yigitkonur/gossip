package jsonrpc

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

// loopback is a Writer that captures messages and routes them back via a channel.
type loopback struct {
	mu   sync.Mutex
	sent [][]byte
	ch   chan []byte
}

func newLoopback() *loopback { return &loopback{ch: make(chan []byte, 16)} }

func (l *loopback) WriteJSON(_ context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.sent = append(l.sent, b)
	l.mu.Unlock()
	l.ch <- b
	return nil
}

func (l *loopback) Close() error { return nil }

func TestClient_Call_HappyPath(t *testing.T) {
	lb := newLoopback()
	c := NewClient(lb)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var gotResult json.RawMessage
	var callErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		gotResult, callErr = c.Call(ctx, "thread/start", map[string]any{"cwd": "/tmp"})
	}()

	sent := <-lb.ch
	var env protocol.Envelope
	if err := json.Unmarshal(sent, &env); err != nil {
		t.Fatalf("unmarshal sent: %v", err)
	}
	reply := append([]byte(`{"jsonrpc":"2.0","id":`), env.ID...)
	reply = append(reply, []byte(`,"result":{"thread":{"id":"t1"}}}`)...)
	c.HandleIncoming(reply)

	<-done
	if callErr != nil {
		t.Fatalf("Call returned error: %v", callErr)
	}
	if string(gotResult) != `{"thread":{"id":"t1"}}` {
		t.Errorf("result = %s", gotResult)
	}
}

func TestClient_ServerRequest_RoundTrip(t *testing.T) {
	lb := newLoopback()
	c := NewClient(lb)

	c.HandleIncoming([]byte(`{"jsonrpc":"2.0","id":99,"method":"item/permissions/requestApproval","params":{"scope":"write"}}`))

	select {
	case req := <-c.ServerRequests():
		if req.Method != "item/permissions/requestApproval" {
			t.Errorf("method = %q", req.Method)
		}
		if err := c.Respond(context.Background(), req.ID, map[string]string{"decision": "accept"}); err != nil {
			t.Fatalf("Respond: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("no server request received")
	}

	sent := <-lb.ch
	var env protocol.Envelope
	if err := json.Unmarshal(sent, &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if string(env.ID) != "99" {
		t.Errorf("response ID = %s, want 99", env.ID)
	}
	if string(env.Result) != `{"decision":"accept"}` {
		t.Errorf("response result = %s", env.Result)
	}
}

func TestClient_Call_UsesNegativeIDs(t *testing.T) {
	lb := newLoopback()
	c := NewClient(lb)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "thread/start", map[string]any{"cwd": "/tmp"})
		done <- err
	}()

	sent := <-lb.ch
	var env protocol.Envelope
	if err := json.Unmarshal(sent, &env); err != nil {
		t.Fatalf("unmarshal sent: %v", err)
	}
	if string(env.ID) != "-1" {
		t.Fatalf("request id = %s, want -1", env.ID)
	}
	reply := append([]byte(`{"jsonrpc":"2.0","id":`), env.ID...)
	reply = append(reply, []byte(`,"result":{"thread":{"id":"t1"}}}`)...)
	c.HandleIncoming(reply)
	if err := <-done; err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
}

func TestClient_HandleIncomingAfterClose_DoesNotPanic(t *testing.T) {
	lb := newLoopback()
	c := NewClient(lb)
	if err := c.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HandleIncoming panicked after Close: %v", r)
		}
	}()

	c.HandleIncoming([]byte(`{"jsonrpc":"2.0","method":"item/permissions/requestApproval","id":1,"params":{}}`))
	c.HandleIncoming([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	c.HandleIncoming([]byte(`{"jsonrpc":"2.0","id":-1,"result":{}}`))
}
