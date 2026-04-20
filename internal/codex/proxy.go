package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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
	secondaries     map[connID]*secondaryConn
	currentConnID   connID
	nextConn        atomic.Int64
	pendingRequests []pendingServerRequest

	OnTUIConnected    func(id int64)
	OnTUIDisconnected func(id int64)
}

// NewProxy returns a proxy server ready to be served via net/http.
func NewProxy(upstream *Client) *Proxy {
	return &Proxy{
		upstream:    upstream,
		ids:         newProxyIDTable(),
		conns:       make(map[connID]*tuiConn),
		secondaries: make(map[connID]*secondaryConn),
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
	var secondary *secondaryConn
	isPrimary := false

	p.mu.Lock()
	p.conns[id] = c
	pending := []pendingServerRequest(nil)
	if p.currentConnID == 0 {
		p.currentConnID = id
		pending = append([]pendingServerRequest(nil), p.pendingRequests...)
		p.pendingRequests = nil
		isPrimary = true
	} else {
		secondary = &secondaryConn{tui: c}
		p.secondaries[id] = secondary
	}
	p.mu.Unlock()

	if !isPrimary {
		// Dedicated picker sockets talk to their own app-server websocket, which
		// keeps their request ids isolated from the primary proxy rewrite table.
		p.serveSecondaryConnection(r.Context(), secondary)
		return
	}

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

type secondaryConn struct {
	tui *tuiConn

	mu        sync.Mutex
	app       *websocket.Conn
	buffer    [][]byte
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func (c *tuiConn) write(ctx context.Context, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func (s *secondaryConn) attachApp(conn *websocket.Conn) [][]byte {
	s.mu.Lock()
	s.app = conn
	buffered := s.buffer
	s.buffer = nil
	s.mu.Unlock()
	return buffered
}

func (s *secondaryConn) writeApp(ctx context.Context, payload []byte) error {
	s.mu.Lock()
	app := s.app
	if app == nil {
		s.buffer = append(s.buffer, append([]byte(nil), payload...))
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return app.Write(ctx, websocket.MessageText, payload)
}

func (s *secondaryConn) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		app := s.app
		s.app = nil
		s.mu.Unlock()
		if app != nil {
			_ = app.Close(websocket.StatusNormalClosure, "")
		}
		_ = s.tui.conn.Close(websocket.StatusNormalClosure, "")
	})
}

func (p *Proxy) serveSecondaryConnection(ctx context.Context, secondary *secondaryConn) {
	go p.connectSecondaryAppServer(secondary)

	defer func() {
		p.mu.Lock()
		delete(p.conns, secondary.tui.id)
		delete(p.secondaries, secondary.tui.id)
		p.mu.Unlock()
		p.ids.ForgetConn(secondary.tui.id)
		secondary.close()
	}()

	for {
		_, payload, err := secondary.tui.conn.Read(ctx)
		if err != nil {
			return
		}
		if err := secondary.writeApp(context.Background(), payload); err != nil {
			return
		}
	}
}

func (p *Proxy) connectSecondaryAppServer(secondary *secondaryConn) {
	url, ok := p.appServerURL()
	if !ok {
		secondary.close()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := websocket.Dial(ctx, url, nil)
	cancel()
	if err != nil {
		secondary.close()
		return
	}
	conn.SetReadLimit(10 * 1024 * 1024)

	for _, payload := range secondary.attachApp(conn) {
		if err := secondary.writeApp(context.Background(), payload); err != nil {
			secondary.close()
			return
		}
	}

	for {
		_, payload, err := conn.Read(context.Background())
		if err != nil {
			secondary.close()
			return
		}
		if err := secondary.tui.write(context.Background(), payload); err != nil {
			secondary.close()
			return
		}
	}
}

func (p *Proxy) appServerURL() (string, bool) {
	if p.upstream == nil {
		return "", false
	}
	if p.upstream.proc != nil {
		return p.upstream.proc.WebSocketURL(), true
	}
	if p.upstream.dialer != nil && p.upstream.dialer.url != "" {
		return p.upstream.dialer.url, true
	}
	return "", false
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
