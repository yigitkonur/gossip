package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/pluginbundle"
)

const minClaudeVersion = "2.1.80"

// pluginBundleURLEnv lets users override the GitHub tarball URL used as the
// last-resort plugin source. Empty value means "derive from version".
const pluginBundleURLEnv = "GOSSIP_PLUGIN_BUNDLE_URL"

var (
	initExecCommand = func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	initUserHomeDir       = os.UserHomeDir
	initPluginSourceDir   = findLocalPluginBundle
	initPluginCopyDir     = copyTree
	initPluginEmbedWrite  = pluginbundle.Install
	initPluginFetchRemote = fetchPluginBundleFromGitHub
	versionNumberPattern  = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
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
	mcpPath, mcpStatus, err := ensureProjectMCPConfig("")
	if err != nil {
		return err
	}

	fmt.Fprintln(out, paintedBanner())
	fmt.Fprintln(out, ui.bold("Project config:"))
	if len(created) == 0 && mcpStatus == mcpEnsureUnchanged {
		fmt.Fprintln(out, "  "+ui.yellow("⚠️")+" No files created — project already configured.")
	} else {
		for _, p := range created {
			fmt.Fprintf(out, "  %s Created: %s\n", ui.green("✅"), ui.cyan(p))
		}
		switch mcpStatus {
		case mcpEnsureCreated:
			fmt.Fprintf(out, "  %s Created: %s\n", ui.green("✅"), ui.cyan(mcpPath))
		case mcpEnsureMerged:
			fmt.Fprintf(out, "  %s Merged gossip into: %s\n", ui.green("✅"), ui.cyan(mcpPath))
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
	source, installErr := installPluginBundle()
	if installErr != nil {
		fmt.Fprintf(out, "  ⚠️ Plugin install skipped: %v\n", installErr)
	} else {
		fmt.Fprintf(out, "  ✅ Gossip plugin copied into Claude cache (source: %s).\n", source)
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

// gossipMCPServer is the stdio MCP server definition Claude Code uses to
// launch `gossip claude`. It is merged into the project's .mcp.json on init.
var gossipMCPServer = map[string]any{
	"command": "gossip",
	"args":    []any{"claude"},
}

// mcpEnsureStatus reports what ensureProjectMCPConfig did to .mcp.json.
type mcpEnsureStatus int

const (
	mcpEnsureUnchanged mcpEnsureStatus = iota
	mcpEnsureCreated
	mcpEnsureMerged
)

// ensureProjectMCPConfig guarantees .mcp.json at projectRoot (cwd when empty)
// contains a "gossip" entry under mcpServers. It creates the file when
// missing, merges a gossip entry into an existing file that lacks one, and
// leaves a file that already has gossip untouched. It never overwrites other
// MCP servers the user may have registered.
func ensureProjectMCPConfig(projectRoot string) (string, mcpEnsureStatus, error) {
	if projectRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", mcpEnsureUnchanged, err
		}
		projectRoot = wd
	}
	path := filepath.Join(projectRoot, ".mcp.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return path, mcpEnsureUnchanged, err
		}
		body, marshalErr := marshalMCPBody(map[string]any{"gossip": gossipMCPServer})
		if marshalErr != nil {
			return path, mcpEnsureUnchanged, marshalErr
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return path, mcpEnsureUnchanged, err
		}
		return path, mcpEnsureCreated, nil
	}

	var doc map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return path, mcpEnsureUnchanged, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}
	var servers map[string]any
	if raw, exists := doc["mcpServers"]; exists {
		typed, ok := raw.(map[string]any)
		if !ok {
			// Refuse to silently replace a non-object mcpServers — that would
			// silently drop user data. Surface it instead; the user can fix
			// the file by hand.
			return path, mcpEnsureUnchanged, fmt.Errorf("%s: mcpServers is not an object (got %T); refusing to overwrite", path, raw)
		}
		servers = typed
	}
	if servers == nil {
		servers = map[string]any{}
	}
	if _, ok := servers["gossip"]; ok {
		return path, mcpEnsureUnchanged, nil
	}
	servers["gossip"] = gossipMCPServer
	doc["mcpServers"] = servers

	body, err := marshalMCPBodyRaw(doc)
	if err != nil {
		return path, mcpEnsureUnchanged, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return path, mcpEnsureUnchanged, err
	}
	return path, mcpEnsureMerged, nil
}

func marshalMCPBody(servers map[string]any) ([]byte, error) {
	return marshalMCPBodyRaw(map[string]any{"mcpServers": servers})
}

