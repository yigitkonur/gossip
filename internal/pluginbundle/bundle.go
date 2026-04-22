// Package pluginbundle ships the Claude Code plugin bundle as an embedded
// filesystem so a standalone gossip binary can seed ~/.claude/plugins/cache
// without any on-disk copy of the source tree.
package pluginbundle

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:assets/gossip
var embedded embed.FS

// Root is the path prefix inside the embedded filesystem.
const Root = "assets/gossip"

// FS returns the embedded plugin bundle rooted at assets/gossip.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, Root)
	if err != nil {
		panic(fmt.Errorf("pluginbundle: sub %s: %w", Root, err))
	}
	return sub
}

// Install writes the embedded plugin bundle to dst, creating directories as
// needed and preserving executable bits for files under scripts/ and server/.
func Install(dst string) error {
	bundle := FS()
	return fs.WalkDir(bundle, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		src, err := bundle.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer src.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, src); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		if isExecutableAsset(path) {
			if err := os.Chmod(target, 0o755); err != nil {
				return err
			}
		}
		return nil
	})
}

func isExecutableAsset(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".sh"
}
