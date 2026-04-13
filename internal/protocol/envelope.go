package protocol

import "encoding/json"

// EnvelopeKind classifies a JSON-RPC 2.0 message by field presence.
type EnvelopeKind int

const (
	// KindUnknown is the zero value — malformed or empty.
	KindUnknown EnvelopeKind = iota
	// KindResponse is a response to a prior client-initiated request. Has id, no method.
	KindResponse
	// KindNotification is a one-way server push. Has method, no id.
	KindNotification
	// KindServerRequest is a server-initiated request needing a client response. Has id AND method.
	KindServerRequest
)

// Envelope is the generic JSON-RPC 2.0 wire shape used for decoding any message.
// Params, Result, Error, and ID are kept as RawMessage so callers can decode selectively.
type Envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the standard JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Kind classifies this envelope using the presence of id and method.
func (e *Envelope) Kind() EnvelopeKind {
	hasID := len(e.ID) > 0 && string(e.ID) != "null"
	hasMethod := e.Method != ""
	switch {
	case hasID && hasMethod:
		return KindServerRequest
	case hasMethod:
		return KindNotification
	case hasID:
		return KindResponse
	default:
		return KindUnknown
	}
}
