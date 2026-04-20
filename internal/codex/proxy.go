package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

type pendingServerRequest struct {
	payload []byte
	server  json.RawMessage
	method  string
}

// Proxy is the TUI-facing WebSocket server.
type Proxy struct {
	upstream *Client
	ids      *proxyIDTable

	mu              sync.Mutex
	conns           map[connID]*tuiConn
	currentConnID   connID
	nextConn        atomic.Int64
	pendingRequests []pendingServerRequest

	OnTUIConnected    func(id int64)
	OnTUIDisconnected func(id int64)
}

// NewProxy returns a proxy server ready to be served via net/http.
func NewProxy(upstream *Client) *Proxy {
	return &Proxy{upstream: upstream, ids: newProxyIDTable(), conns: make(map[connID]*tuiConn)}
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
	p.currentConnID = id
	pending := append([]pendingServerRequest(nil), p.pendingRequests...)
	p.pendingRequests = nil
	p.mu.Unlock()

	if p.OnTUIConnected != nil {
		p.OnTUIConnected(int64(id))
	}
	p.replayPending(r.Context(), c, pending)

	defer func() {
		p.mu.Lock()
		delete(p.conns, id)
		if p.currentConnID == id {
			p.currentConnID = 0
		}
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
	if !p.isCurrentConn(c.id) {
		return
	}
	var env protocol.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	switch env.Kind() {
	case protocol.KindResponse:
		id, ok := idAsInt64(env.ID)
		if !ok {
			return
		}
		conn, orig, found := p.ids.Resolve(id)
		if !found || conn != c.id {
			return
		}
		rewritten, err := restoreID(payload, orig)
		if err != nil {
			return
		}
		p.writeUpstream(ctx, rewritten)
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

// HandleUpstreamMessage classifies and routes a message from the app-server to the current TUI.
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
		if !found || !p.isCurrentConn(conn) {
			return
		}
		restored, err := restoreID(payload, orig)
		if err != nil {
			return
		}
		p.writeToConn(ctx, conn, restored)
	case protocol.KindNotification:
		p.writeCurrent(ctx, payload)
	case protocol.KindServerRequest:
		if !p.sendServerRequest(ctx, payload, env.ID, env.Method) {
			p.bufferServerRequest(payload, env.ID, env.Method)
		}
	}
}

func (p *Proxy) sendServerRequest(ctx context.Context, payload []byte, serverID json.RawMessage, method string) bool {
	current, ok := p.currentConn()
	if !ok {
		return false
	}
	proxyID := p.ids.Allocate(current.id, serverID)
	rewritten, err := rewriteID(payload, proxyID)
	if err != nil {
		return false
	}
	return current.write(ctx, rewritten) == nil
}

func (p *Proxy) bufferServerRequest(payload []byte, serverID json.RawMessage, method string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingRequests = append(p.pendingRequests, pendingServerRequest{payload: append([]byte(nil), payload...), server: append(json.RawMessage(nil), serverID...), method: method})
}

func (p *Proxy) replayPending(ctx context.Context, c *tuiConn, pending []pendingServerRequest) {
	remaining := make([]pendingServerRequest, 0)
	for _, req := range pending {
		proxyID := p.ids.Allocate(c.id, req.server)
		rewritten, err := rewriteID(req.payload, proxyID)
		if err != nil || c.write(ctx, rewritten) != nil {
			remaining = append(remaining, req)
		}
	}
	if len(remaining) > 0 {
		p.mu.Lock()
		p.pendingRequests = append(remaining, p.pendingRequests...)
		p.mu.Unlock()
	}
}

func (p *Proxy) currentConn() (*tuiConn, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.conns[p.currentConnID]
	if c == nil {
		return nil, false
	}
	return c, true
}

func (p *Proxy) isCurrentConn(id connID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentConnID == id && p.conns[id] != nil
}

func (p *Proxy) writeCurrent(ctx context.Context, payload []byte) {
	current, ok := p.currentConn()
	if !ok {
		return
	}
	_ = current.write(ctx, payload)
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
