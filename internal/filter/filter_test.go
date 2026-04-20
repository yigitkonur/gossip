package filter

import (
	"strings"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestClassify_Markers(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		mode   Mode
		action Action
		marker MarkerLevel
	}{
		{"important", "[IMPORTANT] ship it", ModeFiltered, ActionForward, MarkerImportant},
		{"status", "[STATUS] compiling", ModeFiltered, ActionBuffer, MarkerStatus},
		{"fyi", "[FYI] note", ModeFiltered, ActionDrop, MarkerFYI},
		{"untagged", "no marker", ModeFiltered, ActionForward, MarkerUntagged},
		{"full-mode", "[STATUS] under full mode", ModeFull, ActionForward, MarkerUntagged},
		{"lowercase marker", "[important] ok", ModeFiltered, ActionForward, MarkerImportant},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.input, c.mode)
			if got.Action != c.action || got.Marker != c.marker {
				t.Errorf("got = %+v", got)
			}
		})
	}
}

func TestBridgeContractReminderIncludesSentinels(t *testing.T) {
	for _, needle := range []string{
		"[Git Operations — FORBIDDEN]",
		"Architect→Builder→Critic",
		"Hypothesis→Experiment→Interpretation",
		"My independent view is:",
	} {
		if !strings.Contains(BridgeContractReminder, needle) {
			t.Fatalf("BridgeContractReminder missing %q", needle)
		}
	}
	if !strings.Contains(ReplyRequiredInstruction, "This is a mandatory requirement") {
		t.Fatalf("ReplyRequiredInstruction missing mandatory-reply guidance: %q", ReplyRequiredInstruction)
	}
}

func TestStatusBuffer_FlushesOnThreshold(t *testing.T) {
	var gotSummary protocol.BridgeMessage
	b := NewStatusBuffer(func(m protocol.BridgeMessage) { gotSummary = m }, StatusBufferOptions{FlushThreshold: 3, FlushTimeout: time.Hour})
	for i := 0; i < 3; i++ {
		b.Add(protocol.BridgeMessage{Content: "[STATUS] tick", Source: protocol.SourceCodex})
	}
	if gotSummary.Content == "" {
		t.Fatal("expected flush to run")
	}
	if b.Size() != 0 {
		t.Errorf("queue should be empty after flush")
	}
}

func TestStatusBuffer_ResumeFlushesThresholdReachedWhilePaused(t *testing.T) {
	flushed := make(chan protocol.BridgeMessage, 1)
	b := NewStatusBuffer(func(m protocol.BridgeMessage) { flushed <- m }, StatusBufferOptions{FlushThreshold: 3, FlushTimeout: time.Hour})

	b.Pause()
	for i := 0; i < 3; i++ {
		b.Add(protocol.BridgeMessage{Content: "[STATUS] tick", Source: protocol.SourceCodex})
	}

	select {
	case msg := <-flushed:
		t.Fatalf("flush should stay paused, got %q", msg.Content)
	default:
	}

	b.Resume()

	select {
	case msg := <-flushed:
		if msg.Content == "" {
			t.Fatal("expected resumed flush content")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resumed flush")
	}
}
