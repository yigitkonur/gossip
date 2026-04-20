package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

type pendingServerRequest struct {
	payload  []byte
	server   json.RawMessage
	method   string
	threadID string
	order    uint64
}

type pendingClientRequest struct {
	conn     connID
	method   string
	threadID string
}

type inFlightServerRequest struct {
	payload  []byte
	server   json.RawMessage
	conn     connID
	method   string
	threadID string
	order    uint64
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
	nextServerOrder atomic.Uint64
	pendingRequests []pendingServerRequest
	pendingClient   map[int64]pendingClientRequest
	serverRequests  map[int64]inFlightServerRequest

	OnTUIConnected    func(id int64)
	OnTUIDisconnected func(id int64)
}

// NewProxy returns a proxy server ready to be served via net/http.
func NewProxy(upstream *Client) *Proxy {
	return &Proxy{
		upstream:       upstream,
		ids:            newProxyIDTable(),
		conns:          make(map[connID]*tuiConn),
		secondaries:    make(map[connID]*secondaryConn),
		pendingClient:  make(map[int64]pendingClientRequest),
		serverRequests: make(map[int64]inFlightServerRequest),
	}
}

// ServeHTTP implements http.Handler so the proxy can be mounted in any server.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
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
	if p.currentConnID == 0 {
		p.currentConnID = id
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

	defer func() {
		p.mu.Lock()
		delete(p.conns, id)
		if p.currentConnID == id {
			p.currentConnID = 0
		}
		p.mu.Unlock()
		p.retireConnectionState(id)
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
		p.dropPendingClientRequestsForConn(secondary.tui.id)
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
		if pending, ok := p.takeServerRequest(id, c.id); ok {
			rewritten, err := restoreID(payload, pending.server)
			if err != nil {
				return
			}
			p.writeUpstream(ctx, rewritten)
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
		p.trackPendingClientRequest(proxyID, c.id, env.Method, env.Params)
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
		payload, env = p.patchInitializeAlreadyError(id, payload, env)
		payload, env = p.patchThreadStartAlreadyError(id, payload, env)
		payload, env = p.patchRateLimitsError(payload, env)
		p.completePendingClientRequest(id, env)
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
		if !p.sendServerRequest(ctx, payload, env.ID, env.Method, env.Params) {
			p.bufferServerRequest(payload, env.ID, env.Method, env.Params)
		}
	}
}

// patchInitializeAlreadyError rewrites the upstream app-server's "Already initialized"
// error for a TUI-initiated initialize into a success response. gossip's primary
// TUI piggybacks on the daemon's already-initialized upstream WS, so a fresh
// TUI sees that error on first connect and aborts. We replay the cached
// initialize result the daemon received, which matches what a fresh
// session-scoped initialize would have returned.
func (p *Proxy) patchInitializeAlreadyError(proxyID int64, payload []byte, env protocol.Envelope) ([]byte, protocol.Envelope) {
	if env.Error == nil || len(env.ID) == 0 {
		return payload, env
	}
	if !strings.Contains(env.Error.Message, "Already initialized") {
		return payload, env
	}
	p.mu.Lock()
	pending, ok := p.pendingClient[proxyID]
	p.mu.Unlock()
	if !ok || pending.method != protocol.MethodInitialize {
		return payload, env
	}
	if p.upstream == nil {
		return payload, env
	}
	cached := p.upstream.InitializeResult()
	if len(cached) == 0 {
		return payload, env
	}
	var rawID any
	if err := json.Unmarshal(env.ID, &rawID); err != nil {
		return payload, env
	}
	var resultAny any
	if err := json.Unmarshal(cached, &resultAny); err != nil {
		return payload, env
	}
	patched, err := json.Marshal(map[string]any{
		"id":     rawID,
		"result": resultAny,
	})
	if err != nil {
		return payload, env
	}
	var patchedEnv protocol.Envelope
	if err := json.Unmarshal(patched, &patchedEnv); err != nil {
		return payload, env
	}
	p.debugLog("patching initialize Already-initialized error with cached success")
	return patched, patchedEnv
}

// patchThreadStartAlreadyError rewrites the upstream app-server's "thread already
// started" (or equivalent) error for a TUI-initiated thread/start into a success
// response. gossip's primary TUI piggybacks on the daemon's shared upstream WS
// which already has a thread running, so a fresh TUI sees an error on bootstrap
// and aborts. We replay the cached thread/start result the daemon received, so
// the TUI adopts the same thread the daemon orchestrates via InjectMessage.
func (p *Proxy) patchThreadStartAlreadyError(proxyID int64, payload []byte, env protocol.Envelope) ([]byte, protocol.Envelope) {
	if env.Error == nil || len(env.ID) == 0 {
		return payload, env
	}
	p.mu.Lock()
	pending, ok := p.pendingClient[proxyID]
	p.mu.Unlock()
	if !ok || pending.method != protocol.MethodThreadStart {
		return payload, env
	}
	if p.upstream == nil {
		return payload, env
	}
	cached := p.upstream.ThreadStartResult()
	if len(cached) == 0 {
		return payload, env
	}
	var rawID any
	if err := json.Unmarshal(env.ID, &rawID); err != nil {
		return payload, env
	}
	var resultAny any
	if err := json.Unmarshal(cached, &resultAny); err != nil {
		return payload, env
	}
	patched, err := json.Marshal(map[string]any{
		"id":     rawID,
		"result": resultAny,
	})
	if err != nil {
		return payload, env
	}
	var patchedEnv protocol.Envelope
	if err := json.Unmarshal(patched, &patchedEnv); err != nil {
		return payload, env
	}
	p.debugLog("patching thread/start error with cached success")
	return patched, patchedEnv
}

func (p *Proxy) patchRateLimitsError(payload []byte, env protocol.Envelope) ([]byte, protocol.Envelope) {
	if env.Error == nil || len(env.ID) == 0 {
		return payload, env
	}
	errMsg := env.Error.Message
	if !strings.Contains(errMsg, "rate limits") && !strings.Contains(errMsg, "rateLimits") {
		return payload, env
	}

	var rawID any
	if err := json.Unmarshal(env.ID, &rawID); err != nil {
		return payload, env
	}

	patched, err := json.Marshal(map[string]any{
		"id": rawID,
		"result": map[string]any{
			"rateLimits": map[string]any{
				"limitId":   nil,
				"limitName": nil,
				"primary": map[string]any{
					"usedPercent":        0,
					"windowDurationMins": 60,
					"resetsAt":           nil,
				},
				"secondary": nil,
				"credits":   nil,
				"planType":  nil,
			},
			"rateLimitsByLimitId": nil,
		},
	})
	if err != nil {
		return payload, env
	}

	var patchedEnv protocol.Envelope
	if err := json.Unmarshal(patched, &patchedEnv); err != nil {
		return payload, env
	}

	p.debugLog("patching rateLimits error with mock success")
	return patched, patchedEnv
}

func (p *Proxy) debugLog(msg string) {
	if p.upstream != nil && p.upstream.opts.Logger != nil {
		p.upstream.opts.Logger(msg)
	}
}

func (p *Proxy) trackPendingClientRequest(proxyID int64, conn connID, method string, params json.RawMessage) {
	p.mu.Lock()
	p.pendingClient[proxyID] = pendingClientRequest{
		conn:     conn,
		method:   method,
		threadID: extractThreadIDFromParams(params),
	}
	p.mu.Unlock()
}

func (p *Proxy) completePendingClientRequest(proxyID int64, env protocol.Envelope) {
	p.mu.Lock()
	pending, ok := p.pendingClient[proxyID]
	if ok {
		delete(p.pendingClient, proxyID)
	}
	p.mu.Unlock()
	if !ok || env.Error != nil {
		return
	}
	switch pending.method {
	case protocol.MethodThreadStart:
		p.clearPendingServerRequests()
	case protocol.MethodThreadResume:
		resumedThreadID := extractThreadIDFromResult(env.Result)
		if resumedThreadID == "" {
			return
		}
		p.replayPendingForThread(context.Background(), pending.conn, resumedThreadID)
		p.dropOrphanPendingServerRequests(resumedThreadID)
		p.dropOrphanPendingClientRequests(pending.conn, resumedThreadID)
	}
}

func (p *Proxy) dropOrphanPendingClientRequests(conn connID, resumedThreadID string) {
	dropped := make([]int64, 0)

	p.mu.Lock()
	for proxyID, pending := range p.pendingClient {
		if pending.conn != conn {
			continue
		}
		if pending.threadID == "" || pending.threadID == resumedThreadID {
			continue
		}
		delete(p.pendingClient, proxyID)
		dropped = append(dropped, proxyID)
	}
	p.mu.Unlock()

	for _, proxyID := range dropped {
		p.ids.ForgetID(proxyID)
	}
}

func (p *Proxy) dropOrphanPendingServerRequests(resumedThreadID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	remaining := make([]pendingServerRequest, 0, len(p.pendingRequests))
	for _, pending := range p.pendingRequests {
		if pending.threadID != "" && pending.threadID != resumedThreadID {
			continue
		}
		remaining = append(remaining, pending)
	}
	p.pendingRequests = remaining
}

func (p *Proxy) clearPendingServerRequests() {
	p.mu.Lock()
	p.pendingRequests = nil
	p.mu.Unlock()
}

func (p *Proxy) dropPendingClientRequestsForConn(conn connID) {
	p.mu.Lock()
	for proxyID, pending := range p.pendingClient {
		if pending.conn == conn {
			delete(p.pendingClient, proxyID)
		}
	}
	p.mu.Unlock()
}

func extractThreadIDFromParams(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var decoded struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return ""
	}
	return decoded.ThreadID
}

