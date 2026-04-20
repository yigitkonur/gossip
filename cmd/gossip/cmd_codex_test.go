package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewCodexCmd_DisablesFlagParsing(t *testing.T) {
	cmd := newCodexCmd()
	if !cmd.DisableFlagParsing {
		t.Fatal("codex command should disable Cobra flag parsing so native Codex flags pass through")
	}
}

func TestWithTerminalStateGuard_RestoresOnRunExit(t *testing.T) {
	prevCapture := captureTerminalState
	prevRestore := restoreTerminalState
	prevSignal := codexSignalNotifyContext
	defer func() {
		captureTerminalState = prevCapture
		restoreTerminalState = prevRestore
		codexSignalNotifyContext = prevSignal
	}()

	restored := 0
	captureTerminalState = func() (string, error) { return "saved", nil }
	restoreTerminalState = func(state string) error {
		restored++
		if state != "saved" {
			t.Fatalf("restore state = %q", state)
		}
		return nil
	}
	codexSignalNotifyContext = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(parent)
	}

	if err := withTerminalStateGuard(func() error { return nil }); err != nil {
		t.Fatalf("withTerminalStateGuard() error = %v", err)
	}
	if restored != 1 {
		t.Fatalf("restore count = %d, want 1", restored)
	}
}

func TestWithTerminalStateGuard_RestoresOnSignal(t *testing.T) {
	prevCapture := captureTerminalState
	prevRestore := restoreTerminalState
	prevSignal := codexSignalNotifyContext
	defer func() {
		captureTerminalState = prevCapture
		restoreTerminalState = prevRestore
		codexSignalNotifyContext = prevSignal
	}()

	signalCtx, signalCancel := context.WithCancel(context.Background())
	restored := make(chan string, 1)
	captureTerminalState = func() (string, error) { return "saved", nil }
	restoreTerminalState = func(state string) error {
		restored <- state
		return nil
	}
	codexSignalNotifyContext = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		return signalCtx, func() {}
	}

	done := make(chan error, 1)
	go func() {
		done <- withTerminalStateGuard(func() error {
			signalCancel()
			select {
			case <-restored:
				return nil
			case <-time.After(time.Second):
				return context.DeadlineExceeded
			}
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("withTerminalStateGuard() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal guard")
	}
}

func TestProxyHealthURL(t *testing.T) {
	got, err := proxyHealthURL("ws://127.0.0.1:4501/socket")
	if err != nil {
		t.Fatalf("proxyHealthURL() error = %v", err)
	}
	if got != "http://127.0.0.1:4501/healthz" {
		t.Fatalf("proxyHealthURL() = %q", got)
	}
}

func TestWaitForProxyReady_UsesHealthzHTTPProbe(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %q, want /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	proxyURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/socket"
	if err := waitForProxyReady(context.Background(), proxyURL); err != nil {
		t.Fatalf("waitForProxyReady() error = %v", err)
	}
	if requests == 0 {
		t.Fatal("expected at least one HTTP readiness probe")
	}
}

func TestNormalizeCodexArgs_StripsOwnedFlags(t *testing.T) {
	got, err := normalizeCodexArgs([]string{"--model", "gpt-5", "--sandbox", "workspace-write"})
	if err != nil {
		t.Fatalf("normalizeCodexArgs returned error: %v", err)
	}
	if got.ShowHelp {
		t.Fatal("ShowHelp should be false")
	}
	want := []string{"--model", "gpt-5", "--sandbox", "workspace-write"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("got %#v want %#v", got.Args, want)
	}
}

func TestNormalizeCodexArgs_StripsLeadingSeparator(t *testing.T) {
	got, err := normalizeCodexArgs([]string{"--", "--model", "gpt-5"})
	if err != nil {
		t.Fatalf("normalizeCodexArgs returned error: %v", err)
	}
	want := []string{"--model", "gpt-5"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("got %#v want %#v", got.Args, want)
	}
}

func TestNormalizeCodexArgs_RejectsOwnedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "remote split", args: []string{"--remote", "ws://127.0.0.1:7777"}, want: `"--remote" is automatically set by gossip codex`},
		{name: "remote equals", args: []string{"--remote=ws://127.0.0.1:7777"}, want: `"--remote" is automatically set by gossip codex`},
		{name: "owned enable split", args: []string{"--enable", "tui_app_server"}, want: `"--enable tui_app_server" is automatically set by gossip codex`},
		{name: "owned enable equals", args: []string{"--enable=tui_app_server"}, want: `"--enable=tui_app_server" is automatically set by gossip codex`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeCodexArgs(tt.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestNormalizeCodexArgs_PreservesOtherEnableFlags(t *testing.T) {
	got, err := normalizeCodexArgs([]string{"--enable", "other_feature", "--model", "o3"})
	if err != nil {
		t.Fatalf("normalizeCodexArgs returned error: %v", err)
	}
	want := []string{"--enable", "other_feature", "--model", "o3"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("got %#v want %#v", got.Args, want)
	}
}

func TestNormalizeCodexArgs_ReportsHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		got, err := normalizeCodexArgs(args)
		if err != nil {
			t.Fatalf("normalizeCodexArgs returned error: %v", err)
		}
		if !got.ShowHelp {
			t.Fatalf("ShowHelp should be true for %v", args)
		}
		if len(got.Args) != 0 {
			t.Fatalf("Args should be empty for help, got %#v", got.Args)
		}
	}
}

func TestNormalizeCodexArgs_RejectsOwnedFlagsAfterLeadingSeparator(t *testing.T) {
	_, err := normalizeCodexArgs([]string{"--", "--remote", "ws://127.0.0.1:7777"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"--remote" is automatically set by gossip codex`) {
		t.Fatalf("error %q does not contain owned-flag rejection", err.Error())
	}
}
