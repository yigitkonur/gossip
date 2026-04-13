package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

// Proxy is the TUI-facing WebSocket server.
type Proxy struct {
	upstream *Client
	ids      *proxyIDTable

	mu       sync.Mutex
	conns    map[connID]*tuiConn
	nextConn atomic.Int64

	upstreamInbound chan []byte

	OnTUIConnected    func(id int64)
	OnTUIDisconnected func(id int64)
}

// NewProxy returns a proxy server ready to be served via net/http.
func NewProxy(upstream *Client) *Proxy {
	return &Proxy{
		upstream:        upstream,
		ids:             newProxyIDTable(),
		conns:           make(map[connID]*tuiConn),
		upstreamInbound: make(chan []byte, 256),
	}
}

// ServeHTTP implements http.Handler so the proxy can be mounted in any server.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(10 * 1024 * 1024)

	id := connID(p.nextConn.Add(1))
	c := &tuiConn{id: id, conn: conn}

	p.mu.Lock()
	p.conns[id] = c
	p.mu.Unlock()

	if p.OnTUIConnected != nil {
		p.OnTUIConnected(int64(id))
	}

	defer func() {
		p.mu.Lock()
		delete(p.conns, id)
		p.mu.Unlock()
		p.ids.ForgetConn(id)
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if p.OnTUIDisconnected != nil {
			p.OnTUIDisconnected(int64(id))
		}
	}()

	p.readLoop(r.Context(), c)
}

// ConnectionCount returns the number of live TUI connections.
func (p *Proxy) ConnectionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

type tuiConn struct {
	id   connID
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *tuiConn) write(ctx context.Context, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func (p *Proxy) readLoop(ctx context.Context, c *tuiConn) {
	for {
		_, payload, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		p.forwardToUpstream(ctx, c, payload)
	}
}

func (p *Proxy) forwardToUpstream(ctx context.Context, c *tuiConn, payload []byte) {
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	switch env.Kind() {
	case protocol.KindResponse:
		p.writeUpstream(ctx, payload)
	case protocol.KindNotification:
		p.writeUpstream(ctx, payload)
	case protocol.KindServerRequest:
		proxyID := p.ids.Allocate(c.id, env.ID)
		rewritten, err := rewriteID(payload, proxyID)
		if err != nil {
			return
		}
		p.writeUpstream(ctx, rewritten)
	default:
		p.writeUpstream(ctx, payload)
	}
}

func (p *Proxy) writeUpstream(ctx context.Context, payload []byte) {
	if p.upstream == nil || p.upstream.dialer == nil {
		return
	}
	_ = p.upstream.dialer.Write(ctx, payload)
}

// HandleUpstreamMessage classifies and routes a message from the app-server to the right TUI.
func (p *Proxy) HandleUpstreamMessage(ctx context.Context, payload []byte) {
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	switch env.Kind() {
	case protocol.KindResponse:
		id, ok := idAsInt64(env.ID)
		if !ok || id < 0 {
			return
		}
		conn, orig, found := p.ids.Resolve(id)
		if !found {
			return
		}
		restored, err := restoreID(payload, orig)
		if err != nil {
			return
		}
		p.writeToConn(ctx, conn, restored)
	case protocol.KindNotification:
		p.broadcast(ctx, payload)
	case protocol.KindServerRequest:
		p.broadcast(ctx, payload)
	}
}

func (p *Proxy) writeToConn(ctx context.Context, id connID, payload []byte) {
	p.mu.Lock()
	c := p.conns[id]
	p.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.write(ctx, payload)
}

func (p *Proxy) broadcast(ctx context.Context, payload []byte) {
	p.mu.Lock()
	conns := make([]*tuiConn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.write(ctx, payload)
	}
}
