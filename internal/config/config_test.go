package config

import (
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
