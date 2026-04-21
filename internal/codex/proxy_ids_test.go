package codex

import (
	"encoding/json"
	"testing"
)

func TestProxyIDTable_AllocateAndResolve(t *testing.T) {
	tbl := newProxyIDTable()

	proxyID := tbl.Allocate(connID(1), json.RawMessage(`5`))
	if proxyID != 100000 {
		t.Fatalf("proxyID = %d, want 100000", proxyID)
	}

	cid, orig, ok := tbl.Resolve(proxyID)
	if !ok {
		t.Fatalf("Resolve(%d) = !ok", proxyID)
	}
	if cid != connID(1) {
		t.Errorf("connID = %d, want 1", cid)
	}
	if string(orig) != "5" {
		t.Errorf("orig = %s", orig)
	}

	if _, _, ok := tbl.Resolve(proxyID); ok {
		t.Errorf("Resolve should be single-shot")
	}
}

func TestProxyIDTable_AllocateUnique(t *testing.T) {
	tbl := newProxyIDTable()
	seen := make(map[int64]struct{})
	for i := 0; i < 1000; i++ {
		id := tbl.Allocate(connID(1), json.RawMessage("1"))
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestProxyIDTable_ForgetID(t *testing.T) {
	tbl := newProxyIDTable()
	proxyID := tbl.Allocate(connID(1), json.RawMessage(`7`))

	tbl.ForgetID(proxyID)

	if _, _, ok := tbl.Resolve(proxyID); ok {
		t.Fatalf("Resolve(%d) unexpectedly succeeded after ForgetID", proxyID)
	}
}
