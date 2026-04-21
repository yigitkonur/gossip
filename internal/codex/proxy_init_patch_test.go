package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestPatchInitializeAlreadyError_UsesCachedResult(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	cached := json.RawMessage(`{"userAgent":"test/0","codexHome":"/tmp","platformFamily":"unix","platformOs":"macos"}`)
	c.initializeResult.Store(cached)

	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[100001] = pendingClientRequest{conn: 1, method: protocol.MethodInitialize}
	p.mu.Unlock()

	errPayload := []byte(`{"id":100001,"error":{"code":-32600,"message":"Already initialized"}}`)
	var env protocol.Envelope
	if err := json.Unmarshal(errPayload, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	patched, patchedEnv := p.patchInitializeAlreadyError(100001, errPayload, env)
	if patchedEnv.Error != nil {
		t.Fatalf("patched envelope still has error: %+v", patchedEnv.Error)
	}
	if patchedEnv.Result == nil {
		t.Fatalf("patched envelope missing result")
	}
	if !strings.Contains(string(patched), `"codexHome":"/tmp"`) {
		t.Fatalf("patched payload missing cached result fields: %s", patched)
	}
}

func TestPatchInitializeAlreadyError_OnlyPatchesMatchingMethod(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	c.initializeResult.Store(json.RawMessage(`{"x":1}`))

	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[100002] = pendingClientRequest{conn: 1, method: protocol.MethodThreadStart}
	p.mu.Unlock()

	raw := []byte(`{"id":100002,"error":{"code":-32600,"message":"Already initialized"}}`)
	var env protocol.Envelope
	_ = json.Unmarshal(raw, &env)

	_, out := p.patchInitializeAlreadyError(100002, raw, env)
	if out.Error == nil {
		t.Fatalf("non-initialize response should not be patched")
	}
}

func TestPatchInitializeAlreadyError_NoCachedResultKeepsError(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[100003] = pendingClientRequest{conn: 1, method: protocol.MethodInitialize}
	p.mu.Unlock()

	raw := []byte(`{"id":100003,"error":{"code":-32600,"message":"Already initialized"}}`)
	var env protocol.Envelope
	_ = json.Unmarshal(raw, &env)

	_, out := p.patchInitializeAlreadyError(100003, raw, env)
	if out.Error == nil {
		t.Fatalf("without cached result the error should stand")
	}
}
