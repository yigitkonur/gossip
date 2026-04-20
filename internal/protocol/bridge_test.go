package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBridgeMessage_RoundTrip(t *testing.T) {
	now := time.Now().UnixMilli()
	in := BridgeMessage{
		ID:        "msg_1",
		Source:    SourceCodex,
		Content:   "hello",
		Timestamp: now,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out BridgeMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestMessageSource_Valid(t *testing.T) {
	cases := []struct {
		in   MessageSource
		want bool
	}{
		{SourceClaude, true},
		{SourceCodex, true},
		{MessageSource("bogus"), false},
	}
	for _, c := range cases {
		if got := c.in.Valid(); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
