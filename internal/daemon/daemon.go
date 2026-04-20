package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
	"github.com/yigitkonur/gossip/internal/statedir"
	"github.com/yigitkonur/gossip/internal/tui"
)

const (
	attachStatusCooldown = 30 * time.Second
	codexStopTimeout     = 3 * time.Second
)

type messageTemplates struct {
	ready   string
	waiting string
}

type stopTimer interface {
	Stop() bool
}

type afterFunc func(time.Duration, func()) stopTimer

type realTimer struct{ *time.Timer }

func (t realTimer) Stop() bool {
	if t.Timer == nil {
		return false
	}
	return t.Timer.Stop()
}

func defaultAfterFunc(d time.Duration, fn func()) stopTimer {
	return realTimer{Timer: time.AfterFunc(d, fn)}
}

// Options configures a daemon run.
type Options struct {
	StateDir        *statedir.StateDir
	AppPort         int
	ProxyPort       int
	ControlPort     int
	FilterMode      filter.Mode
	AttentionWindow time.Duration
	IdleShutdown    time.Duration
	Logger          func(msg string)
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
	stateMu   sync.Mutex
	afterFunc afterFunc

	claudeAttached           bool
	idleShutdownTimer        stopTimer
	claudeDisconnectTimer    stopTimer
	attentionWindowTimer     stopTimer
	attentionWindowActive    bool
	attentionWindowConnID    int64
	claudeOnlineNoticeSent   bool
	claudeOfflineNoticeShown bool
	lastAttachStatusSent     time.Time
	runCancel                context.CancelFunc
	replyRequired            bool
	replyReceivedDuringTurn  bool
	messageTemplates         messageTemplates
	bridgeReadyAt            atomic.Int64
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
	if opts.AttentionWindow == 0 {
		opts.AttentionWindow = 15 * time.Second
	}
	if opts.IdleShutdown == 0 {
		opts.IdleShutdown = 30 * time.Second
	}
	d := &Daemon{opts: opts, filter: opts.FilterMode, afterFunc: defaultAfterFunc}
	d.messageTemplates = messageTemplates{
		ready:   "✅ Codex TUI connected ({thread_id}). Bridge ready.",
		waiting: fmt.Sprintf("⏳ Waiting for Codex TUI to connect. Run in another terminal:\ncodex --enable tui_app_server --remote ws://127.0.0.1:%d", opts.ProxyPort),
	}
	d.tuiState = tui.NewState(tui.Options{
		DisconnectGrace: 2500 * time.Millisecond,
		Logger:          opts.Logger,
		OnDisconnectPersisted: func(id int64) {
			if d.control != nil {
				d.control.Broadcast(context.Background(), protocol.BridgeMessage{ID: fmt.Sprintf("system_tui_disconnected_%d", time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: fmt.Sprintf("⚠️ Codex TUI disconnected (conn #%d). Codex is still running in the background — reconnect the TUI to resume.", id), Timestamp: time.Now().UnixMilli()})
			}
		},
		OnReconnectAfterNotice: func(id int64) {
			if d.control != nil {
				d.control.Broadcast(context.Background(), protocol.BridgeMessage{ID: fmt.Sprintf("system_tui_reconnected_%d", time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: fmt.Sprintf("✅ Codex TUI reconnected (conn #%d). Bridge restored, communication can continue.", id), Timestamp: time.Now().UnixMilli()})
			}
			if d.codex != nil {
				_, _ = d.codex.InjectMessage(context.Background(), "✅ Claude Code is still online, bridge restored. Bidirectional communication can continue.")
			}
		},
	})
	return d
}

// SetReadyMessageTemplate overrides the ready notification template.
func (d *Daemon) SetReadyMessageTemplate(template string) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.messageTemplates.ready = template
}

// SetWaitingMessageTemplate overrides the waiting notification template.
func (d *Daemon) SetWaitingMessageTemplate(template string) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.messageTemplates.waiting = template
}

func (d *Daemon) currentReadyMessage(threadID string) string {
	d.stateMu.Lock()
	template := d.messageTemplates.ready
	d.stateMu.Unlock()
	return strings.ReplaceAll(template, "{thread_id}", threadID)
}

func (d *Daemon) currentWaitingMessage() string {
	d.stateMu.Lock()
	template := d.messageTemplates.waiting
	d.stateMu.Unlock()
	return strings.ReplaceAll(template, "{thread_id}", d.threadID())
}

func (d *Daemon) threadID() string {
	if d.codex == nil {
		return ""
	}
	return d.codex.ActiveThreadID()
}

func (d *Daemon) shouldEmitAttachStatus(now time.Time, queuedCount int) bool {
	d.stateMu.Lock()
	rapidReattach := !d.lastAttachStatusSent.IsZero() && now.Sub(d.lastAttachStatusSent) < attachStatusCooldown
	d.lastAttachStatusSent = now
	d.stateMu.Unlock()
	return queuedCount == 0 && !rapidReattach
}

