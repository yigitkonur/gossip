package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDev_CopiesPluginBundleIntoClaudeCache(t *testing.T) {
	homeDir := t.TempDir()
	pluginDir := t.TempDir()

	mustWriteFile(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), "{}")
	mustWriteFile(t, filepath.Join(pluginDir, "README.md"), "new plugin contents")
	mustWriteFile(t, filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip", "README.md"), "stale contents")

	prevHome := initUserHomeDir
	prevPlugin := initPluginSourceDir
	initUserHomeDir = func() (string, error) { return homeDir, nil }
	initPluginSourceDir = func() (string, error) { return pluginDir, nil }
	defer func() {
		initUserHomeDir = prevHome
		initPluginSourceDir = prevPlugin
	}()

	var out bytes.Buffer
	if err := runDev(&out); err != nil {
		t.Fatalf("runDev() error = %v\noutput:\n%s", err, out.String())
	}

	manifestPath := filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("plugin manifest missing: %v", err)
	}

	readmePath := filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip", "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read synced README.md: %v", err)
	}
	if string(readme) != "new plugin contents" {
		t.Fatalf("README.md contents = %q, want overwritten plugin contents", string(readme))
	}

	output := out.String()
	if !strings.Contains(output, "copied .claude-plugin/plugin.json -> "+manifestPath) {
		t.Fatalf("runDev() output missing manifest copy line:\n%s", output)
	}
	if !strings.Contains(output, "copied README.md -> "+readmePath) {
		t.Fatalf("runDev() output missing README copy line:\n%s", output)
	}
	if !strings.Contains(output, "Synced 2 Gossip plugin files to "+filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip")) {
		t.Fatalf("runDev() output missing final summary:\n%s", output)
	}
}
