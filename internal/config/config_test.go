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