// Run blocks until ctx is cancelled, running all layers under an errgroup.
func (d *Daemon) Run(ctx context.Context) error {
	runCtx, runCancel := context.WithCancel(ctx)
	d.stateMu.Lock()
	d.runCancel = runCancel
	d.stateMu.Unlock()
	defer runCancel()

	d.codex = codex.NewClient(codex.ClientOptions{Port: d.opts.AppPort, Logger: d.opts.Logger})
	d.proxy = codex.NewProxy(d.codex)
	d.codex.AttachProxy(d.proxy)
	if err := d.codex.Start(runCtx); err != nil {
		return fmt.Errorf("codex start: %w", err)
	}
	d.writeStatusFile()
	d.writePortsFile()
	defer d.removeStatusFile()
	defer d.removePortsFile()

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

	// These fields (codex, proxy, control, statusBuf) are assigned above and
	// never modified after this point. The go statement inside errgroup.Go()
	// creates a happens-before edge (Go Memory Model §Goroutine creation),
	// so Snapshot() may read them without holding stateMu.
	g, gctx := errgroup.WithContext(runCtx)

	g.Go(func() error {
		srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", d.opts.ProxyPort), Handler: d.proxy}
		go func() {
			<-gctx.Done()
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutCancel()
			if err := srv.Shutdown(shutCtx); err != nil {
				srv.Close()
			}
		}()
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})

	g.Go(func() error {
		srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", d.opts.ControlPort), Handler: d.control.HTTPHandler()}
		go func() {
			<-gctx.Done()
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutCancel()
			if err := srv.Shutdown(shutCtx); err != nil {
				srv.Close()
			}
		}()
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})

	g.Go(func() error { return d.pumpCodexEvents(gctx) })

	err := g.Wait()
	d.clearAttentionWindow("daemon stopped")
	d.cancelIdleShutdown()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), codexStopTimeout)
	defer stopCancel()
	_ = d.codex.Stop(stopCtx)
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
		d.broadcastSystem(ctx, "system_ready", d.currentReadyMessage(ev.ThreadID))
	case codex.EventTurnStarted:
		d.broadcastSystem(ctx, "system_turn_started", "⏳ Codex is working on the current task.")
	case codex.EventTurnCompleted:
		d.statusBuf.Flush("turn completed")
		d.stateMu.Lock()
		replyRequired := d.replyRequired
		replyReceived := d.replyReceivedDuringTurn
		d.replyRequired = false
		d.replyReceivedDuringTurn = false
		d.stateMu.Unlock()
		if replyRequired && !replyReceived {
			d.broadcastSystem(ctx, "system_reply_missing", "⚠️ Codex completed the turn without sending a reply while require_reply was set.")
		}
		d.broadcastSystem(ctx, "system_turn_completed", "✅ Codex finished the current turn. You can reply now if needed.")
		d.startAttentionWindow(0)
	case codex.EventAgentMessage:
		msg, ok := ev.MessageToBridge()
		if !ok {
			return
		}
		d.stateMu.Lock()
		replyRequired := d.replyRequired
		if replyRequired {
			d.replyReceivedDuringTurn = true
		}
		d.stateMu.Unlock()
		if replyRequired {
			d.statusBuf.Flush("reply-required message arrived")
			d.control.Broadcast(ctx, msg)
			if marker, _ := filter.ParseMarker(msg.Content); marker == filter.MarkerImportant {
				d.startAttentionWindow(0)
			}
			return
		}
		result := filter.Classify(msg.Content, d.filter)
		switch result.Action {
		case filter.ActionForward:
			d.control.Broadcast(ctx, msg)
			if result.Marker == filter.MarkerImportant {
				d.startAttentionWindow(0)
			}
		case filter.ActionBuffer:
			d.statusBuf.Add(msg)
		case filter.ActionDrop:
		}
	case codex.EventApprovalRequest:
		if d.opts.Logger != nil && ev.Approval != nil {
			d.opts.Logger("approval requested: " + ev.Approval.Method)
		}
	case codex.EventProcessExit:
		d.tuiState.HandleCodexExit()
		d.broadcastSystem(ctx, "system_codex_exit", "⚠️ Codex app-server exited. Gossip daemon is still running, but Codex needs to be restarted.")
	}
}

