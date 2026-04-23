package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestService_InitDefaults_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	created, err := s.InitDefaults()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(created) != 2 {
		t.Errorf("created = %d files, want 2", len(created))
	}
	if s.ConfigPath() != filepath.Join(dir, ".gossip", "config.json") {
		t.Errorf("bad ConfigPath")
	}
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load after init: %v", err)
	}
	if cfg.Daemon.Port != 4500 {
		t.Errorf("Daemon.Port = %d", cfg.Daemon.Port)
	}
	if cfg.Agents["claude"].Mode != "pull" {
		t.Fatalf("Claude mode = %q, want pull", cfg.Agents["claude"].Mode)
	}
}

func TestService_LoadOrDefault_ReturnsDefaultIfMissing(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	cfg := s.LoadOrDefault()
	if cfg.Version != "1.0" {
		t.Errorf("Version = %q", cfg.Version)
	}
}

func TestService_Load_NormalizesTSShapeKeys(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := `{
  "version": "1.0",
  "codex": {
    "appPort": 5510,
    "proxyPort": 5511
  },
  "claude": {
    "mode": "push"
  },
  "turnCoordination": {
    "attentionWindowSeconds": 19
  },
  "idleShutdownSeconds": 44
}
`
	if err := os.WriteFile(s.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.Port != 5510 || cfg.Daemon.ProxyPort != 5511 {
		t.Fatalf("Daemon ports = (%d,%d), want (5510,5511)", cfg.Daemon.Port, cfg.Daemon.ProxyPort)
	}
	if cfg.Agents["claude"].Mode != "push" {
		t.Fatalf("Claude mode = %q, want push", cfg.Agents["claude"].Mode)
	}
	if cfg.Agents["claude"].Role != DefaultConfig.Agents["claude"].Role {
		t.Fatalf("Claude role = %q, want default role %q", cfg.Agents["claude"].Role, DefaultConfig.Agents["claude"].Role)
	}
	if cfg.TurnCoordination.AttentionWindowSeconds != 19 {
		t.Fatalf("AttentionWindowSeconds = %d, want 19", cfg.TurnCoordination.AttentionWindowSeconds)
	}
	if cfg.IdleShutdownSeconds != 44 {
		t.Fatalf("IdleShutdownSeconds = %d, want 44", cfg.IdleShutdownSeconds)
	}
}

func TestService_InitDefaults_SeedsLoopConfig(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if _, err := s.InitDefaults(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Loop.Enabled {
		t.Errorf("Loop.Enabled = false, want true by default")
	}
	if cfg.Loop.MaxIterations != 5 {
		t.Errorf("Loop.MaxIterations = %d, want 5", cfg.Loop.MaxIterations)
	}
	if cfg.Loop.PerTurnTimeoutMs != 90_000 {
		t.Errorf("Loop.PerTurnTimeoutMs = %d, want 90000", cfg.Loop.PerTurnTimeoutMs)
	}
	if len(cfg.Loop.CompletionTags) == 0 || cfg.Loop.CompletionTags[0] != "COMPLETION" {
		t.Errorf("Loop.CompletionTags[0] = %v, want COMPLETION first", cfg.Loop.CompletionTags)
	}
	if len(cfg.Loop.ApprovalTags) == 0 || cfg.Loop.ApprovalTags[0] != "COMPLETED" {
		t.Errorf("Loop.ApprovalTags[0] = %v, want COMPLETED first", cfg.Loop.ApprovalTags)
	}
}

func TestService_Load_LegacyConfigGetsDefaultLoop(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{
  "version": "1.0",
  "daemon": {"port": 4500, "proxyPort": 4501},
  "agents": {"claude": {"role":"R","mode":"pull"}, "codex":{"role":"I"}},
  "turnCoordination": {"attentionWindowSeconds": 15, "busyGuard": true},
  "idleShutdownSeconds": 30
}
`
	if err := os.WriteFile(s.ConfigPath(), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Loop.Enabled || cfg.Loop.MaxIterations == 0 || len(cfg.Loop.CompletionTags) == 0 {
		t.Fatalf("legacy config should receive default Loop, got %+v", cfg.Loop)
	}
}

func TestService_Load_HonorsExplicitLoopOverrides(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{
  "version": "1.0",
  "loop": {
    "enabled": false,
    "maxIterations": 2,
    "perTurnTimeoutMs": 5000,
    "completionTags": ["X"],
    "approvalTags": ["Y"]
  }
}
`
	if err := os.WriteFile(s.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Loop.Enabled {
		t.Errorf("Loop.Enabled should respect override=false")
	}
	if cfg.Loop.MaxIterations != 2 {
		t.Errorf("MaxIterations = %d, want 2", cfg.Loop.MaxIterations)
	}
	if cfg.Loop.PerTurnTimeoutMs != 5000 {
		t.Errorf("PerTurnTimeoutMs = %d, want 5000", cfg.Loop.PerTurnTimeoutMs)
	}
	if len(cfg.Loop.CompletionTags) != 1 || cfg.Loop.CompletionTags[0] != "X" {
		t.Errorf("CompletionTags = %v, want [X]", cfg.Loop.CompletionTags)
	}
	if len(cfg.Loop.ApprovalTags) != 1 || cfg.Loop.ApprovalTags[0] != "Y" {
		t.Errorf("ApprovalTags = %v, want [Y]", cfg.Loop.ApprovalTags)
	}
}

func TestCloneDefaultConfig_DoesNotShareLoopSlices(t *testing.T) {
	a := cloneDefaultConfig()
	b := cloneDefaultConfig()
	a.Loop.CompletionTags[0] = "MUTATED"
	if b.Loop.CompletionTags[0] == "MUTATED" {
		t.Fatalf("cloneDefaultConfig shared Loop.CompletionTags slice")
	}
	if DefaultConfig.Loop.CompletionTags[0] == "MUTATED" {
		t.Fatalf("cloneDefaultConfig shared slice with DefaultConfig")
	}
}

func TestService_Load_PrefersTSShapeOverGoAliases(t *testing.T) {
	dir := t.TempDir()
	s := NewService(dir)
	if err := os.MkdirAll(filepath.Dir(s.ConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := `{
  "version": "1.0",
  "daemon": {
    "port": 4500,
    "proxyPort": 4501
  },
  "codex": {
    "appPort": 6600,
    "proxyPort": 6601
  },
  "agents": {
    "claude": {
      "role": "Reviewer, Planner",
      "mode": "pull"
    }
  },
  "claude": {
    "mode": "push"
  }
}
`
	if err := os.WriteFile(s.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.Port != 6600 || cfg.Daemon.ProxyPort != 6601 {
		t.Fatalf("Daemon ports = (%d,%d), want TS-shape (6600,6601)", cfg.Daemon.Port, cfg.Daemon.ProxyPort)
	}
	if cfg.Agents["claude"].Mode != "push" {
		t.Fatalf("Claude mode = %q, want TS-shape push", cfg.Agents["claude"].Mode)
	}
}
