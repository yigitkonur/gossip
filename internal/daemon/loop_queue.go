package daemon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BlockingReply is what a queued Claude→Codex send eventually resolves to.
// Received is false when the send failed, timed out, or the Codex turn
// completed without an [IMPORTANT]-marked agentMessage.
type BlockingReply struct {
	Text     string
	Received bool
	Error    string
}

// LoopQueue serializes Claude→Codex blocking sends used by the completion
// loop hook. It absorbs a burst of enqueues while the Codex TUI is not yet
// attachable, pumps them one at a time once the bridge is ready, and
// correlates the next [IMPORTANT] agentMessage (or the turn-completed
// signal) back to the waiting caller.
//
// It is in-memory only by design: the daemon is a process singleton and the
// blocking send's caller (hook or `gossip bridge send`) holds a live
// WebSocket to the daemon; if the daemon crashes, the caller's WS dies too
// and the enqueued slot cannot be resumed from disk anyway.
type LoopQueue struct {
	sender      func(ctx context.Context, text string, requireReply bool) (bool, string)
	bridgeReady func() bool
	log         func(msg string)

	mu     sync.Mutex
	queue  []*blockingSend
	active *blockingSend

	nextSeq atomic.Int64
}

type blockingSend struct {
	requestID    string
	text         string
	requireReply bool
	waitMs       int
	replyCh      chan BlockingReply

	// all three guarded by LoopQueue.mu
	resolved bool
	timer    *time.Timer
	deadline time.Time
}

// NewLoopQueue constructs a queue. sender injects a message into Codex
// (typically backed by Daemon.OnClaudeToCodex). bridgeReady reports whether
// the Codex TUI is attached and a thread exists. Logger is optional.
func NewLoopQueue(sender func(ctx context.Context, text string, requireReply bool) (bool, string), bridgeReady func() bool, log func(string)) *LoopQueue {
	if log == nil {
		log = func(string) {}
	}
	return &LoopQueue{sender: sender, bridgeReady: bridgeReady, log: log}
}

// Enqueue adds a blocking send and returns a channel that will fire exactly
// once with the resolved Reply. waitMs caps how long the caller is willing
// to wait for Codex to respond; ≤0 falls back to 90 seconds.
func (q *LoopQueue) Enqueue(ctx context.Context, text string, requireReply bool, waitMs int) (string, <-chan BlockingReply) {
	if waitMs <= 0 {
		waitMs = 90_000
	}
	seq := q.nextSeq.Add(1)
	bs := &blockingSend{
		requestID:    fmt.Sprintf("blk_%d_%d", time.Now().UnixMilli(), seq),
		text:         text,
		requireReply: requireReply,
		waitMs:       waitMs,
		replyCh:      make(chan BlockingReply, 1),
	}
	q.mu.Lock()
	q.queue = append(q.queue, bs)
	q.mu.Unlock()
	go q.pump(ctx)
	return bs.requestID, bs.replyCh
}

// PendingCount returns queued + active sends awaiting resolution.
func (q *LoopQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.queue)
	if q.active != nil {
		n++
	}
	return n
}

// DrainForTUI signals that the bridge became ready; triggers a pump attempt.
func (q *LoopQueue) DrainForTUI(ctx context.Context) { go q.pump(ctx) }

// OnAgentMessage resolves the active send with the supplied text. The caller
// decides which messages qualify (typically the first [IMPORTANT]-marked
// Codex agentMessage during a requireReply turn).
func (q *LoopQueue) OnAgentMessage(text string) {
	q.mu.Lock()
	bs := q.active
	if bs == nil {
		q.mu.Unlock()
		return
	}
	q.resolveLocked(bs, BlockingReply{Text: text, Received: true})
	q.mu.Unlock()
	go q.pump(context.Background())
}

// OnTurnCompletedWithoutReply resolves the active send as "no reply" when a
// Codex turn completed but no [IMPORTANT] agentMessage was observed.
func (q *LoopQueue) OnTurnCompletedWithoutReply() {
	q.mu.Lock()
	bs := q.active
	if bs == nil {
		q.mu.Unlock()
		return
	}
	q.resolveLocked(bs, BlockingReply{Received: false, Error: "Codex turn completed without [IMPORTANT] reply"})
	q.mu.Unlock()
	go q.pump(context.Background())
}

// pump promotes the head of the queue to active and calls sender, as long
// as there is no active send, there is something queued, and the bridge is
// ready. Safe to invoke concurrently — only one pump succeeds per call.
func (q *LoopQueue) pump(ctx context.Context) {
	q.mu.Lock()
	if q.active != nil || len(q.queue) == 0 || !q.bridgeReady() {
		q.mu.Unlock()
		return
	}
	bs := q.queue[0]
	q.queue = q.queue[1:]
	q.active = bs
	bs.deadline = time.Now().Add(time.Duration(bs.waitMs) * time.Millisecond)
	bs.timer = time.AfterFunc(time.Duration(bs.waitMs)*time.Millisecond, func() {
		q.onDeadline(bs.requestID)
	})
	q.mu.Unlock()

	ok, errMsg := q.sender(ctx, bs.text, bs.requireReply)
	if !ok {
		q.mu.Lock()
		q.resolveLocked(bs, BlockingReply{Received: false, Error: errMsg})
		q.mu.Unlock()
		go q.pump(context.Background())
	}
}

func (q *LoopQueue) onDeadline(requestID string) {
	q.mu.Lock()
	bs := q.active
	if bs == nil || bs.requestID != requestID {
		q.mu.Unlock()
		return
	}
	q.resolveLocked(bs, BlockingReply{Received: false, Error: fmt.Sprintf("timed out after %d ms waiting for Codex reply", bs.waitMs)})
	q.mu.Unlock()
	go q.pump(context.Background())
}

// resolveLocked must be called with q.mu held. Guarantees the replyCh
// receives at most one value and cleans up timer + active state.
func (q *LoopQueue) resolveLocked(bs *blockingSend, r BlockingReply) {
	if bs.resolved {
		return
	}
	bs.resolved = true
	if bs.timer != nil {
		bs.timer.Stop()
	}
	if q.active == bs {
		q.active = nil
	}
	// cap==1 + resolved guard → non-blocking send.
	select {
	case bs.replyCh <- r:
	default:
	}
}
