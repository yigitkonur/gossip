package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "dev",
		Short:  "Sync the local Gossip plugin into Claude's cache",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDev(cmd.OutOrStdout())
		},
	}
}

func runDev(out io.Writer) error {
	home, err := initUserHomeDir()
	if err != nil {
		return err
	}
	src, err := initPluginSourceDir()
	if err != nil {
		return err
	}

	// Divergence: the Go rewrite ships a direct local plugin bundle, so dev sync
	// copies that bundle straight into Claude's cache instead of registering a
	// local marketplace or managing versioned cache directories.
	dst := filepath.Join(home, ".claude", "plugins", "cache", "gossip")
	copied, err := copyTreeWithSummary(out, src, dst)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Synced %d Gossip plugin files to %s\n", len(copied), dst)
	return nil
}

func copyTreeWithSummary(out io.Writer, src, dst string) ([]string, error) {
	copied := make([]string, 0)
	err := filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
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
		if err := os.WriteFile(target, data, info.Mode()); err != nil {
			return err
		}
		fmt.Fprintf(out, "copied %s -> %s\n", rel, target)
		copied = append(copied, target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return copied, nil
}
