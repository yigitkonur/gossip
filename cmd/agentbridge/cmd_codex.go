package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/coder/websocket"
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
			if err := waitForProxyReady(cmd.Context(), proxyURL); err != nil {
				return err
			}
			fmt.Println("daemon ready. launching codex TUI with --remote", proxyURL)
			tui := exec.Command("codex", "--enable", "tui_app_server", "--remote", proxyURL)
			tui.Stdin = os.Stdin
			tui.Stdout = os.Stdout
			tui.Stderr = os.Stderr
			if err := tui.Start(); err != nil {
				return err
			}
			_ = os.WriteFile(sd.TuiPidFile(), []byte(fmt.Sprintf("%d\n", tui.Process.Pid)), 0o644)
			err := tui.Wait()
			_ = os.Remove(sd.TuiPidFile())
			return err
		},
	}
}

func waitForProxyReady(ctx context.Context, proxyURL string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		dialCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, _, err := websocket.Dial(dialCtx, proxyURL, nil)
		cancel()
		if err == nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func logToStderr(msg string) { fmt.Fprintln(os.Stderr, msg) }
