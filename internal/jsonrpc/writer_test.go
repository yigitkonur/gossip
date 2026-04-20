package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestStdioWriter_SerializesWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewStdioWriter(nopWriteCloser{Writer: &buf})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = w.WriteJSON(context.Background(), map[string]int{"n": i})
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d: %q", len(lines), buf.String())
	}
	for _, line := range lines {
		var m map[string]int
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("bad JSON line %q: %v", line, err)
		}
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
