package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

const reconnectDelayCap = 30 * time.Second

// ClientOptions configures a Client.
type ClientOptions struct {
	URL             string
	OnCodexMsg      func(msg protocol.BridgeMessage)
	OnStatus        func(status Status)
	OnDisconnect    func(code int, reason string, uptime time.Duration)
	OnRejected      func(code int, reason string, uptime time.Duration)
	OnReconnect     func()
	Logger          func(msg string)
	MaxBackoff      time.Duration
	ShouldReconnect func() bool
}

// Client dials the daemon control server with reconnect.
type Client struct {
	opts ClientOptions

	mu       sync.Mutex
	conn     *websocket.Conn
	openedAt time.Time
	nextReq  atomic.Int64
	pending  map[string]chan ServerMessage
}

// NewClient constructs a Client.
func NewClient(opts ClientOptions) *Client {
	if opts.MaxBackoff == 0 {
		opts.MaxBackoff = reconnectDelayCap
	}
	return &Client{opts: opts, pending: make(map[string]chan ServerMessage)}
}

// Connect performs a single dial and starts the reader goroutine.
func (c *Client) Connect(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, c.opts.URL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.opts.URL, err)
	}
	c.mu.Lock()
	c.conn = conn
	c.openedAt = time.Now()
	c.mu.Unlock()
	go c.readLoop(ctx, conn)
	return nil
}

// AttachClaude sends the claude_connect control message.
func (c *Client) AttachClaude(ctx context.Context) error {
	return c.send(ctx, ClientMessage{Type: ClientMsgClaudeConnect})
}

// SendReply forwards a Claude reply and waits for the result.
func (c *Client) SendReply(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) (bool, string) {
	id := fmt.Sprintf("reply_%d_%d", time.Now().UnixMilli(), c.nextReq.Add(1))
	resp := make(chan ServerMessage, 1)
	c.mu.Lock()
	c.pending[id] = resp
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.send(ctx, ClientMessage{Type: ClientMsgClaudeToCodex, RequestID: id, Message: &msg, RequireReply: requireReply}); err != nil {
		return false, err.Error()
	}

	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case m := <-resp:
		return m.Success, m.Error
	case <-callCtx.Done():
		return false, "timed out waiting for daemon reply"
	}
}

// SendReplyBlocking injects a message into Codex via the daemon's outbound
// loop queue and waits for an [IMPORTANT]-marked reply (or timeout / send
// failure). Callers pass waitMs; the client adds a 10-second margin before
// giving up at its own layer to let the daemon's timer fire first.
func (c *Client) SendReplyBlocking(ctx context.Context, msg protocol.BridgeMessage, requireReply bool, waitMs int) (text string, received bool, errMsg string) {
	id := fmt.Sprintf("blk_%d_%d", time.Now().UnixMilli(), c.nextReq.Add(1))
	resp := make(chan ServerMessage, 1)
	c.mu.Lock()
	c.pending[id] = resp
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.send(ctx, ClientMessage{Type: ClientMsgClaudeToCodexBlocking, RequestID: id, Message: &msg, RequireReply: requireReply, WaitMs: waitMs}); err != nil {
		return "", false, err.Error()
	}

	// Normalize waitMs first so a zero or negative caller value matches
	// the daemon's 90 s fallback before we add the 10 s WS round-trip
	// margin. Without this the client would time out long before the
	// daemon's timer could fire.
	effectiveWaitMs := waitMs
	if effectiveWaitMs <= 0 {
		effectiveWaitMs = 90_000
	}
	margin := effectiveWaitMs + 10_000
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(margin)*time.Millisecond)
	defer cancel()
	select {
	case m := <-resp:
		return m.Text, m.Received, m.Error
	case <-callCtx.Done():
		return "", false, "timed out waiting for daemon blocking reply"
	}
}

