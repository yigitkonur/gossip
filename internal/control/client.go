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

// ClientOptions configures a Client.
type ClientOptions struct {
	URL             string
	OnCodexMsg      func(msg protocol.BridgeMessage)
	OnStatus        func(status Status)
	OnDisconnect    func(code int, reason string, uptime time.Duration)
	OnRejected      func(code int, reason string, uptime time.Duration)
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
		opts.MaxBackoff = 30 * time.Second
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
	for {
		if ctx.Err() != nil {
			return nil
		}
		if c.opts.ShouldReconnect != nil && !c.opts.ShouldReconnect() {
			return nil
		}
		err := c.Connect(ctx)
		if err == nil {
			if err := c.AttachClaude(ctx); err != nil && c.opts.Logger != nil {
				c.opts.Logger("attach failed: " + err.Error())
			}
			c.waitClosed(ctx)
			attempt = 0
			continue
		}
		if c.opts.Logger != nil {
			c.opts.Logger(fmt.Sprintf("control dial failed (attempt %d): %v", attempt+1, err))
		}
		backoff := time.Duration(1<<minInt(attempt, 5)) * time.Second
		if backoff > c.opts.MaxBackoff {
			backoff = c.opts.MaxBackoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		attempt++
	}
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
	case ServerMsgClaudeToCodexResult:
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
