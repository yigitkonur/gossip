package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeSender struct {
	mu     sync.Mutex
	calls  []string
	err    string
	delay  time.Duration
	fail   bool
}

func (f *fakeSender) send(ctx context.Context, text string, requireReply bool) (bool, string) {
	f.mu.Lock()
	f.calls = append(f.calls, text)
	failLocal := f.fail
	errLocal := f.err
	delay := f.delay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if failLocal {
		return false, errLocal
	}
	return true, ""
}

func (f *fakeSender) callsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.calls...)
	return out
}

func waitReply(t *testing.T, ch <-chan BlockingReply, timeout time.Duration) (BlockingReply, bool) {
	t.Helper()
	select {
	case r := <-ch:
		return r, true
	case <-time.After(timeout):
		return BlockingReply{}, false
	}
}

func TestLoopQueue_BridgeNotReadyQueuesUntilDrain(t *testing.T) {
	snd := &fakeSender{}
	ready := false
	q := NewLoopQueue(snd.send, func() bool { return ready }, nil)
	_, ch := q.Enqueue(context.Background(), "hello", true, 5_000)

	time.Sleep(30 * time.Millisecond)
	if len(snd.callsCopy()) != 0 {
		t.Fatalf("sender invoked before bridge ready: %v", snd.callsCopy())
	}
	if q.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", q.PendingCount())
	}

	ready = true
	q.DrainForTUI(context.Background())
	time.Sleep(30 * time.Millisecond)

	if got := snd.callsCopy(); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("sender calls = %v, want [hello]", got)
	}
	q.OnAgentMessage("codex says hi")

	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok {
		t.Fatalf("reply not delivered")
	}
	if !r.Received || r.Text != "codex says hi" {
		t.Fatalf("reply = %+v, want received+text", r)
	}
	if q.PendingCount() != 0 {
		t.Fatalf("pending after resolve = %d, want 0", q.PendingCount())
	}
}

func TestLoopQueue_SenderFailureResolvesWithError(t *testing.T) {
	snd := &fakeSender{fail: true, err: "Codex is not ready"}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)

	_, ch := q.Enqueue(context.Background(), "x", true, 5_000)
	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok {
		t.Fatalf("no reply delivered")
	}
	if r.Received {
		t.Errorf("Received = true, want false on send failure")
	}
	if r.Error != "Codex is not ready" {
		t.Errorf("Error = %q, want %q", r.Error, "Codex is not ready")
	}
}

func TestLoopQueue_TimeoutWhenCodexSilent(t *testing.T) {
	snd := &fakeSender{}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)

	_, ch := q.Enqueue(context.Background(), "ping", true, 80) // 80 ms
	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok {
		t.Fatalf("no reply delivered")
	}
	if r.Received {
		t.Errorf("Received = true, want false on timeout")
	}
	if r.Error == "" {
		t.Errorf("Error empty on timeout")
	}
}

func TestLoopQueue_TurnCompletedWithoutReplyResolves(t *testing.T) {
	snd := &fakeSender{}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)

	_, ch := q.Enqueue(context.Background(), "ping", true, 5_000)
	time.Sleep(30 * time.Millisecond)
	q.OnTurnCompletedWithoutReply()

	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok {
		t.Fatalf("no reply delivered")
	}
	if r.Received {
		t.Errorf("Received = true, want false")
	}
}

func TestLoopQueue_SerializesTwoEnqueues(t *testing.T) {
	snd := &fakeSender{}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)

	_, ch1 := q.Enqueue(context.Background(), "first", true, 5_000)
	_, ch2 := q.Enqueue(context.Background(), "second", true, 5_000)
	time.Sleep(30 * time.Millisecond)

	calls := snd.callsCopy()
	if len(calls) != 1 || calls[0] != "first" {
		t.Fatalf("only first should have sent yet, got %v", calls)
	}
	if q.PendingCount() != 2 {
		t.Fatalf("pending = %d, want 2 (1 active + 1 queued)", q.PendingCount())
	}

	q.OnAgentMessage("reply to first")
	r1, ok := waitReply(t, ch1, 500*time.Millisecond)
	if !ok || r1.Text != "reply to first" {
		t.Fatalf("first reply = %+v, ok=%v", r1, ok)
	}

	time.Sleep(30 * time.Millisecond)
	calls = snd.callsCopy()
	if len(calls) != 2 || calls[1] != "second" {
		t.Fatalf("second should have sent after first resolved, got %v", calls)
	}

	q.OnAgentMessage("reply to second")
	r2, ok := waitReply(t, ch2, 500*time.Millisecond)
	if !ok || r2.Text != "reply to second" {
		t.Fatalf("second reply = %+v, ok=%v", r2, ok)
	}
}

func TestLoopQueue_DoubleResolveIsNoop(t *testing.T) {
	snd := &fakeSender{}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)
	_, ch := q.Enqueue(context.Background(), "ping", true, 5_000)
	time.Sleep(30 * time.Millisecond)

	q.OnAgentMessage("first")
	q.OnAgentMessage("second") // should be silently ignored (no active)

	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok || r.Text != "first" {
		t.Fatalf("first reply lost or overwritten: got %+v, ok=%v", r, ok)
	}
	// No second reply should arrive on this channel.
	select {
	case extra := <-ch:
		t.Fatalf("second reply leaked to channel: %+v", extra)
	case <-time.After(60 * time.Millisecond):
	}
}

// Ensure errors.Is preserves through the BlockingReply.Error string (sanity
// test that we aren't wrapping in a type that breaks strings.Contains).
func TestLoopQueue_ErrorTextPassthrough(t *testing.T) {
	const want = "daemon told us no"
	snd := &fakeSender{fail: true, err: want}
	q := NewLoopQueue(snd.send, func() bool { return true }, nil)
	_, ch := q.Enqueue(context.Background(), "x", true, 5_000)
	r, ok := waitReply(t, ch, 500*time.Millisecond)
	if !ok {
		t.Fatalf("no reply")
	}
	if r.Error != want {
		t.Errorf("Error = %q, want %q", r.Error, want)
	}
	// Silence errors import; ensure the test file compiles even without usage.
	_ = errors.New
	_ = fmt.Sprintf
}
