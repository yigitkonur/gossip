package main

import "testing"

func TestDisabledReplyError(t *testing.T) {
	if got := disabledReplyError(bridgeDisabledReasonRejected); got != "Gossip rejected this session — another Claude Code session is already connected. Close the other session first, or run `gossip kill` to reset." {
		t.Fatalf("rejected error = %q", got)
	}
	if got := disabledReplyError(bridgeDisabledReasonKilled); got != "Gossip is disabled by `gossip kill`. Restart Claude Code (`gossip claude`), switch to a new conversation, or run /resume to reconnect." {
		t.Fatalf("killed error = %q", got)
	}
}