// Disconnect closes the connection gracefully.
func (c *Client) Disconnect() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// RunWithReconnect connects and reconnects with exponential backoff until ctx is cancelled.
func (c *Client) RunWithReconnect(ctx context.Context) error {
	attempt := 0
	maxBackoff := effectiveReconnectMax(c.opts.MaxBackoff)
	connectedOnce := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		if c.opts.ShouldReconnect != nil && !c.opts.ShouldReconnect() {
			return nil
		}
		err := c.Connect(ctx)
		if err == nil {
			attachErr := c.AttachClaude(ctx)
			if attachErr != nil {
				if c.opts.Logger != nil {
					c.opts.Logger("attach failed: " + attachErr.Error())
				}
			} else if connectedOnce && c.opts.OnReconnect != nil {
				c.opts.OnReconnect()
			}
			connectedOnce = true
			c.waitClosed(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if c.opts.ShouldReconnect != nil && !c.opts.ShouldReconnect() {
				return nil
			}
			delay := reconnectCooldown(maxBackoff)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
			attempt = 0
			continue
		}
		if c.opts.Logger != nil {
			c.opts.Logger(fmt.Sprintf("control dial failed (attempt %d): %v", attempt+1, err))
		}
		backoff := reconnectBackoff(attempt, maxBackoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		attempt++
	}
}

func effectiveReconnectMax(max time.Duration) time.Duration {
	if max <= 0 || max > reconnectDelayCap {
		return reconnectDelayCap
	}
	return max
}

func reconnectCooldown(max time.Duration) time.Duration {
	max = effectiveReconnectMax(max)
	if max < reconnectDelayCap {
		return max
	}
	// Divergence: we keep a 30s post-disconnect reconnect floor to prevent bridge reconnect churn, even though current TS retries sooner.
	return reconnectDelayCap
}

func reconnectBackoff(attempt int, max time.Duration) time.Duration {
	max = effectiveReconnectMax(max)
	backoff := time.Duration(1<<minInt(attempt, 5)) * time.Second
	if backoff > max {
		return max
	}
	return backoff
}

func (c *Client) waitClosed(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			alive := c.conn != nil
			c.mu.Unlock()
			if !alive {
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) {
	closeCode := 0
	closeReason := "read loop exit"
	rejected := false
	defer func() {
		c.mu.Lock()
		openedAt := c.openedAt
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		if rejected {
			c.rejectPendingReplies("Gossip daemon rejected this session.")
			if c.opts.OnRejected != nil {
				c.opts.OnRejected(closeCode, closeReason, time.Since(openedAt))
			}
			return
		}
		c.rejectPendingReplies("Daemon connection closed")
		if c.opts.OnDisconnect != nil {
			c.opts.OnDisconnect(closeCode, closeReason, time.Since(openedAt))
		}
	}()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			var closeErr websocket.CloseError
			if errors.As(err, &closeErr) {
				closeCode = int(closeErr.Code)
				closeReason = closeErr.Reason
				rejected = closeErr.Code == CloseCodeReplaced
			}
			return
		}
		var msg ServerMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		c.handleServerMessage(msg)
	}
}

func (c *Client) handleServerMessage(msg ServerMessage) {
	switch msg.Type {
	case ServerMsgCodexToClaude:
		if msg.Message != nil && c.opts.OnCodexMsg != nil {
			c.opts.OnCodexMsg(*msg.Message)
		}
	case ServerMsgClaudeToCodexResult, ServerMsgClaudeToCodexReply:
		c.mu.Lock()
		ch, ok := c.pending[msg.RequestID]
		c.mu.Unlock()
		if ok {
			select {
			case ch <- msg:
			default:
			}
		}
	case ServerMsgStatus:
		if msg.Status != nil && c.opts.OnStatus != nil {
			c.opts.OnStatus(*msg.Status)
		}
	}
}

func (c *Client) send(ctx context.Context, m ClientMessage) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("control client: not connected")
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, payload)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Client) rejectPendingReplies(error string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for requestID, pending := range c.pending {
		select {
		case pending <- ServerMessage{Type: ServerMsgClaudeToCodexResult, RequestID: requestID, Success: false, Error: error}:
		default:
		}
		delete(c.pending, requestID)
	}
}
