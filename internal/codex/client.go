package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/jsonrpc"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
)

// ClientOptions configures a Codex Client.
type ClientOptions struct {
	Binary string
	Port   int
	Logger func(msg string)
}

// Client is the high-level Layer C subprocess owner.
type Client struct {
	opts ClientOptions

	proc   *Process
	dialer *Dialer
	rpc    *jsonrpc.Client
	proxy  *Proxy

	threadID       atomic.Value
	turnInProgress atomic.Bool

	agentMessageMu   sync.Mutex
	agentMessageBufs map[string]*strings.Builder
	turnMu           sync.Mutex
	activeTurnIDs    map[string]struct{}

	events chan Event
}

// NewClient returns a Client that spawns and drives a codex app-server process.
func NewClient(opts ClientOptions) *Client {
	if opts.Port == 0 {
		opts.Port = 4500
	}
	c := &Client{
		opts:             opts,
		events:           make(chan Event, 64),
		agentMessageBufs: make(map[string]*strings.Builder),
		activeTurnIDs:    make(map[string]struct{}),
	}
	c.threadID.Store("")
	return c
}

// AttachProxy binds a TUI proxy so upstream messages are routed to both the RPC client and proxy.
func (c *Client) AttachProxy(proxy *Proxy) {
	c.proxy = proxy
}

// Events returns the read-only event channel.
func (c *Client) Events() <-chan Event { return c.events }

// ActiveThreadID returns the id of the current thread, or an empty string if none.
func (c *Client) ActiveThreadID() string {
	v, _ := c.threadID.Load().(string)
	return v
}

// TurnInProgress reports whether a Codex turn is currently running.
func (c *Client) TurnInProgress() bool { return c.turnInProgress.Load() }

// Start boots the subprocess, dials the WebSocket, initializes, and opens a thread.
func (c *Client) Start(ctx context.Context) error {
	c.proc = NewProcess(ProcessOptions{
		Binary: c.opts.Binary,
		Port:   c.opts.Port,
		Logger: func(stream, line string) {
			if c.opts.Logger != nil {
				c.opts.Logger("[codex-" + stream + "] " + line)
			}
		},
	})
	if err := c.proc.Start(ctx); err != nil {
		return fmt.Errorf("codex subprocess: %w", err)
	}

	c.dialer = NewDialer(c.proc.WebSocketURL(), DialerOptions{
		OnMessage: c.handleIncoming,
		OnConnect: func() {
			if c.opts.Logger != nil {
				c.opts.Logger("connected to codex app-server")
			}
		},
		OnDisconnect: func(err error) {
			if c.opts.Logger != nil {
				c.opts.Logger("codex WS disconnected: " + err.Error())
			}
		},
		Logger: c.opts.Logger,
	})
	if err := c.dialer.ConnectOnce(ctx); err != nil {
		return err
	}

	c.rpc = jsonrpc.NewClient(&dialerWriter{d: c.dialer})

	go c.consumeNotifications(ctx)
	go c.consumeServerRequests(ctx)
	go func() {
		_ = c.dialer.Run(ctx)
	}()
	go c.watchProcessExit()

	if err := c.initialize(ctx); err != nil {
		return err
	}
	return c.startThread(ctx)
}

// Stop gracefully shuts down the subprocess and held connection.

func (c *Client) watchProcessExit() {
	if c.proc == nil {
		return
	}
	done := c.proc.Done()
	if done == nil {
		return
	}
	<-done
	if c.dialer != nil {
		_ = c.dialer.Close()
	}
	c.turnMu.Lock()
	c.activeTurnIDs = make(map[string]struct{})
	c.turnMu.Unlock()
	c.turnInProgress.Store(false)
	c.emit(Event{Kind: EventProcessExit, ThreadID: c.ActiveThreadID()})
}

func (c *Client) Stop(ctx context.Context) error {
	if c.dialer != nil {
		_ = c.dialer.Close()
	}
	if c.proc != nil {
		return c.proc.Stop(ctx)
	}
	return nil
}

// InjectMessage sends a user-turn input into the active thread via turn/start.
func (c *Client) InjectMessage(ctx context.Context, text string) (bool, string) {
	threadID := c.ActiveThreadID()
	if threadID == "" {
		return false, "no active thread"
	}
	if c.turnInProgress.Load() {
		return false, "codex turn already in progress"
	}
	params := protocol.TurnStartParams{
		ThreadID: threadID,
		Input:    []protocol.UserInput{{Type: "text", Text: text}},
	}
	_, err := c.rpc.Call(ctx, protocol.MethodTurnStart, params)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (c *Client) initialize(ctx context.Context) error {
	params := protocol.InitializeParams{
		ClientInfo: protocol.ClientInfo{Name: "agentbridge", Version: "0.2.0"},
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.rpc.Call(callCtx, protocol.MethodInitialize, params)
	return err
}

func (c *Client) startThread(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := c.rpc.Call(callCtx, protocol.MethodThreadStart, protocol.ThreadStartParams{})
	if err != nil {
		return err
	}
	var resp protocol.ThreadStartResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return err
	}
	if resp.Thread.ID == "" {
		return errors.New("codex thread/start returned no thread id")
	}
	c.threadID.Store(resp.Thread.ID)
	c.emit(Event{Kind: EventThreadReady, ThreadID: resp.Thread.ID})
	return nil
}

func (c *Client) handleIncoming(payload []byte) {
	c.rpc.HandleIncoming(payload)
	if c.proxy != nil {
		c.proxy.HandleUpstreamMessage(context.Background(), payload)
	}
}

func (c *Client) consumeNotifications(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-c.rpc.Notifications():
			if !ok {
				return
			}
			c.dispatchNotification(n)
		}
	}
}