func extractThreadIDFromResult(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}
	var decoded struct {
		Thread protocol.Thread `json:"thread"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		return ""
	}
	return decoded.Thread.ID
}

func (p *Proxy) sendServerRequest(ctx context.Context, payload []byte, serverID json.RawMessage, method string, params json.RawMessage) bool {
	current, ok := p.currentConn()
	if !ok {
		return false
	}
	proxyID := p.ids.Allocate(current.id, serverID)
	rewritten, err := rewriteID(payload, proxyID)
	if err != nil {
		return false
	}
	if err := current.write(ctx, rewritten); err != nil {
		p.ids.ForgetID(proxyID)
		return false
	}
	p.mu.Lock()
	p.serverRequests[proxyID] = inFlightServerRequest{
		payload:  append([]byte(nil), payload...),
		server:   append(json.RawMessage(nil), serverID...),
		conn:     current.id,
		method:   method,
		threadID: extractThreadIDFromParams(params),
		order:    p.nextServerOrder.Add(1),
	}
	p.mu.Unlock()
	return true
}

func (p *Proxy) bufferServerRequest(payload []byte, serverID json.RawMessage, method string, params json.RawMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingRequests = append(p.pendingRequests, pendingServerRequest{
		payload:  append([]byte(nil), payload...),
		server:   append(json.RawMessage(nil), serverID...),
		method:   method,
		threadID: extractThreadIDFromParams(params),
		order:    p.nextServerOrder.Add(1),
	})
}

func (p *Proxy) replayPendingForThread(ctx context.Context, conn connID, resumedThreadID string) {
	current, ok := p.currentConn()
	if !ok || current.id != conn {
		return
	}

	p.mu.Lock()
	pending := append([]pendingServerRequest(nil), p.pendingRequests...)
	p.pendingRequests = nil
	p.mu.Unlock()

	remaining := make([]pendingServerRequest, 0)
	for _, req := range pending {
		if req.threadID != "" && req.threadID != resumedThreadID {
			remaining = append(remaining, req)
			continue
		}
		if !p.replayServerRequest(ctx, current, req) {
			remaining = append(remaining, req)
		}
	}
	if len(remaining) > 0 {
		p.mu.Lock()
		p.pendingRequests = append(remaining, p.pendingRequests...)
		sortPendingServerRequests(p.pendingRequests)
		p.mu.Unlock()
	}
}

func (p *Proxy) replayServerRequest(ctx context.Context, current *tuiConn, req pendingServerRequest) bool {
	proxyID := p.ids.Allocate(current.id, req.server)
	rewritten, err := rewriteID(req.payload, proxyID)
	if err != nil {
		p.ids.ForgetID(proxyID)
		return false
	}
	if err := current.write(ctx, rewritten); err != nil {
		p.ids.ForgetID(proxyID)
		return false
	}
	p.mu.Lock()
	p.serverRequests[proxyID] = inFlightServerRequest{
		payload:  append([]byte(nil), req.payload...),
		server:   append(json.RawMessage(nil), req.server...),
		conn:     current.id,
		method:   req.method,
		threadID: req.threadID,
		order:    req.order,
	}
	p.mu.Unlock()
	return true
}

func (p *Proxy) takeServerRequest(proxyID int64, conn connID) (inFlightServerRequest, bool) {
	p.mu.Lock()
	pending, ok := p.serverRequests[proxyID]
	if ok && pending.conn == conn {
		delete(p.serverRequests, proxyID)
	} else {
		ok = false
	}
	p.mu.Unlock()
	if ok {
		p.ids.ForgetID(proxyID)
	}
	return pending, ok
}

func (p *Proxy) retireConnectionState(conn connID) {
	p.dropPendingClientRequestsForConn(conn)

	requeued := make([]pendingServerRequest, 0)
	forgotten := make([]int64, 0)

	p.mu.Lock()
	for proxyID, pending := range p.serverRequests {
		if pending.conn != conn {
			continue
		}
		delete(p.serverRequests, proxyID)
		forgotten = append(forgotten, proxyID)
		requeued = append(requeued, pendingServerRequest{
			payload:  append([]byte(nil), pending.payload...),
			server:   append(json.RawMessage(nil), pending.server...),
			method:   pending.method,
			threadID: pending.threadID,
			order:    pending.order,
		})
	}
	if len(requeued) > 0 {
		sort.Slice(requeued, func(i, j int) bool {
			return requeued[i].order < requeued[j].order
		})
		p.pendingRequests = append(p.pendingRequests, requeued...)
		sortPendingServerRequests(p.pendingRequests)
	}
	p.mu.Unlock()

	for _, proxyID := range forgotten {
		p.ids.ForgetID(proxyID)
	}
}

func sortPendingServerRequests(pending []pendingServerRequest) {
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].order < pending[j].order
	})
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
