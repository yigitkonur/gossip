package daemon

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestDaemon_StateHandlers_NoDataRace(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})
	d.codex = codex.NewClient(codex.ClientOptions{})
	d.tuiState.MarkBridgeReady()
	d.tuiState.HandleTUIConnected(1)
	d.replyRequired = true

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); d.OnClaudeConnect() }()
		go func() { defer wg.Done(); d.OnClaudeDisconnect("test") }()
		go func(i int) {
			defer wg.Done()
			d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventAgentMessage, ThreadID: "t", TurnID: fmt.Sprintf("%d", i), Text: "hello"})
		}(i)
		go func(i int) {
			defer wg.Done()
			d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: fmt.Sprintf("%d", i)})
		}(i)
	}
	wg.Wait()
}
