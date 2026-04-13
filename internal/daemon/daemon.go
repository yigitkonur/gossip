package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/raysonmeng/agent-bridge/internal/codex"
	"github.com/raysonmeng/agent-bridge/internal/control"
	"github.com/raysonmeng/agent-bridge/internal/filter"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
	"github.com/raysonmeng/agent-bridge/internal/tui"
)

// Options configures a daemon run.
type Options struct {
	StateDir     *statedir.StateDir
	AppPort      int
	ProxyPort    int
	ControlPort  int
	FilterMode   filter.Mode
	IdleShutdown time.Duration
	Logger       func(msg string)
}

// Daemon bundles the supervisor state.
type Daemon struct {
	opts Options

	codex    *codex.Client
	proxy    *codex.Proxy
	control  *control.Server
	tuiState *tui.State
	filter   filter.Mode

	statusBuf *filter.StatusBuffer

	attachedMu        sync.Mutex
	claudeAttached    bool
	idleShutdownTimer *time.Timer
	runCancel         context.CancelFunc
	bridgeReadyAt     atomic.Int64
}

// New constructs a Daemon from Options.
func New(opts Options) *Daemon {
	if opts.AppPort == 0 {
		opts.AppPort = 4500
	}
	if opts.ProxyPort == 0 {
		opts.ProxyPort = 4501
	}
	if opts.ControlPort == 0 {
		opts.ControlPort = 4502
	}
	if opts.FilterMode == "" {
		opts.FilterMode = filter.ModeFiltered
	}
	if opts.IdleShutdown == 0 {
		opts.IdleShutdown = 30 * time.Second
	}
	d := &Daemon{opts: opts, filter: opts.FilterMode}
	d.tuiState = tui.NewState(tui.Options{DisconnectGrace: 2500 * time.Millisecond, Logger: opts.Logger})
	return d
}

// Run blocks until ctx is cancelled, running all layers under an errgroup.
func (d *Daemon) Run(ctx context.Context) error {
	runCtx, runCancel := context.WithCancel(ctx)
	d.runCancel = runCancel
	defer runCancel()

	d.codex = codex.NewClient(codex.ClientOptions{Port: d.opts.AppPort, Logger: d.opts.Logger})
	d.proxy = codex.NewProxy(d.codex)
	d.codex.AttachProxy(d.proxy)
	if err := d.codex.Start(runCtx); err != nil {
		return fmt.Errorf("codex start: %w", err)
	}

	d.proxy.OnTUIConnected = func(id int64) {
		d.tuiState.HandleTUIConnected(id)
		d.cancelIdleShutdown()
		if d.opts.Logger != nil {
			d.opts.Logger(fmt.Sprintf("TUI conn #%d connected", id))
		}
	}
	d.proxy.OnTUIDisconnected = func(id int64) {
		d.tuiState.HandleTUIDisconnected(id)
		d.scheduleIdleShutdown()
		if d.opts.Logger != nil {
			d.opts.Logger(fmt.Sprintf("TUI conn #%d disconnected", id))
		}
	}

	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(d.onStatusFlush, filter.StatusBufferOptions{})

	g, gctx := errgroup.WithContext(runCtx)

	g.Go(func() error {
		srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", d.opts.ProxyPort), Handler: d.proxy}
		go func() { <-gctx.Done(); _ = srv.Shutdown(context.Background()) }()
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	})

	g.Go(func() error {
		srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", d.opts.ControlPort), Handler: d.control.HTTPHandler()}
		go func() { <-gctx.Done(); _ = srv.Shutdown(context.Background()) }()
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	})

	g.Go(func() error { return d.pumpCodexEvents(gctx) })

	err := g.Wait()
	d.cancelIdleShutdown()
	_ = d.codex.Stop(context.Background())
	return err
}

func (d *Daemon) pumpCodexEvents(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-d.codex.Events():
			if !ok {
				return nil
			}
			d.handleCodexEvent(ctx, ev)
		}
	}
}

