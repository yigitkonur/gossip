package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/mcp"
)

func TestMaxBufferedMessagesFromEnv_PrimaryWinsOverAlias(t *testing.T) {
	t.Setenv("GOSSIP_MAX_BUFFERED_MESSAGES", "250")
	t.Setenv("AGENTBRIDGE_MAX_BUFFERED_MESSAGES", "50")
	if got := maxBufferedMessagesFromEnv(); got != 250 {
		t.Fatalf("maxBufferedMessagesFromEnv() = %d, want 250", got)
	}
}

func TestMaxBufferedMessagesFromEnv_LegacyAliasHonored(t *testing.T) {
	t.Setenv("GOSSIP_MAX_BUFFERED_MESSAGES", "")
	t.Setenv("AGENTBRIDGE_MAX_BUFFERED_MESSAGES", "75")
	if got := maxBufferedMessagesFromEnv(); got != 75 {
		t.Fatalf("maxBufferedMessagesFromEnv() = %d, want 75", got)
	}
}

func TestMaxBufferedMessagesFromEnv_UnsetReturnsZero(t *testing.T) {
	t.Setenv("GOSSIP_MAX_BUFFERED_MESSAGES", "")
	t.Setenv("AGENTBRIDGE_MAX_BUFFERED_MESSAGES", "")
	if got := maxBufferedMessagesFromEnv(); got != 0 {
		t.Fatalf("maxBufferedMessagesFromEnv() = %d, want 0 (server falls back to 100)", got)
	}
}

func TestMaxBufferedMessagesFromEnv_NonPositiveIgnored(t *testing.T) {
	t.Setenv("GOSSIP_MAX_BUFFERED_MESSAGES", "0")
	t.Setenv("AGENTBRIDGE_MAX_BUFFERED_MESSAGES", "-5")
	if got := maxBufferedMessagesFromEnv(); got != 0 {
		t.Fatalf("maxBufferedMessagesFromEnv() = %d, want 0 for non-positive inputs", got)
	}
}

func TestResolveClaudeDeliveryMode(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "pull"}

	t.Run("GOSSIP_MODE override wins", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "push")
		t.Setenv("AGENTBRIDGE_MODE", "pull")
		if got := resolveClaudeDeliveryMode(cfg, nil); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})

	t.Run("AGENTBRIDGE_MODE alias overrides config", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "")
		t.Setenv("AGENTBRIDGE_MODE", "push")
		if got := resolveClaudeDeliveryMode(cfg, nil); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})

	t.Run("config used when env empty", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "")
		t.Setenv("AGENTBRIDGE_MODE", "")
		if got := resolveClaudeDeliveryMode(cfg, nil); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("invalid env falls back to config", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "banana")
		t.Setenv("AGENTBRIDGE_MODE", "")
		if got := resolveClaudeDeliveryMode(cfg, nil); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("defaults to pull and logs when config unset", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "")
		t.Setenv("AGENTBRIDGE_MODE", "")
		cfg := config.DefaultConfig
		cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "auto"}

		var logs []string
		got := resolveClaudeDeliveryMode(cfg, func(msg string) { logs = append(logs, msg) })
		if got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
		if len(logs) != 1 || logs[0] != "Delivery mode defaulting to pull" {
			t.Fatalf("logs = %#v, want exactly default-pull message", logs)
		}
	})
}

func TestClaudeInstructionsIncludeSentinels(t *testing.T) {
	for _, needle := range []string{
		"## Message delivery",
		"Architect -> Builder -> Critic",
		"Current consensus:",
	} {
		if !strings.Contains(claudeInstructions, needle) {
			t.Fatalf("claudeInstructions missing %q", needle)
		}
	}
	if strings.Contains(claudeInstructions, "chat_id: line") {
		t.Fatalf("claudeInstructions should not include the extra pull-mode header sentence: %q", claudeInstructions)
	}
	if strings.Contains(claudeInstructions, "Architect→Builder→Critic") {
		t.Fatalf("claudeInstructions should use TS ASCII arrows: %q", claudeInstructions)
	}
}

func TestShouldSendReconnectNotice(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("first notification passes", func(t *testing.T) {
		var last atomic.Int64
		if !shouldSendReconnectNotice(&last, now) {
			t.Fatal("first reconnect notice should be allowed")
		}
	})

	t.Run("cooldown suppresses duplicate", func(t *testing.T) {
		var last atomic.Int64
		last.Store(now.UnixMilli())
		if shouldSendReconnectNotice(&last, now.Add(reconnectNotifyCooldown-time.Millisecond)) {
			t.Fatal("duplicate reconnect notice should be suppressed inside cooldown")
		}
	})

	t.Run("cooldown expiry allows another notice", func(t *testing.T) {
		var last atomic.Int64
		last.Store(now.UnixMilli())
		if !shouldSendReconnectNotice(&last, now.Add(reconnectNotifyCooldown)) {
			t.Fatal("reconnect notice should be allowed after cooldown expires")
		}
	})
}
