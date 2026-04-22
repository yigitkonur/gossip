package pluginbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesAllAssets(t *testing.T) {
	dst := t.TempDir()
	if err := Install(dst); err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := []string{
		".claude-plugin/plugin.json",
		".mcp.json",
		"commands/init.md",
		"hooks/hooks.json",
		"server/gossip-claude.sh",
	}
	for _, rel := range want {
		path := filepath.Join(dst, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
}

func TestInstallMakesShellScriptsExecutable(t *testing.T) {
	dst := t.TempDir()
	if err := Install(dst); err != nil {
		t.Fatalf("Install: %v", err)
	}
	shims := []string{
		"server/gossip-claude.sh",
	}
	for _, rel := range shims {
		info, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s not executable: mode=%v", rel, info.Mode())
		}
	}
}

// TestBundleInSync fails if plugins/gossip/ and internal/pluginbundle/assets/gossip/
// diverge. Any change to the canonical bundle must be mirrored; run `make sync-plugin`.
func TestBundleInSync(t *testing.T) {
	repoRoot := findRepoRoot(t)
	canonical := filepath.Join(repoRoot, "plugins", "gossip")

	walkHashes := func(root string) map[string]string {
		out := map[string]string{}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
		return out
	}

	canonHashes := walkHashes(canonical)
	embedHashes := map[string]string{}
	sub := FS()
	if err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, err := sub.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		embedHashes[path] = hex.EncodeToString(h.Sum(nil))
		return nil
	}); err != nil {
		t.Fatalf("walk embed: %v", err)
	}

	for rel, want := range canonHashes {
		got, ok := embedHashes[rel]
		if !ok {
			t.Errorf("embed missing %s — run `make sync-plugin`", rel)
			continue
		}
		if got != want {
			t.Errorf("embed drift at %s — run `make sync-plugin`", rel)
		}
	}
	for rel := range embedHashes {
		if _, ok := canonHashes[rel]; !ok {
			t.Errorf("embed has stale file %s — run `make sync-plugin`", rel)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root not found from %s", dir)
	return ""
}

// Sanity: the package name in the embed sub-fs root matches Root prefix.
func TestRootPrefix(t *testing.T) {
	if !strings.HasPrefix(Root, "assets/") {
		t.Fatalf("Root should live under assets/: %s", Root)
	}
}