func (d *Daemon) handleCodexEvent(ctx context.Context, ev codex.Event) {
	switch ev.Kind {
	case codex.EventThreadReady:
		d.tuiState.MarkBridgeReady()
		d.bridgeReadyAt.Store(time.Now().UnixMilli())
		d.broadcastSystem(ctx, "system_ready", "✅ Codex bridge ready (thread "+ev.ThreadID+")")
	case codex.EventTurnStarted:
		d.broadcastSystem(ctx, "system_turn_started", "⏳ Codex is working on the current task.")
	case codex.EventTurnCompleted:
		d.statusBuf.Flush("turn completed")
		d.broadcastSystem(ctx, "system_turn_completed", "✅ Codex finished the current turn. You can reply now if needed.")
	case codex.EventAgentMessage:
		msg, ok := ev.MessageToBridge()
		if !ok {
			return
		}
		result := filter.Classify(msg.Content, d.filter)
		switch result.Action {
		case filter.ActionForward:
			d.control.Broadcast(ctx, msg)
		case filter.ActionBuffer:
			d.statusBuf.Add(msg)
		case filter.ActionDrop:
		}
	case codex.EventApprovalRequest:
		if d.opts.Logger != nil && ev.Approval != nil {
			d.opts.Logger("approval requested: " + ev.Approval.Method)
		}
	}
}

func (d *Daemon) scheduleIdleShutdown() {
	d.cancelIdleShutdown()
	d.attachedMu.Lock()
	attached := d.claudeAttached
	d.attachedMu.Unlock()
	if attached {
		return
	}
	if d.proxy != nil && d.proxy.ConnectionCount() > 0 {
		return
	}
	d.idleShutdownTimer = time.AfterFunc(d.opts.IdleShutdown, func() {
		d.attachedMu.Lock()
		attached := d.claudeAttached
		d.attachedMu.Unlock()
		if attached {
			return
		}
		if d.proxy != nil && d.proxy.ConnectionCount() > 0 {
			return
		}
		if d.runCancel != nil {
			d.runCancel()
		}
	})
}

func (d *Daemon) cancelIdleShutdown() {
	if d.idleShutdownTimer != nil {
		d.idleShutdownTimer.Stop()
		d.idleShutdownTimer = nil
	}
}

func (d *Daemon) onStatusFlush(summary protocol.BridgeMessage) {
	d.control.Broadcast(context.Background(), summary)
}

func (d *Daemon) broadcastSystem(ctx context.Context, id, content string) {
	d.control.Broadcast(ctx, protocol.BridgeMessage{ID: fmt.Sprintf("%s_%d", id, time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: content, Timestamp: time.Now().UnixMilli()})
}

// OnClaudeConnect implements control.Handler.
func (d *Daemon) OnClaudeConnect() {
	d.attachedMu.Lock()
	d.claudeAttached = true
	d.attachedMu.Unlock()
	d.cancelIdleShutdown()
	if d.statusBuf != nil {
		d.statusBuf.Flush("claude reconnected")
	}
	if d.opts.Logger != nil {
		d.opts.Logger("claude frontend attached")
	}
}

// OnClaudeDisconnect implements control.Handler.
func (d *Daemon) OnClaudeDisconnect(reason string) {
	d.attachedMu.Lock()
	d.claudeAttached = false
	d.attachedMu.Unlock()
	d.scheduleIdleShutdown()
	if d.opts.Logger != nil {
		d.opts.Logger("claude frontend detached: " + reason)
	}
}

// OnClaudeToCodex implements control.Handler.
func (d *Daemon) OnClaudeToCodex(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) (bool, string) {
	body := msg.Content + "\n\n" + filter.BridgeContractReminder
	if requireReply {
		body += filter.ReplyRequiredInstruction
	}
	return d.codex.InjectMessage(ctx, body)
}

// Snapshot implements control.Handler.
func (d *Daemon) Snapshot() control.Status {
	tuiConnected := false
	if d.proxy != nil {
		tuiConnected = d.proxy.ConnectionCount() > 0
	}
	threadID := ""
	if d.codex != nil {
		threadID = d.codex.ActiveThreadID()
	}
	queued := 0
	if d.statusBuf != nil {
		queued += d.statusBuf.Size()
	}
	if d.control != nil {
		queued += d.control.QueuedCount()
	}
	return control.Status{
		BridgeReady:        d.tuiState.CanReply(),
		TuiConnected:       tuiConnected,
		ThreadID:           threadID,
		QueuedMessageCount: queued,
		ProxyURL:           fmt.Sprintf("ws://127.0.0.1:%d", d.opts.ProxyPort),
		AppServerURL:       fmt.Sprintf("ws://127.0.0.1:%d", d.opts.AppPort),
		Pid:                os.Getpid(),
	}
}
