// Package jsonrpc provides transport-agnostic JSON-RPC 2.0 client primitives
// for the Codex app-server protocol. It is built on internal/protocol.Envelope.
package jsonrpc

import (
	"encoding/json"
	"strconv"
	"strings"
)

// NormalizeID converts a JSON-RPC id RawMessage into a stable string key.
func NormalizeID(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", false
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}
		return s, true
	}
	if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return strconv.FormatInt(i, 10), true
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return strconv.FormatInt(int64(f), 10), true
	}
	return "", false
}
