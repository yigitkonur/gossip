package control

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

// Handler is the daemon-side callback surface invoked by the control server.
type Handler interface {
	OnClaudeConnect()
	OnClaudeDisconnect(reason string)
	OnClaudeToCodex(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) (success bool, errorMsg string)
	Snapshot() Status
}

// Server is a control WebSocket server exposing /ws, /healthz, and /readyz.
type Server struct {
	handler Handler

	mu       sync.Mutex
	conns    map[int64]*controlConn
	nextID   int64
	attached *controlConn
}

// NewServer constructs a control server bound to the given handler.
func NewServer(h Handler) *Server {
	return &Server{handler: h, conns: make(map[int64]*controlConn)}
}

// HTTPHandler returns an http.Handler serving /ws, /healthz, and /readyz.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

// Broadcast sends a codex_to_claude message to the currently attached bridge.
func (s *Server) Broadcast(ctx context.Context, msg protocol.BridgeMessage) {
	s.mu.Lock()
	c := s.attached
	s.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.write(ctx, ServerMessage{Type: ServerMsgCodexToClaude, Message: &msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := s.handler.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	status := s.handler.Snapshot()
	if !status.BridgeReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c := &controlConn{conn: conn}
	s.mu.Lock()
	s.nextID++
	c.id = s.nextID
	s.conns[c.id] = c
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.conns, c.id)
		if s.attached == c {
			s.attached = nil
			s.handler.OnClaudeDisconnect("ws closed")
		}
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, payload, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var msg ClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		s.handleClientMessage(r.Context(), c, msg)
	}
}

func (s *Server) handleClientMessage(ctx context.Context, c *controlConn, msg ClientMessage) {
	switch msg.Type {
	case ClientMsgClaudeConnect:
		s.mu.Lock()
		s.attached = c
		s.mu.Unlock()
		s.handler.OnClaudeConnect()
		status := s.handler.Snapshot()
		_ = c.write(ctx, ServerMessage{Type: ServerMsgStatus, Status: &status})
	case ClientMsgClaudeDisconnect:
		s.mu.Lock()
		if s.attached == c {
			s.attached = nil
		}
		s.mu.Unlock()
		s.handler.OnClaudeDisconnect("client requested")
	case ClientMsgClaudeToCodex:
		if msg.Message == nil {
			_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexResult, RequestID: msg.RequestID, Success: false, Error: "missing message"})
			return
		}
		ok, errMsg := s.handler.OnClaudeToCodex(ctx, *msg.Message, msg.RequireReply)
		_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexResult, RequestID: msg.RequestID, Success: ok, Error: errMsg})
	case ClientMsgStatus:
		status := s.handler.Snapshot()
		_ = c.write(ctx, ServerMessage{Type: ServerMsgStatus, Status: &status})
	}
}

type controlConn struct {
	id   int64
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *controlConn) write(ctx context.Context, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, payload)
}
