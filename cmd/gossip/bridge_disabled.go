package main

type bridgeDisabledReason string

const (
	bridgeDisabledReasonNone     bridgeDisabledReason = ""
	bridgeDisabledReasonKilled   bridgeDisabledReason = "killed"
	bridgeDisabledReasonRejected bridgeDisabledReason = "rejected"
)

func disabledReplyError(reason bridgeDisabledReason) string {
	switch reason {
	case bridgeDisabledReasonRejected:
		return "Gossip rejected this session — another Claude Code session is already connected. Close the other session first, or run `gossip kill` to reset."
	case bridgeDisabledReasonKilled:
		fallthrough
	default:
		return "Gossip is disabled by `gossip kill`. Restart Claude Code (`gossip claude`), switch to a new conversation, or run /resume to reconnect."
	}
}
