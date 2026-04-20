package filter

import (
	"fmt"
	"sync"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

// StatusBufferOptions tunes a StatusBuffer.
type StatusBufferOptions struct {
	FlushThreshold int
	FlushTimeout   time.Duration
}

// StatusBuffer accumulates [STATUS] messages and flushes them on threshold or timeout.
type StatusBuffer struct {
	onFlush        func(summary protocol.BridgeMessage)
	flushThreshold int
	flushTimeout   time.Duration

	mu                 sync.Mutex
	queue              []protocol.BridgeMessage
	timer              *time.Timer
	timerDeadline      time.Time
	paused             bool
	pendingFlushReason string
}

// NewStatusBuffer returns a new StatusBuffer.
func NewStatusBuffer(onFlush func(protocol.BridgeMessage), opts StatusBufferOptions) *StatusBuffer {
	if opts.FlushThreshold == 0 {
		opts.FlushThreshold = 3
	}
	if opts.FlushTimeout == 0 {
		opts.FlushTimeout = 15 * time.Second
	}
	return &StatusBuffer{onFlush: onFlush, flushThreshold: opts.FlushThreshold, flushTimeout: opts.FlushTimeout}
}

// Size returns the current queue length.
func (b *StatusBuffer) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// Add enqueues a message and auto-flushes if over threshold.
func (b *StatusBuffer) Add(msg protocol.BridgeMessage) {
	b.mu.Lock()
	b.queue = append(b.queue, msg)
	size := len(b.queue)
	paused := b.paused
	if paused && size >= b.flushThreshold {
		b.pendingFlushReason = "threshold reached after resume"
	}
	b.mu.Unlock()
	if !paused {
		b.resetTimer()
		if size >= b.flushThreshold {
			b.Flush("threshold reached")
		}
	}
}

// Pause stops auto-flushing until Resume.
func (b *StatusBuffer) Pause() {
	b.mu.Lock()
	b.paused = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()
}

// Resume restarts auto-flushing.
func (b *StatusBuffer) Resume() {
	b.mu.Lock()
	b.paused = false
	size := len(b.queue)
	reason := b.pendingFlushReason
	if reason == "" && !b.timerDeadline.IsZero() && time.Now().After(b.timerDeadline) {
		reason = "timeout after resume"
	}
	if reason != "" {
		b.pendingFlushReason = ""
	}
	b.mu.Unlock()
	if size == 0 {
		return
	}
	if reason != "" {
		b.Flush(reason)
		return
	}
	b.resetTimer()
}

// Flush combines the buffered messages into a single summary and calls onFlush.
func (b *StatusBuffer) Flush(reason string) {
	b.mu.Lock()
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return
	}
	msgs := b.queue
	b.queue = nil
	b.pendingFlushReason = ""
	b.timerDeadline = time.Time{}
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()

	var combined string
	for i, m := range msgs {
		_, body := ParseMarker(m.Content)
		if i > 0 {
			combined += "\n---\n"
		}
		combined += body
	}
	summary := protocol.BridgeMessage{
		ID:        fmt.Sprintf("status_summary_%d", time.Now().UnixMilli()),
		Source:    protocol.SourceCodex,
		Content:   fmt.Sprintf("[STATUS summary — %d update(s), flushed: %s]\n%s", len(msgs), reason, combined),
		Timestamp: time.Now().UnixMilli(),
	}
	b.onFlush(summary)
}

func (b *StatusBuffer) resetTimer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timerDeadline = time.Now().Add(b.flushTimeout)
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.flushTimeout, func() {
		b.Flush("timeout")
	})
}
