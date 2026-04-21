package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/statedir"
)

func daemonOptionsFromConfig(sd *statedir.StateDir, cfg config.Config) daemon.Options {
	return daemon.Options{
		StateDir:        sd,
		AppPort:         cfg.Daemon.Port,
		ProxyPort:       cfg.Daemon.ProxyPort,
		ControlPort:     controlPort(),
		FilterMode:      filter.ModeFiltered,
		AttentionWindow: attentionWindowFromConfig(cfg),
		Logger:          logToStderr,
		IdleShutdown:    idleShutdownFromConfig(cfg),
	}
}

func attentionWindowFromConfig(cfg config.Config) time.Duration {
	if raw := os.Getenv("GOSSIP_ATTENTION_WINDOW_MS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	if cfg.TurnCoordination.AttentionWindowSeconds > 0 {
		return time.Duration(cfg.TurnCoordination.AttentionWindowSeconds) * time.Second
	}
	return 15 * time.Second
}

func idleShutdownFromConfig(cfg config.Config) time.Duration {
	if raw := os.Getenv("GOSSIP_IDLE_SHUTDOWN_MS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return time.Duration(cfg.IdleShutdownSeconds) * time.Second
}

func daemonFilterMode(logger func(string)) filter.Mode {
	if mode, ok := parseFilterMode(os.Getenv("GOSSIP_FILTER_MODE")); ok {
		if logger != nil {
			logger("Filter mode: " + string(mode))
		}
		return mode
	}
	if logger != nil {
		logger("Filter mode: " + string(filter.ModeFiltered))
	}
	return filter.ModeFiltered
}

func parseFilterMode(raw string) (filter.Mode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(filter.ModeFiltered):
		return filter.ModeFiltered, true
	case string(filter.ModeFull), "passthrough":
		return filter.ModeFull, true
	default:
		return "", false
	}
}

type stateDirEnsurer interface{ Ensure() error }

type pidFileWriter interface{ WritePid() error }

func ensureDaemonState(sd stateDirEnsurer, lc pidFileWriter) error {
	if err := sd.Ensure(); err != nil {
		return err
	}
	return lc.WritePid()
}

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "daemon",
		Short:  "Run the background daemon (invoked internally)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			cfg := config.NewService("").LoadOrDefault()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: controlPort(), Logger: logToStderr})
			if lc.WasKilled() {
				return nil
			}
			if err := ensureDaemonState(sd, lc); err != nil {
				return err
			}
			defer lc.RemovePid()

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			opts := daemonOptionsFromConfig(sd, cfg)
			opts.FilterMode = daemonFilterMode(logToStderr)
			d := daemon.New(opts)
			return d.Run(ctx)
		},
	}
}
