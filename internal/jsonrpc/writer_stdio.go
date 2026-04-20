package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"sync"
)

type stdioWriter struct {
	mu sync.Mutex
	w  io.WriteCloser
}

// NewStdioWriter wraps an io.WriteCloser as a newline-delimited JSON-RPC Writer.
func NewStdioWriter(w io.WriteCloser) Writer {
	return &stdioWriter{w: w}
}

func (s *stdioWriter) WriteJSON(_ context.Context, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(payload); err != nil {
		return err
	}
	if _, err := s.w.Write([]byte{'\n'}); err != nil {
		return err
	}
	return nil
}

func (s *stdioWriter) Close() error { return s.w.Close() }
