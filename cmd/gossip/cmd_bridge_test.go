package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/protocol"
	"github.com/yigitkonur/gossip/internal/statedir"
)

type fakeBridgeClient struct {
	connectErr error

	gotCtx   context.Context
	gotMsg   protocol.BridgeMessage
	gotReq   bool
	gotWait  int
	returns  []any // [text, received, errMsg]
	disconnects int
}

func (f *fakeBridgeClient) Connect(ctx context.Context) error {
	if f.connectErr != nil {
		return f.connectErr
	}
	return nil
}

func (f *fakeBridgeClient) SendReplyBlocking(ctx context.Context, msg protocol.BridgeMessage, requireReply bool, waitMs int) (string, bool, string) {
	f.gotCtx = ctx
	f.gotMsg = msg
	f.gotReq = requireReply
	f.gotWait = waitMs
	if len(f.returns) != 3 {
		return "", false, "test stub not configured"
	}
	return f.returns[0].(string), f.returns[1].(bool), f.returns[2].(string)
}

func (f *fakeBridgeClient) Disconnect() { f.disconnects++ }

func withBridgeHooks(t *testing.T, f *fakeBridgeClient) func() {
	t.Helper()
	sd := statedir.New(t.TempDir())
	prevClient := bridgeNewClient
	prevState := bridgeStateDir
	bridgeNewClient = func(url string) bridgeClient { return f }
	bridgeStateDir = func() *statedir.StateDir { return sd }
	return func() {
		bridgeNewClient = prevClient
		bridgeStateDir = prevState
	}
}

func TestRunBridgeSend_HappyPath(t *testing.T) {
	f := &fakeBridgeClient{returns: []any{"codex approved [COMPLETED]", true, ""}}
	defer withBridgeHooks(t, f)()

	got, code := runBridgeSend(context.Background(), bridgeSendParams{
		Text:         "Claude summary here",
		RequireReply: true,
		WaitMs:       30_000,
	})

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !got.Received || got.Text != "codex approved [COMPLETED]" || got.Error != "" {
		t.Errorf("result = %+v, want received+text", got)
	}
	if f.gotMsg.Content != "Claude summary here" {
		t.Errorf("sent content = %q", f.gotMsg.Content)
	}
	if !f.gotReq || f.gotWait != 30_000 {
		t.Errorf("require_reply=%v wait=%d", f.gotReq, f.gotWait)
	}
	if f.disconnects != 1 {
		t.Errorf("disconnect called %d times, want 1", f.disconnects)
	}
}

func TestRunBridgeSend_NoReplyYieldsExit1(t *testing.T) {
	f := &fakeBridgeClient{returns: []any{"", false, "timed out after 5000 ms"}}
	defer withBridgeHooks(t, f)()

	got, code := runBridgeSend(context.Background(), bridgeSendParams{Text: "x", RequireReply: true, WaitMs: 5_000})

	if code != 1 {
		t.Errorf("exit code = %d, want 1 on no-reply", code)
	}
	if got.Received {
		t.Errorf("Received = true, want false")
	}
	if !strings.Contains(got.Error, "timed out") {
		t.Errorf("Error passthrough missing: %q", got.Error)
	}
}

func TestRunBridgeSend_DaemonUnreachableYieldsExit2(t *testing.T) {
	f := &fakeBridgeClient{connectErr: errStubDaemonDown}
	defer withBridgeHooks(t, f)()

	got, code := runBridgeSend(context.Background(), bridgeSendParams{Text: "x", RequireReply: true, WaitMs: 1_000})

	if code != 2 {
		t.Errorf("exit code = %d, want 2 on connect failure", code)
	}
	if got.Received {
		t.Errorf("Received = true, want false")
	}
	if !strings.Contains(got.Error, "daemon unreachable") {
		t.Errorf("Error should mention daemon unreachable: %q", got.Error)
	}
	if f.disconnects != 0 {
		t.Errorf("disconnect called on failed connect (want 0): %d", f.disconnects)
	}
}

func TestBridgeSendResult_JSONShape(t *testing.T) {
	r := BridgeSendResult{Received: true, Text: "hi", Error: ""}
	b, _ := json.Marshal(r)
	want := `{"received":true,"text":"hi","error":""}`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}
}

type stubErrString string

func (e stubErrString) Error() string { return string(e) }

var errStubDaemonDown = stubErrString("connection refused")