func marshalMCPBodyRaw(doc map[string]any) ([]byte, error) {
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// installPluginBundle writes the Gossip plugin bundle into the Claude Code
// cache using the first available source, in order:
//
//  1. A local checkout (dev workflow, or install.sh that dropped the bundle
//     next to the binary or under /usr/local/share/gossip).
//  2. The bundle embedded in the gossip binary at build time.
//  3. A fetch from the GitHub release / master archive.
//
// It returns a human-readable description of the source that was used so the
// CLI output can tell the user where the bundle came from.
func installPluginBundle() (string, error) {
	home, err := initUserHomeDir()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(home, ".claude", "plugins", "cache", "gossip")

	var errs []string

	// Each fallback writes into dst. If a fallback fails midway (local copy
	// failure after partial writes, embed extract aborting on disk full,
	// etc.) we wipe dst before the next attempt so the next writer starts
	// from a clean slate. Otherwise a half-written local copy plus a
	// subsequent embed write could leave a mix of files from different
	// sources.
	cleanDst := func() { _ = os.RemoveAll(dst) }

	if src, err := initPluginSourceDir(); err == nil {
		if copyErr := initPluginCopyDir(src, dst); copyErr == nil {
			return "local checkout", nil
		} else {
			errs = append(errs, fmt.Sprintf("local: %v", copyErr))
			cleanDst()
		}
	} else {
		errs = append(errs, fmt.Sprintf("local: %v", err))
	}

	if embedErr := initPluginEmbedWrite(dst); embedErr == nil {
		return "embedded bundle", nil
	} else {
		errs = append(errs, fmt.Sprintf("embed: %v", embedErr))
		cleanDst()
	}

	if fetchErr := initPluginFetchRemote(dst); fetchErr == nil {
		return "github release", nil
	} else {
		errs = append(errs, fmt.Sprintf("remote: %v", fetchErr))
		cleanDst()
	}

	return "", fmt.Errorf("no plugin source succeeded (%s)", strings.Join(errs, "; "))
}

// findLocalPluginBundle searches well-known on-disk locations for the plugin
// bundle: next to the current working directory (dev), adjacent to the gossip
// binary (dist), and under /usr/local/share/gossip (install.sh on POSIX).
func findLocalPluginBundle() (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "plugins", "gossip"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 5; i++ {
			candidates = append(candidates,
				filepath.Join(dir, "plugins", "gossip"),
				filepath.Join(dir, "share", "gossip", "plugins", "gossip"),
			)
			dir = filepath.Dir(dir)
		}
	}
	candidates = append(candidates,
		"/usr/local/share/gossip/plugins/gossip",
		"/usr/share/gossip/plugins/gossip",
	)
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, ".claude-plugin", "plugin.json")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("local plugin bundle not found")
}

// fetchPluginBundleFromGitHub downloads the plugin bundle from the GitHub
// repository archive tarball and writes it to dst. It prefers the tag that
// matches the running binary's build version; if the version is a dev build
// or the tag archive is missing, it falls back to the master branch archive.
// GOSSIP_PLUGIN_BUNDLE_URL overrides the URL entirely.
func fetchPluginBundleFromGitHub(dst string) error {
	urls := candidatePluginBundleURLs()
	var lastErr error
	for _, url := range urls {
		if err := downloadAndExtractPluginBundle(url, dst); err != nil {
			lastErr = fmt.Errorf("%s: %w", url, err)
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate URLs")
	}
	return lastErr
}

func candidatePluginBundleURLs() []string {
	if override := strings.TrimSpace(os.Getenv(pluginBundleURLEnv)); override != "" {
		return []string{override}
	}
	urls := []string{}
	if tag := releaseTag(); tag != "" {
		urls = append(urls, fmt.Sprintf("https://codeload.github.com/yigitkonur/gossip/tar.gz/refs/tags/%s", tag))
	}
	urls = append(urls, "https://codeload.github.com/yigitkonur/gossip/tar.gz/refs/heads/master")
	return urls
}

func releaseTag() string {
	v := strings.TrimSpace(version)
	if v == "" || strings.Contains(v, "dev") || strings.Contains(v, "next") {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// maxPluginTarballBytes caps the compressed download size for the GitHub
// fallback. The canonical bundle is <30 KiB; 50 MiB is four orders of
// magnitude of headroom and still keeps us safe against a hostile or
// mirror-corrupted archive under a user-supplied GOSSIP_PLUGIN_BUNDLE_URL.
const maxPluginTarballBytes = 50 * 1024 * 1024

func downloadAndExtractPluginBundle(url, dst string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxPluginTarballBytes))
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractPluginBundleFromTar(tar.NewReader(gz), dst)
}

// extractPluginBundleFromTar copies entries under <root>/plugins/gossip/ into
// dst, ignoring everything else. The root prefix is the single top-level
// directory inside the GitHub archive (e.g. "gossip-0.2.0").
func extractPluginBundleFromTar(tr *tar.Reader, dst string) error {
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.ToSlash(filepath.Clean(hdr.Name))
		parts := strings.SplitN(clean, "/", 2)
		if len(parts) != 2 {
			continue
		}
		inner := parts[1]
		const prefix = "plugins/gossip/"
		if !strings.HasPrefix(inner, prefix) {
			continue
		}
		rel := strings.TrimPrefix(inner, prefix)
		if rel == "" {
			continue
		}
		// Component-aware traversal check: reject any path segment that is
		// exactly "..". A substring match on ".." would reject legitimate
		// filenames like "foo..bar".
		for _, seg := range strings.Split(rel, "/") {
			if seg == ".." {
				return fmt.Errorf("refusing path traversal: %s", hdr.Name)
			}
		}
		target := filepath.Join(dst, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("plugins/gossip/ not found in archive")
	}
	return nil
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
