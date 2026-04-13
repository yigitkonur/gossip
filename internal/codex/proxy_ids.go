package codex

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

// connID is a per-connection integer assigned when the TUI connects.
type connID int64

type proxyIDEntry struct {
	conn connID
	orig json.RawMessage
}

// proxyIDTable is the central ID-rewriting table for the TUI proxy.
type proxyIDTable struct {
	next atomic.Int64
	mu   sync.Mutex
	m    map[int64]proxyIDEntry
}

func newProxyIDTable() *proxyIDTable {
	t := &proxyIDTable{m: make(map[int64]proxyIDEntry)}
	t.next.Store(100000)
	return t
}

// Allocate reserves a new proxy id, remembers the mapping, and returns the new id.
func (t *proxyIDTable) Allocate(conn connID, orig json.RawMessage) int64 {
	id := t.next.Add(1)
	t.mu.Lock()
	t.m[id] = proxyIDEntry{conn: conn, orig: append(json.RawMessage(nil), orig...)}
	t.mu.Unlock()
	return id
}

// Resolve returns the original conn+id for a proxy id and removes the entry.
func (t *proxyIDTable) Resolve(id int64) (connID, json.RawMessage, bool) {
	t.mu.Lock()
	entry, ok := t.m[id]
	if ok {
		delete(t.m, id)
	}
	t.mu.Unlock()
	return entry.conn, entry.orig, ok
}

// ForgetConn drops every entry belonging to the given conn.
func (t *proxyIDTable) ForgetConn(conn connID) {
	t.mu.Lock()
	for id, entry := range t.m {
		if entry.conn == conn {
			delete(t.m, id)
		}
	}
	t.mu.Unlock()
}
