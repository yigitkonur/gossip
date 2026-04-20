package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestPatchThreadStartAlreadyError_UsesCachedResult(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	cached := json.RawMessage(`{"thread":{"id":"thr_123"}}`)
	c.threadStartResult.Store(cached)

	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[200001] = pendingClientRequest{conn: 1, method: protocol.MethodThreadStart}
	p.mu.Unlock()

	errPayload := []byte(`{"id":200001,"error":{"code":-32600,"message":"thread already started"}}`)
	var env protocol.Envelope
	if err := json.Unmarshal(errPayload, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	patched, patchedEnv := p.patchThreadStartAlreadyError(200001, errPayload, env)
	if patchedEnv.Error != nil {
		t.Fatalf("patched envelope still has error: %+v", patchedEnv.Error)
	}
	if patchedEnv.Result == nil {
		t.Fatalf("patched envelope missing result")
	}
	if !strings.Contains(string(patched), `"id":"thr_123"`) {
		t.Fatalf("patched payload missing cached thread id: %s", patched)
	}
}

func TestPatchThreadStartAlreadyError_OnlyPatchesMatchingMethod(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	c.threadStartResult.Store(json.RawMessage(`{"thread":{"id":"thr_x"}}`))

	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[200002] = pendingClientRequest{conn: 1, method: protocol.MethodInitialize}
	p.mu.Unlock()

	raw := []byte(`{"id":200002,"error":{"code":-32600,"message":"thread already started"}}`)
	var env protocol.Envelope
	_ = json.Unmarshal(raw, &env)

	_, out := p.patchThreadStartAlreadyError(200002, raw, env)
	if out.Error == nil {
		t.Fatalf("non-thread/start response should not be patched")
	}
}

func TestPatchThreadStartAlreadyError_NoCachedResultKeepsError(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[200003] = pendingClientRequest{conn: 1, method: protocol.MethodThreadStart}
	p.mu.Unlock()

	raw := []byte(`{"id":200003,"error":{"code":-32600,"message":"thread already started"}}`)
	var env protocol.Envelope
	_ = json.Unmarshal(raw, &env)

	_, out := p.patchThreadStartAlreadyError(200003, raw, env)
	if out.Error == nil {
		t.Fatalf("without cached result the error should stand")
	}
}
