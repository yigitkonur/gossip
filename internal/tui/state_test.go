package tui

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestState_ReconnectBeforeGraceClearsNotice(t *testing.T) {
	var persistedCalled atomic.Bool
	s := NewState(Options{
		DisconnectGrace:       50 * time.Millisecond,
		OnDisconnectPersisted: func(int64) { persistedCalled.Store(true) },
	})
	s.MarkBridgeReady()
	s.HandleTUIConnected(1)
	s.HandleTUIDisconnected(1)
	s.HandleTUIConnected(2)
	time.Sleep(100 * time.Millisecond)
	if persistedCalled.Load() {
		t.Error("persisted callback should NOT fire after reconnect within grace")
	}
}

func TestState_DisconnectPastGraceFiresCallback(t *testing.T) {
	fired := make(chan int64, 1)
	s := NewState(Options{
		DisconnectGrace:       20 * time.Millisecond,
		OnDisconnectPersisted: func(id int64) { fired <- id },
	})
	s.MarkBridgeReady()
	s.HandleTUIConnected(7)
	s.HandleTUIDisconnected(7)
	select {
	case id := <-fired:
		if id != 7 {
			t.Errorf("id = %d", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("callback did not fire")
	}
}
