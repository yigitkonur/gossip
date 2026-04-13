package codex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// DialerOptions configures a persistent WebSocket dialer.
type DialerOptions struct {
	OnMessage    func([]byte)
	OnConnect    func()
	OnDisconnect func(err error)
	Logger       func(msg string)
	MaxBackoff   time.Duration
}

// Dialer owns a persistent WebSocket connection with exponential-backoff reconnect.
type Dialer struct {
	url  string
	opts DialerOptions

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

// NewDialer returns a Dialer ready to be run.
func NewDialer(url string, opts DialerOptions) *Dialer {
	if opts.MaxBackoff == 0 {
		opts.MaxBackoff = 30 * time.Second
	}
	return &Dialer{url: url, opts: opts}
}

// Conn returns the currently-held connection or nil.
func (d *Dialer) Conn() *websocket.Conn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn
}

// ConnectOnce performs a single dial and launches the reader loop.
func (d *Dialer) ConnectOnce(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, d.url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", d.url, err)
	}
	conn.SetReadLimit(10 * 1024 * 1024)

	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()

	if d.opts.OnConnect != nil {
		d.opts.OnConnect()
	}
	go d.readLoop(ctx, conn)
	return nil
}

// Run keeps the dialer connected until ctx is cancelled.
func (d *Dialer) Run(ctx context.Context) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := d.ConnectOnce(ctx)
		if err == nil {
			d.waitDisconnect(ctx)
			attempt = 0
			continue
		}
		if d.opts.Logger != nil {
			d.opts.Logger(fmt.Sprintf("codex dial failed (attempt %d): %v", attempt+1, err))
		}
		backoff := time.Duration(1<<minInt(attempt, 5)) * time.Second
		if backoff > d.opts.MaxBackoff {
			backoff = d.opts.MaxBackoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		attempt++
	}
}

func (d *Dialer) waitDisconnect(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.Lock()
			alive := d.conn != nil
			d.mu.Unlock()
			if !alive {
				return
			}
		}
	}
}

func (d *Dialer) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		d.mu.Lock()
		if d.conn == conn {
			d.conn = nil
		}
		d.mu.Unlock()
	}()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			if d.opts.OnDisconnect != nil {
				d.opts.OnDisconnect(err)
			}
			return
		}
		if d.opts.OnMessage != nil {
			d.opts.OnMessage(payload)
		}
	}
}

// Close shuts down the dialer and any held connection.
func (d *Dialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if d.conn != nil {
		_ = d.conn.Close(websocket.StatusNormalClosure, "")
		d.conn = nil
	}
	return nil
}

// ErrNotConnected is returned by Write when the dialer has no live connection.
var ErrNotConnected = errors.New("codex: not connected")

// Write sends a text frame on the current connection.
func (d *Dialer) Write(ctx context.Context, payload []byte) error {
	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	return conn.Write(ctx, websocket.MessageText, payload)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
