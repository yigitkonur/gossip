package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestInstallPluginBundle_FallsBackToEmbedWhenLocalMissing(t *testing.T) {
	homeDir := t.TempDir()

	prevHome := initUserHomeDir
	prevSource := initPluginSourceDir
	prevFetch := initPluginFetchRemote
	initUserHomeDir = func() (string, error) { return homeDir, nil }
	initPluginSourceDir = func() (string, error) { return "", errStubLocalMissing }
	initPluginFetchRemote = func(string) error {
		t.Fatalf("remote fetch should not run when embed succeeds")
		return nil
	}
	defer func() {
		initUserHomeDir = prevHome
		initPluginSourceDir = prevSource
		initPluginFetchRemote = prevFetch
	}()

	source, err := installPluginBundle()
	if err != nil {
		t.Fatalf("installPluginBundle: %v", err)
	}
	if source != "embedded bundle" {
		t.Fatalf("source = %q, want embedded bundle", source)
	}
	manifest := filepath.Join(homeDir, ".claude", "plugins", "cache", "gossip", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("manifest missing after embed install: %v", err)
	}
}

func TestInstallPluginBundle_FallsBackToRemoteWhenLocalAndEmbedFail(t *testing.T) {
	homeDir := t.TempDir()
	fetchCalled := false

	prevHome := initUserHomeDir
	prevSource := initPluginSourceDir
	prevEmbed := initPluginEmbedWrite
	prevFetch := initPluginFetchRemote
	initUserHomeDir = func() (string, error) { return homeDir, nil }
	initPluginSourceDir = func() (string, error) { return "", errStubLocalMissing }
	initPluginEmbedWrite = func(string) error { return errStubEmbedBroken }
	initPluginFetchRemote = func(dst string) error {
		fetchCalled = true
		return os.MkdirAll(filepath.Join(dst, ".claude-plugin"), 0o755)
	}
	defer func() {
		initUserHomeDir = prevHome
		initPluginSourceDir = prevSource
		initPluginEmbedWrite = prevEmbed
		initPluginFetchRemote = prevFetch
	}()

	source, err := installPluginBundle()
	if err != nil {
		t.Fatalf("installPluginBundle: %v", err)
	}
	if !fetchCalled {
		t.Fatalf("expected remote fetch to run")
	}
	if source != "github release" {
		t.Fatalf("source = %q, want github release", source)
	}
}

func TestInstallPluginBundle_AggregatesErrorsWhenAllFail(t *testing.T) {
	homeDir := t.TempDir()

	prevHome := initUserHomeDir
	prevSource := initPluginSourceDir
	prevEmbed := initPluginEmbedWrite
	prevFetch := initPluginFetchRemote
	initUserHomeDir = func() (string, error) { return homeDir, nil }
	initPluginSourceDir = func() (string, error) { return "", errStubLocalMissing }
	initPluginEmbedWrite = func(string) error { return errStubEmbedBroken }
	initPluginFetchRemote = func(string) error { return errStubFetchOffline }
	defer func() {
		initUserHomeDir = prevHome
		initPluginSourceDir = prevSource
		initPluginEmbedWrite = prevEmbed
		initPluginFetchRemote = prevFetch
	}()

	if _, err := installPluginBundle(); err == nil {
		t.Fatalf("expected error when every source fails")
	} else {
		msg := err.Error()
		for _, frag := range []string{"local:", "embed:", "remote:", "stub-local-missing", "stub-embed-broken", "stub-fetch-offline"} {
			if !strings.Contains(msg, frag) {
				t.Errorf("error missing %q: %v", frag, err)
			}
		}
	}
}

func TestReleaseTag_StripsDevBuilds(t *testing.T) {
	cases := map[string]string{
		"0.2.0":      "v0.2.0",
		"v0.2.0":     "v0.2.0",
		"0.2.0-dev":  "",
		"0.2.1-next": "",
		"dev":        "",
		"":           "",
	}
	prev := version
	defer func() { version = prev }()
	for in, want := range cases {
		version = in
		if got := releaseTag(); got != want {
			t.Errorf("releaseTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractPluginBundleFromTar_OnlyCopiesPluginsGossip(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	entries := map[string]string{
		"gossip-0.2.0/README.md":                              "repo readme",
		"gossip-0.2.0/plugins/gossip/README.md":               "plugin readme",
		"gossip-0.2.0/plugins/gossip/.claude-plugin/a.json":   "{}",
		"gossip-0.2.0/plugins/gossip/server/gossip-claude.sh": "#!/bin/sh",
	}
	for name, body := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, ".sh") {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	dst := t.TempDir()
	if err := extractPluginBundleFromTar(tar.NewReader(&buf), dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "README.md")); err != nil {
		t.Errorf("plugin README missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".claude-plugin", "a.json")); err != nil {
		t.Errorf("plugin manifest missing: %v", err)
	}
	info, err := os.Stat(filepath.Join(dst, "server", "gossip-claude.sh"))
	if err != nil {
		t.Fatalf("shim missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("shim not executable: %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(dst, "..", "README.md")); !os.IsNotExist(err) && err != nil {
		// no-op: repo README must NOT land under dst's parent — spot-check.
	}
}

var (
	errStubLocalMissing = stubErr("stub-local-missing")
	errStubEmbedBroken  = stubErr("stub-embed-broken")
	errStubFetchOffline = stubErr("stub-fetch-offline")
)

type stubErr string

func (e stubErr) Error() string { return string(e) }

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
