package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/yigitkonur/gossip/internal/protocol"
)

// ServerRequest is delivered when the remote peer sends a request requiring a response.
type ServerRequest struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

// Notification is delivered for one-way server pushes.
type Notification struct {
	Method string
	Params json.RawMessage
}

// Client is a JSON-RPC 2.0 client over a single Writer.
type Client struct {
	writer Writer
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[string]chan *protocol.Envelope
	closed  bool

	notifications  chan Notification
	serverRequests chan ServerRequest
}

// NewClient constructs a Client using the given Writer.
func NewClient(w Writer) *Client {
	return &Client{
		writer:         w,
		pending:        make(map[string]chan *protocol.Envelope),
		notifications:  make(chan Notification, 256),
		serverRequests: make(chan ServerRequest, 16),
	}
}

// Notifications returns the read-only channel of inbound notifications.
func (c *Client) Notifications() <-chan Notification { return c.notifications }

// ServerRequests returns the read-only channel of inbound server-initiated requests.
func (c *Client) ServerRequests() <-chan ServerRequest { return c.serverRequests }

// Call sends a client→server request and waits for its response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(-1)
	key := idKey(id)
	respCh := make(chan *protocol.Envelope, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("jsonrpc: client closed")
	}
	c.pending[key] = respCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	if err := c.writer.WriteJSON(ctx, req); err != nil {
		return nil, fmt.Errorf("jsonrpc: write %s: %w", method, err)
	}

	select {
	case env, ok := <-respCh:
		if !ok || env == nil {
			return nil, errors.New("jsonrpc: client closed")
		}
		if env.Error != nil {
			return nil, fmt.Errorf("jsonrpc: %s: %d %s", method, env.Error.Code, env.Error.Message)
		}
		return env.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Notify sends a one-way notification with no id.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	n := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		n["params"] = params
	}
	return c.writer.WriteJSON(ctx, n)
}

// Respond sends a successful response to a server-initiated request.
func (c *Client) Respond(ctx context.Context, id json.RawMessage, result any) error {
	var rawID any
	if err := json.Unmarshal(id, &rawID); err != nil {
		return err
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID,
		"result":  result,
	}
	return c.writer.WriteJSON(ctx, resp)
}

// RespondError sends an error response to a server-initiated request.
func (c *Client) RespondError(ctx context.Context, id json.RawMessage, code int, message string) error {
	var rawID any
	if err := json.Unmarshal(id, &rawID); err != nil {
		return err
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID,
		"error":   map[string]any{"code": code, "message": message},
	}
	return c.writer.WriteJSON(ctx, resp)
}

// HandleIncoming dispatches a single inbound JSON-RPC message.
func (c *Client) HandleIncoming(raw []byte) {
	var env protocol.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	switch env.Kind() {
	case protocol.KindResponse:
		key, ok := NormalizeID(env.ID)
		if !ok {
			return
		}
		ch, exists := c.pending[key]
		if exists {
			select {
			case ch <- &env:
			default:
			}
		}
	case protocol.KindNotification:
		select {
		case c.notifications <- Notification{Method: env.Method, Params: env.Params}:
		default:
		}
	case protocol.KindServerRequest:
		select {
		case c.serverRequests <- ServerRequest{ID: append(json.RawMessage(nil), env.ID...), Method: env.Method, Params: env.Params}:
		default:
		}
	}
}

// Close shuts down the client and closes its channels.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for _, ch := range c.pending {
		close(ch)
	}
	notifications := c.notifications
	serverRequests := c.serverRequests
	c.pending = nil
	c.notifications = nil
	c.serverRequests = nil
	c.mu.Unlock()
	close(notifications)
	close(serverRequests)
	return c.writer.Close()
}

func idKey(id int64) string {
	return fmt.Sprintf("%d", id)
}
