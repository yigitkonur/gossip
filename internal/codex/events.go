package codex

import (
	"time"

	"github.com/yigitkonur/gossip/internal/protocol"
)

// EventKind classifies a Codex lifecycle event.
type EventKind int

const (
	// EventThreadReady fires once after thread/start returns successfully.
	EventThreadReady EventKind = iota
	// EventTurnStarted fires when Codex begins a turn.
	EventTurnStarted
	// EventTurnCompleted fires when Codex finishes a turn.
	EventTurnCompleted
	// EventAgentMessage delivers an accumulated agentMessage text for a completed item.
	EventAgentMessage
	// EventApprovalRequest fires when the server asks for an approval.
	EventApprovalRequest
	// EventProcessExit fires when the Codex app-server process exits.
	EventProcessExit
)

// Event is a typed value delivered on Client.Events().
type Event struct {
	Kind     EventKind
	ThreadID string
	TurnID   string
	Text     string
	Approval *ApprovalRequest
	ExitCode *int
}

// ApprovalRequest holds the information needed to respond to a server approval.
type ApprovalRequest struct {
	ID     []byte
	Method string
	Params []byte
}

// MessageToBridge converts an Event into a BridgeMessage ready for Claude.
func (e Event) MessageToBridge() (protocol.BridgeMessage, bool) {
	if e.Kind != EventAgentMessage {
		return protocol.BridgeMessage{}, false
	}
	return protocol.BridgeMessage{
		ID:        "codex_" + e.ThreadID + "_" + e.TurnID,
		Source:    protocol.SourceCodex,
		Content:   e.Text,
		Timestamp: time.Now().UnixMilli(),
	}, true
}
