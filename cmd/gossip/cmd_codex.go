package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/statedir"
	"github.com/spf13/cobra"
)

type codexArgsResult struct {
	Args     []string
	ShowHelp bool
}

func newCodexCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "codex",
		Short:              "Ensure the daemon is running, then launch Codex TUI connected to the proxy",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalized, err := normalizeCodexArgs(args)
			if err != nil {
				return err
			}
			if normalized.ShowHelp {
				return cmd.Help()
			}

			sd := statedir.New("")
			if err := sd.Ensure(); err != nil {
				return err
			}
			cfg := config.NewService("").LoadOrDefault()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: controlPort(), Logger: logToStderr})
			lc.ClearKilled()
			if err := lc.EnsureRunning(cmd.Context()); err != nil {
				return err
			}
			proxyURL := fmt.Sprintf("ws://127.0.0.1:%d", cfg.Daemon.ProxyPort)
			if status, ok := readDaemonStatus(sd.StatusFile()); ok && status.ProxyURL != "" {
				proxyURL = status.ProxyURL
			}
			if err := waitForProxyReady(cmd.Context(), proxyURL); err != nil {
				return err
			}
			fmt.Println("daemon ready. launching codex TUI with --remote", proxyURL)
			codexArgs := append([]string{"--enable", "tui_app_server", "--remote", proxyURL}, normalized.Args...)
			tui := exec.Command("codex", codexArgs...)
			tui.Stdin = os.Stdin
			tui.Stdout = os.Stdout
			tui.Stderr = os.Stderr
			if err := tui.Start(); err != nil {
				return err
			}
			_ = os.WriteFile(sd.TuiPidFile(), []byte(fmt.Sprintf("%d\n", tui.Process.Pid)), 0o644)
			err = tui.Wait()
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

func normalizeCodexArgs(args []string) (codexArgsResult, error) {
	explicitPassthrough := false
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
		explicitPassthrough = true
	}
	if !explicitPassthrough && len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return codexArgsResult{ShowHelp: true}, nil
	}

	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--remote":
			return codexArgsResult{}, ownedCodexFlagError(`"--remote" is automatically set by gossip codex.`)
		case strings.HasPrefix(arg, "--remote="):
			return codexArgsResult{}, ownedCodexFlagError(`"--remote" is automatically set by gossip codex.`)
		case arg == "--enable":
			if i+1 < len(args) && args[i+1] == "tui_app_server" {
				return codexArgsResult{}, ownedCodexFlagError(`"--enable tui_app_server" is automatically set by gossip codex.`)
			}
			filtered = append(filtered, arg)
		case arg == "--enable=tui_app_server":
			return codexArgsResult{}, ownedCodexFlagError(`"--enable=tui_app_server" is automatically set by gossip codex.`)
		default:
			filtered = append(filtered, arg)
		}
	}
	return codexArgsResult{Args: filtered}, nil
}

func ownedCodexFlagError(msg string) error {
	return fmt.Errorf("%s\n\nIf you need full control over these flags, use the native command directly:\n  codex [your flags here]", msg)
}
