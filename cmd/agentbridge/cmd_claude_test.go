package main

import (
	"testing"

	"github.com/raysonmeng/agent-bridge/internal/config"
	"github.com/raysonmeng/agent-bridge/internal/mcp"
)

func TestResolveClaudeDeliveryMode(t *testing.T) {
	cfg := config.DefaultConfig
	cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "pull"}

	t.Run("env override wins", func(t *testing.T) {
		t.Setenv("AGENTBRIDGE_MODE", "push")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})

	t.Run("config used when env empty", func(t *testing.T) {
		t.Setenv("AGENTBRIDGE_MODE", "")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("invalid env falls back to config", func(t *testing.T) {
		t.Setenv("AGENTBRIDGE_MODE", "banana")
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPull {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPull)
		}
	})

	t.Run("invalid config falls back to push", func(t *testing.T) {
		t.Setenv("AGENTBRIDGE_MODE", "")
		cfg := config.DefaultConfig
		cfg.Agents["claude"] = config.AgentConfig{Role: "Reviewer, Planner", Mode: "auto"}
		if got := resolveClaudeDeliveryMode(cfg); got != mcp.DeliveryPush {
			t.Fatalf("resolveClaudeDeliveryMode() = %q, want %q", got, mcp.DeliveryPush)
		}
	})
}
