package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestRewriteUserAgentString_SwapsClientInfo(t *testing.T) {
	ua := "gossip/0.121.0 (Mac OS 26.4.1; arm64) unknown (gossip; 0.2.0)"
	got := rewriteUserAgentString(ua, &protocol.ClientInfo{Name: "codex-tui", Version: "0.121.0"})
	want := "codex-tui/0.121.0 (Mac OS 26.4.1; arm64) unknown (codex-tui; 0.121.0)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRewriteUserAgentString_LeavesMalformedUnchanged(t *testing.T) {
	for _, ua := range []string{"", "noslash", "only/slash"} {
		got := rewriteUserAgentString(ua, &protocol.ClientInfo{Name: "x", Version: "1"})
		if got != ua {
			t.Fatalf("malformed ua %q rewritten to %q", ua, got)
		}
	}
}

func TestPatchInitializeAlreadyError_RewritesUserAgent(t *testing.T) {
	c := NewClient(ClientOptions{Port: 45000})
	cached := json.RawMessage(`{"userAgent":"gossip/0.121.0 (os; arch) unknown (gossip; 0.2.0)","codexHome":"/tmp","platformFamily":"unix","platformOs":"macos"}`)
	c.initializeResult.Store(cached)

	p := NewProxy(c)
	p.mu.Lock()
	p.pendingClient[300001] = pendingClientRequest{
		conn:       1,
		method:     protocol.MethodInitialize,
		clientInfo: &protocol.ClientInfo{Name: "codex-tui", Version: "0.121.0"},
	}
	p.mu.Unlock()

	errPayload := []byte(`{"id":300001,"error":{"code":-32600,"message":"Already initialized"}}`)
	var env protocol.Envelope
	_ = json.Unmarshal(errPayload, &env)

	patched, patchedEnv := p.patchInitializeAlreadyError(300001, errPayload, env)
	if patchedEnv.Error != nil {
		t.Fatalf("still has error: %+v", patchedEnv.Error)
	}
	if !strings.Contains(string(patched), `"userAgent":"codex-tui/0.121.0 (os; arch) unknown (codex-tui; 0.121.0)"`) {
		t.Fatalf("userAgent not rewritten in patched payload: %s", patched)
	}
}

func TestTrackPendingClientRequest_CapturesClientInfoForInitialize(t *testing.T) {
	p := NewProxy(nil)
	params := json.RawMessage(`{"clientInfo":{"name":"codex-tui","version":"0.121.0"},"capabilities":{}}`)
	p.trackPendingClientRequest(9999, 1, protocol.MethodInitialize, params)
	p.mu.Lock()
	entry := p.pendingClient[9999]
	p.mu.Unlock()
	if entry.clientInfo == nil {
		t.Fatal("expected clientInfo captured")
	}
	if entry.clientInfo.Name != "codex-tui" || entry.clientInfo.Version != "0.121.0" {
		t.Fatalf("got %+v", entry.clientInfo)
	}
}

func TestTrackPendingClientRequest_SkipsClientInfoForNonInitialize(t *testing.T) {
	p := NewProxy(nil)
	params := json.RawMessage(`{"clientInfo":{"name":"x","version":"1"}}`)
	p.trackPendingClientRequest(9998, 1, protocol.MethodThreadStart, params)
	p.mu.Lock()
	entry := p.pendingClient[9998]
	p.mu.Unlock()
	if entry.clientInfo != nil {
		t.Fatalf("should not capture clientInfo for %s", protocol.MethodThreadStart)
	}
}
