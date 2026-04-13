// Package protocol defines the wire types exchanged between AgentBridge,
// Claude Code, and Codex app-server.
package protocol

// MessageSource identifies which side originated a BridgeMessage.
type MessageSource string

const (
	SourceClaude MessageSource = "claude"
	SourceCodex  MessageSource = "codex"
)

// Valid reports whether the source is one of the two recognised values.
func (s MessageSource) Valid() bool {
	return s == SourceClaude || s == SourceCodex
}

// BridgeMessage is the unit of communication between the two agents, with Timestamp in Unix milliseconds.
type BridgeMessage struct {
	ID        string        `json:"id"`
	Source    MessageSource `json:"source"`
	Content   string        `json:"content"`
	Timestamp int64         `json:"timestamp"`
}
