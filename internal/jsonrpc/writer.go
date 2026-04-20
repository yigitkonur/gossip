package jsonrpc

import "context"

// Writer is the transport-agnostic output side of a JSON-RPC 2.0 connection.
type Writer interface {
	// WriteJSON marshals v to JSON and writes it as a single framed message.
	WriteJSON(ctx context.Context, v any) error
	// Close releases transport resources.
	Close() error
}