func (c *Client) dispatchNotification(n jsonrpc.Notification) {
	switch n.Method {
	case protocol.MethodTurnStarted:
		var p protocol.TurnStartedParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			return
		}
		turnID := p.Turn.ID
		if turnID == "" {
			turnID = p.TurnID
		}
		wasInProgress := c.TurnInProgress()
		c.markTurnStarted(turnID)
		if !wasInProgress {
			threadID := p.ThreadID
			if threadID == "" {
				threadID = c.ActiveThreadID()
			}
			c.emit(Event{Kind: EventTurnStarted, ThreadID: threadID, TurnID: turnID})
		}
	case protocol.MethodTurnCompleted:
		var p protocol.TurnCompletedParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			return
		}
		turnID := p.Turn.ID
		if turnID == "" {
			turnID = p.TurnID
		}
		wasInProgress := c.TurnInProgress()
		c.markTurnCompleted(turnID)
		if wasInProgress && !c.TurnInProgress() {
			c.flushPendingAgentMessages()
			threadID := p.ThreadID
			if threadID == "" {
				threadID = c.ActiveThreadID()
			}
			c.emit(Event{Kind: EventTurnCompleted, ThreadID: threadID, TurnID: turnID})
		}
	case protocol.MethodItemAgentMessageDelta:
		var p protocol.AgentMessageDeltaParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			return
		}
		c.appendDelta(p)
	case protocol.MethodItemCompleted:
		var p protocol.ItemCompletedParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			return
		}
		c.finalizeItem(p)
	}
}

func (c *Client) appendDelta(p protocol.AgentMessageDeltaParams) {
	key := p.TurnID + "_" + p.ItemID
	c.agentMessageMu.Lock()
	defer c.agentMessageMu.Unlock()
	buf, ok := c.agentMessageBufs[key]
	if !ok {
		buf = &strings.Builder{}
		c.agentMessageBufs[key] = buf
	}
	buf.WriteString(p.Delta)
}

func (c *Client) finalizeItem(p protocol.ItemCompletedParams) {
	if p.Item.Type != "" && p.Item.Type != "agentMessage" {
		return
	}
	key := p.TurnID + "_" + p.Item.ID
	c.agentMessageMu.Lock()
	buf, ok := c.agentMessageBufs[key]
	if ok {
		delete(c.agentMessageBufs, key)
	}
	c.agentMessageMu.Unlock()
	text := extractItemContent(p.Item)
	if text == "" && ok && buf.Len() > 0 {
		text = buf.String()
	}
	if text == "" {
		return
	}
	c.emit(Event{Kind: EventAgentMessage, ThreadID: p.ThreadID, TurnID: p.TurnID, Text: text})
}

func (c *Client) flushPendingAgentMessages() {
	c.agentMessageMu.Lock()
	buffers := c.agentMessageBufs
	c.agentMessageBufs = make(map[string]*strings.Builder)
	c.agentMessageMu.Unlock()
	for key, buf := range buffers {
		if buf.Len() == 0 {
			continue
		}
		parts := strings.SplitN(key, "_", 2)
		turnID := ""
		if len(parts) > 0 {
			turnID = parts[0]
		}
		c.emit(Event{Kind: EventAgentMessage, ThreadID: c.ActiveThreadID(), TurnID: turnID, Text: buf.String()})
	}
}

func (c *Client) consumeServerRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-c.rpc.ServerRequests():
			if !ok {
				return
			}
			if c.proxy != nil {
				c.emit(Event{Kind: EventApprovalRequest, Approval: &ApprovalRequest{ID: req.ID, Method: req.Method, Params: req.Params}})
				continue
			}
			_ = c.rpc.Respond(ctx, req.ID, map[string]string{"decision": "accept"})
			c.emit(Event{Kind: EventApprovalRequest, Approval: &ApprovalRequest{ID: req.ID, Method: req.Method, Params: req.Params}})
		}
	}
}

func extractItemContent(item protocol.Item) string {
	if item.Text != "" {
		return item.Text
	}
	if len(item.Content) == 0 {
		return ""
	}
	var parts []string
	for _, content := range item.Content {
		if content.Type == "text" && content.Text != "" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "")
}

func (c *Client) markTurnStarted(turnID string) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if turnID == "" {
		turnID = fmt.Sprintf("unknown:%d", time.Now().UnixNano())
	}
	c.activeTurnIDs[turnID] = struct{}{}
	c.turnInProgress.Store(len(c.activeTurnIDs) > 0)
}

func (c *Client) markTurnCompleted(turnID string) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if turnID == "" {
		for id := range c.activeTurnIDs {
			delete(c.activeTurnIDs, id)
		}
	} else {
		delete(c.activeTurnIDs, turnID)
	}
	c.turnInProgress.Store(len(c.activeTurnIDs) > 0)
}

func (c *Client) emit(e Event) {
	select {
	case c.events <- e:
	default:
		if c.opts.Logger != nil {
			c.opts.Logger("event channel full; dropping " + fmt.Sprintf("%d", e.Kind))
		}
	}
}

type dialerWriter struct{ d *Dialer }

func (w *dialerWriter) WriteJSON(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.d.Write(ctx, b)
}

func (w *dialerWriter) Close() error { return w.d.Close() }

// ServeProxy starts an http.Server on the given address for the TUI proxy.
func ServeProxy(ctx context.Context, addr string, proxy *Proxy) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: proxy,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
