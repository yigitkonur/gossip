// Package config loads and saves the per-project .gossip/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the machine-readable project config.
type Config struct {
	Version             string                 `json:"version"`
	Daemon              DaemonConfig           `json:"daemon"`
	Agents              map[string]AgentConfig `json:"agents"`
	Markers             []string               `json:"markers"`
	TurnCoordination    TurnCoordinationConfig `json:"turnCoordination"`
	IdleShutdownSeconds int                    `json:"idleShutdownSeconds"`
}

// DaemonConfig holds per-daemon settings.
type DaemonConfig struct {
	Port      int `json:"port"`
	ProxyPort int `json:"proxyPort"`
}

// AgentConfig describes one agent.
type AgentConfig struct {
	Role string `json:"role"`
	Mode string `json:"mode,omitempty"`
}

// TurnCoordinationConfig tunes the attention-window / busy-guard behavior.
type TurnCoordinationConfig struct {
	AttentionWindowSeconds int  `json:"attentionWindowSeconds"`
	BusyGuard              bool `json:"busyGuard"`
}

// Service loads and saves config from a project root.
type Service struct {
	root              string
	configDir         string
	configPath        string
	collaborationPath string
}

// NewService binds to the given project root (or cwd if empty).
func NewService(projectRoot string) *Service {
	if projectRoot == "" {
		projectRoot, _ = os.Getwd()
	}
	dir := filepath.Join(projectRoot, ".gossip")
	return &Service{
		root:              projectRoot,
		configDir:         dir,
		configPath:        filepath.Join(dir, "config.json"),
		collaborationPath: filepath.Join(dir, "collaboration.md"),
	}
}

// ConfigPath returns the absolute config.json path.
func (s *Service) ConfigPath() string { return s.configPath }

// CollaborationPath returns the absolute collaboration.md path.
func (s *Service) CollaborationPath() string { return s.collaborationPath }

// HasConfig reports whether config.json exists.
func (s *Service) HasConfig() bool {
	_, err := os.Stat(s.configPath)
	return err == nil
}

// Load returns the parsed config or an error if it doesn't exist or is invalid.
func (s *Service) Load() (Config, error) {
	b, err := os.ReadFile(s.configPath)
	if err != nil {
		return Config{}, err
	}
	cfg := cloneDefaultConfig()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", s.configPath, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", s.configPath, err)
	}
	normalizeCompatConfig(&cfg, raw)
	return cfg, nil
}

// LoadOrDefault loads the config or returns the default if missing.
func (s *Service) LoadOrDefault() Config {
	cfg, err := s.Load()
	if err != nil {
		return cloneDefaultConfig()
	}
	return cfg
}

// Save writes the config back to disk, creating the directory if needed.
func (s *Service) Save(cfg Config) error {
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(s.configPath, b, 0o644)
}

// SaveCollaboration writes the collaboration.md rules file.
func (s *Service) SaveCollaboration(content string) error {
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.collaborationPath, []byte(content), 0o644)
}

// InitDefaults creates config.json and collaboration.md if missing.
func (s *Service) InitDefaults() ([]string, error) {
	var created []string
	if !s.HasConfig() {
		if err := s.Save(DefaultConfig); err != nil {
			return created, err
		}
		created = append(created, s.configPath)
	}
	if _, err := os.Stat(s.collaborationPath); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveCollaboration(DefaultCollaborationMD); err != nil {
			return created, err
		}
		created = append(created, s.collaborationPath)
	}
	return created, nil
}

func cloneDefaultConfig() Config {
	cfg := DefaultConfig
	cfg.Agents = make(map[string]AgentConfig, len(DefaultConfig.Agents))
	for name, agent := range DefaultConfig.Agents {
		cfg.Agents[name] = agent
	}
	cfg.Markers = append([]string(nil), DefaultConfig.Markers...)
	return cfg
}

func normalizeCompatConfig(cfg *Config, raw map[string]any) {
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}
	if codexRaw, ok := raw["codex"].(map[string]any); ok {
		if appPort, ok := normalizeCompatInt(codexRaw["appPort"]); ok {
			cfg.Daemon.Port = appPort
		}
		if proxyPort, ok := normalizeCompatInt(codexRaw["proxyPort"]); ok {
			cfg.Daemon.ProxyPort = proxyPort
		}
	}
	if claudeRaw, ok := raw["claude"].(map[string]any); ok {
		if mode, ok := claudeRaw["mode"].(string); ok && mode != "" {
			agent := cfg.Agents["claude"]
			if agent.Role == "" {
				agent.Role = DefaultConfig.Agents["claude"].Role
			}
			agent.Mode = mode
			cfg.Agents["claude"] = agent
		}
	}
}

func normalizeCompatInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case string:
		var parsed int
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
