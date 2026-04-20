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

	"github.com/yigitkonur/gossip/internal/protocol"
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

	cancelServe context.CancelCauseFunc

	closed chan struct{}
}

// NewServer constructs a server.
func NewServer(opts ServerOptions) *Server {
	if opts.Name == "" {
		opts.Name = "gossip"
	}
	if opts.Version == "" {
		opts.Version = "0.2.0"
	}
	if opts.DeliveryMode == "" {
		opts.DeliveryMode = DeliveryPull
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
	serveCtx, cancel := context.WithCancelCause(ctx)
	s.cancelServe = cancel
	defer cancel(nil)

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
			s.handleRequest(serveCtx, line)
		}
	}()
	select {
	case <-serveCtx.Done():
		// Close the reader to unblock scanner.Scan() in the goroutine above.
		// Per Go stdlib, closing the read end of a pipe causes Scan() to
		// return false, which closes the done channel.
		r.Close()
		<-done // wait for scanner goroutine to exit cleanly
		return context.Cause(serveCtx)
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
	_ = s.write(Response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) respondError(id json.RawMessage, code int, message string) {
	_ = s.write(Response{JSONRPC: "2.0", ID: id, Error: &ResponseError{Code: code, Message: message}})
}

func (s *Server) write(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		s.log("marshal: " + err.Error())
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writer == nil {
		return nil
	}
	// Single write with appended newline — atomic at the OS level for
	// payloads under PIPE_BUF (4KB on POSIX), and correct under writeMu
	// for larger payloads.
	buf := append(payload, '\n')
	if _, err := s.writer.Write(buf); err != nil {
		s.log("write to stdout: " + err.Error())
		if s.cancelServe != nil {
			s.cancelServe(fmt.Errorf("write to stdout: %w", err))
		}
		return err
	}
	return nil
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
