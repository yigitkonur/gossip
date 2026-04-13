package main

import (
	"reflect"
	"testing"
)

func TestFilterCodexArgs_StripsOwnedFlags(t *testing.T) {
	got := filterCodexArgs([]string{"--model", "gpt-5", "--remote", "ws://x", "--enable", "tui_app_server", "--sandbox", "workspace-write"})
	want := []string{"--model", "gpt-5", "--sandbox", "workspace-write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
