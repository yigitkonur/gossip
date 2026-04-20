package main

import (
	"testing"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/mcp"
)

func TestResolveClaudeDeliveryMode(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "pull"}

	t.Run("env override wins", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "push")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})

	t.Run("config used when env empty", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("invalid env falls back to config", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "banana")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("invalid config falls back to push", func(t *testing.T) {
		t.Setenv("GOSSIP_MODE", "")
		cfg := config.DefaultConfig
		cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "auto"}
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})
}