func (d *Daemon) scheduleIdleShutdown() {
	d.stateMu.Lock()
	if d.idleShutdownTimer != nil {
		d.idleShutdownTimer.Stop()
		d.idleShutdownTimer = nil
	}
	if d.claudeAttached {
		d.stateMu.Unlock()
		return
	}
	if d.proxy != nil && d.proxy.ConnectionCount() > 0 {
		d.stateMu.Unlock()
		return
	}
	var timer stopTimer
	timer = d.afterFunc(d.opts.IdleShutdown, func() {
		d.stateMu.Lock()
		if d.idleShutdownTimer != timer {
			d.stateMu.Unlock()
			return
		}
		d.idleShutdownTimer = nil
		attached := d.claudeAttached
		cancel := d.runCancel // read under stateMu
		d.stateMu.Unlock()
		if attached {
			return
		}
		if d.proxy != nil && d.proxy.ConnectionCount() > 0 {
			return
		}
		if cancel != nil {
			cancel()
		}
	})
	d.idleShutdownTimer = timer
	d.stateMu.Unlock()
}

func (d *Daemon) cancelIdleShutdown() {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.idleShutdownTimer != nil {
		d.idleShutdownTimer.Stop()
		d.idleShutdownTimer = nil
	}
}

func (d *Daemon) scheduleClaudeDisconnectNotification() {
	d.stateMu.Lock()
	if d.claudeDisconnectTimer != nil {
		d.claudeDisconnectTimer.Stop()
		d.claudeDisconnectTimer = nil
	}
	var timer stopTimer
	timer = d.afterFunc(5*time.Second, func() {
		d.stateMu.Lock()
		if d.claudeDisconnectTimer != timer {
			d.stateMu.Unlock()
			return
		}
		d.claudeDisconnectTimer = nil
		attached := d.claudeAttached
		onlineNoticeSent := d.claudeOnlineNoticeSent
		codexClient := d.codex
		d.stateMu.Unlock()
		if attached || !d.tuiState.CanReply() || !onlineNoticeSent || codexClient == nil {
			return
		}
		_, _ = codexClient.InjectMessage(context.Background(), "⚠️ Claude Code went offline. Gossip is still running in the background; it will reconnect automatically when Claude reopens.")
		d.stateMu.Lock()
		d.claudeOnlineNoticeSent = false
		d.claudeOfflineNoticeShown = true
		d.stateMu.Unlock()
	})
	d.claudeDisconnectTimer = timer
	d.stateMu.Unlock()
}

func (d *Daemon) clearPendingClaudeDisconnect() {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if d.claudeDisconnectTimer != nil {
		d.claudeDisconnectTimer.Stop()
		d.claudeDisconnectTimer = nil
	}
}

func (d *Daemon) writeStatusFile() {
	if d.opts.StateDir == nil {
		return
	}
	payload := fmt.Sprintf("{\n  \"proxyUrl\": \"ws://127.0.0.1:%d\",\n  \"appServerUrl\": \"ws://127.0.0.1:%d\",\n  \"controlPort\": %d,\n  \"pid\": %d\n}\n", d.opts.ProxyPort, d.opts.AppPort, d.opts.ControlPort, os.Getpid())
	_ = os.WriteFile(d.opts.StateDir.StatusFile(), []byte(payload), 0o644)
}

func (d *Daemon) writePortsFile() {
	if d.opts.StateDir == nil {
		return
	}
	payload := fmt.Sprintf("{\n  \"controlPort\": %d,\n  \"appPort\": %d,\n  \"proxyPort\": %d\n}\n", d.opts.ControlPort, d.opts.AppPort, d.opts.ProxyPort)
	_ = os.WriteFile(d.opts.StateDir.PortsFile(), []byte(payload), 0o644)
}

func (d *Daemon) removeStatusFile() {
	if d.opts.StateDir != nil {
		_ = os.Remove(d.opts.StateDir.StatusFile())
	}
}

func (d *Daemon) removePortsFile() {
	if d.opts.StateDir != nil {
		_ = os.Remove(d.opts.StateDir.PortsFile())
	}
}

func (d *Daemon) onStatusFlush(summary protocol.BridgeMessage) {
	d.control.Broadcast(context.Background(), summary)
}

func (d *Daemon) broadcastSystem(ctx context.Context, id, content string) {
	if d.control == nil {
		if d.opts.Logger != nil {
			d.opts.Logger("control server unavailable; skipping system broadcast: " + id)
		}
		return
	}
	d.control.Broadcast(ctx, protocol.BridgeMessage{ID: fmt.Sprintf("%s_%d", id, time.Now().UnixMilli()), Source: protocol.SourceCodex, Content: content, Timestamp: time.Now().UnixMilli()})
}

