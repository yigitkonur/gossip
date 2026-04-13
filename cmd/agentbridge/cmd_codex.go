package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/raysonmeng/agent-bridge/internal/config"
	"github.com/raysonmeng/agent-bridge/internal/daemon"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
	"github.com/spf13/cobra"
)

func newCodexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "codex",
		Short: "Ensure the daemon is running, then launch Codex TUI connected to the proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			if err := sd.Ensure(); err != nil {
				return err
			}
			cfg := config.NewService("").LoadOrDefault()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: 4502, Logger: logToStderr})
			lc.ClearKilled()
			if err := lc.EnsureRunning(cmd.Context()); err != nil {
				return err
			}
			proxyURL := fmt.Sprintf("ws://127.0.0.1:%d", cfg.Daemon.ProxyPort)
			fmt.Println("daemon ready. launching codex TUI with --remote", proxyURL)
			tui := exec.Command("codex", "--enable", "tui_app_server", "--remote", proxyURL)
			tui.Stdin = os.Stdin
			tui.Stdout = os.Stdout
			tui.Stderr = os.Stderr
			return tui.Run()
		},
	}
}

func logToStderr(msg string) { fmt.Fprintln(os.Stderr, msg) }
