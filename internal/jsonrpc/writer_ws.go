package jsonrpc

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
)

type wsWriter struct {
	conn *websocket.Conn
}

// NewWebSocketWriter wraps a coder/websocket connection as a JSON-RPC Writer.
func NewWebSocketWriter(conn *websocket.Conn) Writer {
	return &wsWriter{conn: conn}
}

func (w *wsWriter) WriteJSON(ctx context.Context, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.conn.Write(ctx, websocket.MessageText, payload)
}

func (w *wsWriter) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}
