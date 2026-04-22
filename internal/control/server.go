package control

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/protocol"
)

// Handler is the daemon-side callback surface invoked by the control server.
type Handler interface {
	OnClaudeConnect()
	OnClaudeDisconnect(reason string)
	OnClaudeToCodex(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) (success bool, errorMsg string)
	// OnClaudeToCodexBlocking injects a message into Codex via the outbound
	// queue and blocks (up to waitMs) for an [IMPORTANT]-marked reply.
	// Returns the reply body, whether it actually arrived, and any error.
	OnClaudeToCodexBlocking(ctx context.Context, msg protocol.BridgeMessage, requireReply bool, waitMs int) (text string, received bool, errorMsg string)
	Snapshot() Status
}

// Server is a control WebSocket server exposing /ws, /healthz, and /readyz.
type Server struct {
	handler Handler

	mu              sync.Mutex
	conns           map[int64]*controlConn
	nextID          int64
	attached        *controlConn
	buffered        []protocol.BridgeMessage
	droppedMessages int
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
		s.bufferMessage(msg)
		return
	}
	if err := c.write(ctx, ServerMessage{Type: ServerMsgCodexToClaude, Message: &msg}); err != nil {
		s.bufferMessage(msg)
	}
}

// QueuedCount returns the number of buffered messages awaiting bridge attach.
func (s *Server) QueuedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buffered)
}

// DroppedCount returns how many buffered messages were trimmed due to overflow.
func (s *Server) DroppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.droppedMessages
}

func (s *Server) bufferMessage(msg protocol.BridgeMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendBufferedLocked(msg)
}

func (s *Server) appendBufferedLocked(msgs ...protocol.BridgeMessage) {
	s.buffered = append(s.buffered, msgs...)
	if overflow := len(s.buffered) - 100; overflow > 0 {
		s.buffered = s.buffered[overflow:]
		s.droppedMessages += overflow
	}
}

func (s *Server) flushBuffered(ctx context.Context, c *controlConn) {
	s.mu.Lock()
	msgs := append([]protocol.BridgeMessage(nil), s.buffered...)
	s.buffered = nil
	s.mu.Unlock()
	for i, msg := range msgs {
		if err := c.write(ctx, ServerMessage{Type: ServerMsgCodexToClaude, Message: &msg}); err != nil {
			s.mu.Lock()
			s.appendBufferedLocked(msgs[i:]...)
			s.mu.Unlock()
			return
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := s.handler.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	status := s.handler.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	if status.ThreadID == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
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
		attached := s.attached
		if attached != nil && attached != c {
			s.mu.Unlock()
			_ = c.conn.Close(CloseCodeReplaced, "another Claude session is already connected")
			return
		}
		s.attached = c
		s.mu.Unlock()
		s.handler.OnClaudeConnect()
		status := s.handler.Snapshot()
		_ = c.write(ctx, ServerMessage{Type: ServerMsgStatus, Status: &status})
		s.flushBuffered(ctx, c)
	case ClientMsgClaudeDisconnect:
		s.mu.Lock()
		if s.attached != c {
			s.mu.Unlock()
			return
		}
		s.attached = nil
		s.mu.Unlock()
		s.handler.OnClaudeDisconnect("client requested")
	case ClientMsgClaudeToCodex:
		s.mu.Lock()
		isAttached := s.attached == c
		s.mu.Unlock()
		if !isAttached {
			return
		}
		if msg.Message == nil {
			_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexResult, RequestID: msg.RequestID, Success: false, Error: "missing message"})
			return
		}
		ok, errMsg := s.handler.OnClaudeToCodex(ctx, *msg.Message, msg.RequireReply)
		_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexResult, RequestID: msg.RequestID, Success: ok, Error: errMsg})
	case ClientMsgClaudeToCodexBlocking:
		s.mu.Lock()
		isAttached := s.attached == c
		s.mu.Unlock()
		if !isAttached {
			return
		}
		if msg.Message == nil {
			_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexReply, RequestID: msg.RequestID, Received: false, Error: "missing message"})
			return
		}
		text, received, errMsg := s.handler.OnClaudeToCodexBlocking(ctx, *msg.Message, msg.RequireReply, msg.WaitMs)
		_ = c.write(ctx, ServerMessage{Type: ServerMsgClaudeToCodexReply, RequestID: msg.RequestID, Text: text, Received: received, Error: errMsg})
	case ClientMsgStatus:
		s.mu.Lock()
		isAttached := s.attached == c
		s.mu.Unlock()
		if !isAttached {
			return
		}
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
