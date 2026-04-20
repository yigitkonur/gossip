package main

import (
	"strings"
	"testing"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/mcp"
)

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
		"Architect→Builder→Critic",
		"chat_id: line",
		"Current consensus:",
	} {
		if !strings.Contains(claudeInstructions, needle) {
			t.Fatalf("claudeInstructions missing %q", needle)
		}
	}
}