// OnClaudeConnect implements control.Handler.
func (d *Daemon) OnClaudeConnect() {
	d.stateMu.Lock()
	d.claudeAttached = true
	d.stateMu.Unlock()
	d.cancelIdleShutdown()
	d.clearPendingClaudeDisconnect()
	if d.statusBuf != nil {
		d.statusBuf.Flush("claude reconnected")
	}
	queuedCount := 0
	if d.control != nil {
		queuedCount = d.control.QueuedCount()
	}
	shouldEmitAttachStatus := d.shouldEmitAttachStatus(time.Now(), queuedCount)
	d.stateMu.Lock()
	needNotify := d.tuiState.CanReply() && (!d.claudeOnlineNoticeSent || d.claudeOfflineNoticeShown) && d.codex != nil
	codexClient := d.codex
	threadID := d.threadID()
	d.stateMu.Unlock()
	if shouldEmitAttachStatus {
		if d.tuiState.CanReply() {
			d.broadcastSystem(context.Background(), "system_ready", d.currentReadyMessage(threadID))
		} else {
			d.broadcastSystem(context.Background(), "system_waiting", d.currentWaitingMessage())
		}
	}
	if needNotify {
		_, _ = codexClient.InjectMessage(context.Background(), "✅ Claude Code is online, bridge restored. Bidirectional communication can continue.")
		d.stateMu.Lock()
		d.claudeOnlineNoticeSent = true
		d.claudeOfflineNoticeShown = false
		d.stateMu.Unlock()
	}
	if d.opts.Logger != nil {
		d.opts.Logger("claude frontend attached")
	}
}

func (d *Daemon) OnClaudeDisconnect(reason string) {
	d.stateMu.Lock()
	d.claudeAttached = false
	d.stateMu.Unlock()
	d.scheduleIdleShutdown()
	d.scheduleClaudeDisconnectNotification()
	if d.opts.Logger != nil {
		d.opts.Logger("claude frontend detached: " + reason)
	}
}

func (d *Daemon) OnClaudeToCodex(ctx context.Context, msg protocol.BridgeMessage, requireReply bool) (bool, string) {
	if !d.tuiState.CanReply() {
		return false, "Codex is not ready. Wait for TUI to connect and create a thread."
	}
	body := msg.Content + "\n\n" + filter.BridgeContractReminder
	if requireReply {
		body += filter.ReplyRequiredInstruction
		d.stateMu.Lock()
		d.replyRequired = true
		d.replyReceivedDuringTurn = false
		d.stateMu.Unlock()
	}
	ok, errMsg := d.codex.InjectMessage(ctx, body)
	if ok {
		d.clearAttentionWindow("claude replied")
	}
	return ok, errMsg
}

func (d *Daemon) Snapshot() control.Status {
	tuiConnected := false
	if d.proxy != nil {
		tuiConnected = d.proxy.ConnectionCount() > 0
	}
	threadID := d.threadID()
	queued := 0
	if d.statusBuf != nil {
		queued += d.statusBuf.Size()
	}
	dropped := 0
	if d.control != nil {
		queued += d.control.QueuedCount()
		dropped = d.control.DroppedCount()
	}
	return control.Status{
		BridgeReady:         d.tuiState.CanReply(),
		TuiConnected:        tuiConnected,
		ThreadID:            threadID,
		QueuedMessageCount:  queued,
		DroppedMessageCount: dropped,
		ProxyURL:            fmt.Sprintf("ws://127.0.0.1:%d", d.opts.ProxyPort),
		AppServerURL:        fmt.Sprintf("ws://127.0.0.1:%d", d.opts.AppPort),
		Pid:                 os.Getpid(),
	}
}

func (d *Daemon) startAttentionWindow(connID int64) {
	d.stateMu.Lock()
	if d.statusBuf == nil {
		d.stateMu.Unlock()
		return
	}
	if d.attentionWindowTimer != nil {
		d.attentionWindowTimer.Stop()
		d.attentionWindowTimer = nil
	}
	d.attentionWindowActive = true
	d.attentionWindowConnID = connID
	statusBuf := d.statusBuf
	var timer stopTimer
	timer = d.afterFunc(d.opts.AttentionWindow, func() {
		d.clearAttentionWindow("expired")
	})
	d.attentionWindowTimer = timer
	d.stateMu.Unlock()
	statusBuf.Pause()
	if d.opts.Logger != nil {
		d.opts.Logger(fmt.Sprintf("attention window started (%dms, conn=%d)", d.opts.AttentionWindow.Milliseconds(), connID))
	}
}

func (d *Daemon) clearAttentionWindow(reason string) {
	d.stateMu.Lock()
	active := d.attentionWindowActive
	timer := d.attentionWindowTimer
	connID := d.attentionWindowConnID
	statusBuf := d.statusBuf
	d.attentionWindowActive = false
	d.attentionWindowConnID = 0
	d.attentionWindowTimer = nil
	d.stateMu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if active && statusBuf != nil {
		statusBuf.Resume()
		if d.opts.Logger != nil {
			d.opts.Logger(fmt.Sprintf("attention window cleared (%s, conn=%d)", reason, connID))
		}
	}
}
