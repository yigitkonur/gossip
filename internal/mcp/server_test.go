package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type pipeConn struct {
	in  *bytes.Buffer
	out *bytes.Buffer
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestServer_InitializeDeclaresChannelCapability(t *testing.T) {
	var out safeBuffer
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")

	s := NewServer(ServerOptions{Name: "gossip", Version: "0.2.0"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Serve(ctx, io.NopCloser(stdin), &writeCloser{Writer: &out})
		close(done)
	}()

	deadline := time.After(500 * time.Millisecond)
	for out.Len() == 0 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("no response written, got %q", out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done

	line := firstLine(out.String())
	if line == "" {
		t.Fatalf("no response written, got %q", out.String())
	}

	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal response: %v, raw=%s", err, line)
	}
	resultBytes, _ := json.Marshal(resp.Result)
	var init InitializeResult
	_ = json.Unmarshal(resultBytes, &init)
	if _, ok := init.Capabilities.Experimental["claude/channel"]; !ok {
		t.Errorf("capabilities.experimental[claude/channel] missing: %+v", init.Capabilities)
	}
	if init.ServerInfo.Name != "gossip" {
		t.Errorf("serverInfo.name = %q", init.ServerInfo.Name)
	}
}

type writeCloser struct{ io.Writer }

func (writeCloser) Close() error { return nil }

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func firstLine(s string) string {
	idx := strings.IndexByte(s, '\n')
	if idx < 0 {
		return strings.TrimRight(s, "\n")
	}
	return s[:idx]
}
