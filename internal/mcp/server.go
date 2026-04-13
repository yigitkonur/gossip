package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

// ServerOptions configures an MCP server.
type ServerOptions struct {
	Name                string
	Version             string
	Instructions        string
	ReplyHandler        func(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) ReplyResult
	Logger              func(msg string)
	DeliveryMode        DeliveryMode
	MaxBufferedMessages int
}

// DeliveryMode identifies how the server surfaces Codex messages to Claude.
type DeliveryMode string

const (
	DeliveryPush DeliveryMode = "push"
	DeliveryPull DeliveryMode = "pull"
)

// ReplyResult is what ReplyHandler returns to the server.
type ReplyResult struct {
	Success bool
	Error   string
}

// Server is a stdio MCP server.
type Server struct {
	opts      ServerOptions
	sessionID string

	writeMu        sync.Mutex
	writer         io.Writer
	preServePush   []protocol.BridgeMessage
	ready          chan struct{}
	readyCloseOnce sync.Once

	queueMu         sync.Mutex
	queue           []protocol.BridgeMessage
	droppedMessages int

	notificationSeq atomic.Int64

	closed chan struct{}
}

// NewServer constructs a server.
func NewServer(opts ServerOptions) *Server {
	if opts.Name == "" {
		opts.Name = "agentbridge"
	}
	if opts.Version == "" {
		opts.Version = "0.2.0"
	}
	if opts.DeliveryMode == "" {
		opts.DeliveryMode = DeliveryPush
	}
	if opts.MaxBufferedMessages == 0 {
		opts.MaxBufferedMessages = 100
	}
	return &Server{
		opts:      opts,
		sessionID: fmt.Sprintf("codex_%d", time.Now().UnixMilli()),
		ready:     make(chan struct{}),
		closed:    make(chan struct{}),
	}
}

// Ready is closed once Serve has bound stdio and notifications can be written safely.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Serve reads line-delimited JSON requests from r and writes responses and notifications to w.
func (s *Server) Serve(ctx context.Context, r io.ReadCloser, w io.WriteCloser) error {
	buffered := s.bindWriter(w)
	defer close(s.closed)
	for _, msg := range buffered {
		s.pushViaChannel(msg)
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			if len(line) == 0 {
				continue
			}
			s.handleRequest(ctx, line)
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return scanner.Err()
	}
}

func (s *Server) handleRequest(ctx context.Context, raw []byte) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		s.log("bad request: " + err.Error())
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "initialized", "notifications/initialized":
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	default:
		if req.ID != nil {
			s.respondError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func (s *Server) handleInitialize(req Request) {
	result := InitializeResult{
		ProtocolVersion: "2025-03-26",
		ServerInfo:      ServerInfo{Name: s.opts.Name, Version: s.opts.Version},
		Capabilities:    ServerCapabilities{Experimental: map[string]struct{}{"claude/channel": {}}},
		Instructions:    s.opts.Instructions,
	}
	s.respond(req.ID, result)
}

func (s *Server) respond(id json.RawMessage, result any) {
	s.write(Response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) respondError(id json.RawMessage, code int, message string) {
	s.write(Response{JSONRPC: "2.0", ID: id, Error: &ResponseError{Code: code, Message: message}})
}

func (s *Server) write(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		s.log("marshal: " + err.Error())
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writer == nil {
		return
	}
	_, _ = s.writer.Write(payload)
	_, _ = s.writer.Write([]byte{'\n'})
}

func (s *Server) bindWriter(w io.Writer) []protocol.BridgeMessage {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writer = w
	buffered := append([]protocol.BridgeMessage(nil), s.preServePush...)
	s.preServePush = nil
	s.readyCloseOnce.Do(func() { close(s.ready) })
	return buffered
}

func (s *Server) log(msg string) {
	if s.opts.Logger != nil {
		s.opts.Logger(msg)
	}
}
