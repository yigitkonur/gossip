package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunInit_CreatesConfigAndCopiesPluginBundle(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	pluginDir := t.TempDir()

	mustWriteFile(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), "{}")
	mustWriteFile(t, filepath.Join(pluginDir, "README.md"), "plugin")

	var calls []string
	restoreInitTestHooks := overrideInitTestHooks(t,
		func(name string, args ...string) (string, error) {
			calls = append(calls, name+" "+args[0])
			switch name {
			case "claude":
				return "claude 2.1.80", nil
			case "codex":
				return "codex 0.9.0", nil
			default:
				return "", nil
			}
		},
		func() (string, error) { return homeDir, nil },
		func() (string, error) { return pluginDir, nil },
	)
	defer restoreInitTestHooks()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer os.Chdir(prevWD)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	var out bytes.Buffer
	if err := runInit(&out); err != nil {
		t.Fatalf("runInit() error = %v\noutput:\n%s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(projectDir, ".gossip", "config.json")); err != nil {
		t.Fatalf("config.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".gossip", "collaboration.md")); err != nil {
		t.Fatalf("collaboration.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("installed plugin manifest missing: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"claude --version", "codex --version"}) {
		t.Fatalf("version checks = %#v", calls)
	}
}

func TestRunInit_RejectsOldClaudeVersion(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	pluginDir := t.TempDir()
	mustWriteFile(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), "{}")

	restoreInitTestHooks := overrideInitTestHooks(t,
		func(name string, args ...string) (string, error) {
			switch name {
			case "claude":
				return "claude 2.1.79", nil
			case "codex":
				return "codex 0.9.0", nil
			default:
				return "", nil
			}
		},
		func() (string, error) { return homeDir, nil },
		func() (string, error) { return pluginDir, nil },
	)
	defer restoreInitTestHooks()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer os.Chdir(prevWD)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	var out bytes.Buffer
	if err := runInit(&out); err == nil {
		t.Fatalf("runInit() succeeded unexpectedly\noutput:\n%s", out.String())
	}
}

func overrideInitTestHooks(t *testing.T, execFn func(string, ...string) (string, error), homeFn func() (string, error), pluginFn func() (string, error)) func() {
	t.Helper()
	prevExec := initExecCommand
	prevHome := initUserHomeDir
	prevPlugin := initPluginSourceDir
	initExecCommand = execFn
	initUserHomeDir = homeFn
	initPluginSourceDir = pluginFn
	return func() {
		initExecCommand = prevExec
		initUserHomeDir = prevHome
		initPluginSourceDir = prevPlugin
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
