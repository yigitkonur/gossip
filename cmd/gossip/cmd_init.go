package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yigitkonur/gossip/internal/config"
)

const minClaudeVersion = "2.1.80"

var (
	initExecCommand = func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	initUserHomeDir      = os.UserHomeDir
	initPluginSourceDir  = findLocalPluginBundle
	initPluginCopyDir    = copyTree
	versionNumberPattern = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create .gossip/ defaults in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.OutOrStdout())
		},
	}
}

func runInit(out io.Writer) error {
	svc := config.NewService("")
	created, err := svc.InitDefaults()
	if err != nil {
		return err
	}

	fmt.Fprintln(out, paintedBanner())
	fmt.Fprintln(out, ui.bold("Project config:"))
	if len(created) == 0 {
		fmt.Fprintln(out, "  "+ui.yellow("⚠️")+" No files created — .gossip/ already populated.")
	} else {
		for _, p := range created {
			fmt.Fprintf(out, "  %s Created: %s\n", ui.green("✅"), ui.cyan(p))
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, ui.bold("Dependency checks:"))
	checkFailed := false

	claudeOutput, err := initExecCommand("claude", "--version")
	if err != nil {
		fmt.Fprintln(out, "  ❌ claude: not found in PATH")
		checkFailed = true
	} else {
		claudeVersion := extractVersionNumber(claudeOutput)
		if claudeVersion != "" && compareVersions(claudeVersion, minClaudeVersion) < 0 {
			fmt.Fprintf(out, "  ❌ claude: %s (requires >= %s)\n", claudeVersion, minClaudeVersion)
			checkFailed = true
		} else if claudeVersion != "" {
			fmt.Fprintf(out, "  ✅ claude: %s\n", claudeVersion)
		} else {
			fmt.Fprintf(out, "  ⚠️ claude: %s (version check skipped)\n", claudeOutput)
		}
	}

	codexOutput, err := initExecCommand("codex", "--version")
	if err != nil {
		fmt.Fprintln(out, "  ❌ codex: not found in PATH")
		checkFailed = true
	} else {
		fmt.Fprintf(out, "  ✅ codex: %s\n", codexOutput)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Plugin install:")
	if installErr := installPluginBundle(); installErr != nil {
		fmt.Fprintf(out, "  ⚠️ Plugin install skipped: %v\n", installErr)
	} else {
		fmt.Fprintln(out, "  ✅ Gossip plugin copied into Claude cache.")
	}
	fmt.Fprintln(out)

	if checkFailed {
		return fmt.Errorf("init checks failed")
	}

	fmt.Fprintln(out, "Setup complete!")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  1. If Claude Code is already running, execute /reload-plugins in your session")
	fmt.Fprintln(out, "  2. Start Claude Code:  gossip claude")
	fmt.Fprintln(out, "  3. Start Codex TUI:    gossip codex")
	return nil
}

func installPluginBundle() error {
	home, err := initUserHomeDir()
	if err != nil {
		return err
	}
	src, err := initPluginSourceDir()
	if err != nil {
		return err
	}
	dst := filepath.Join(home, ".claude", "plugins", "cache", "gossip")
	return initPluginCopyDir(src, dst)
}

func findLocalPluginBundle() (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "plugins", "gossip"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 5; i++ {
			candidates = append(candidates, filepath.Join(dir, "plugins", "gossip"))
			dir = filepath.Dir(dir)
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, ".claude-plugin", "plugin.json")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("local plugin bundle not found")
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func extractVersionNumber(raw string) string {
	match := versionNumberPattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func compareVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		va := parseVersionPart(pa, i)
		vb := parseVersionPart(pb, i)
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}

func parseVersionPart(parts []string, idx int) int {
	if idx >= len(parts) {
		return 0
	}
	var n int
	fmt.Sscanf(parts[idx], "%d", &n)
	return n
}
