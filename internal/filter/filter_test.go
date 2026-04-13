package filter

import (
	"testing"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/protocol"
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
