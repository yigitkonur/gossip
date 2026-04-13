package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/raysonmeng/agent-bridge/internal/config"
	"github.com/raysonmeng/agent-bridge/internal/daemon"
	"github.com/raysonmeng/agent-bridge/internal/filter"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "daemon",
		Short:  "Run the background daemon (invoked internally)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			_ = sd.Ensure()
			cfg := config.NewService("").LoadOrDefault()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: 4502, Logger: logToStderr})
			if lc.WasKilled() {
				return nil
			}
			_ = lc.WritePid()
			defer lc.RemovePid()

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			d := daemon.New(daemon.Options{
				StateDir:    sd,
				AppPort:     cfg.Daemon.Port,
				ProxyPort:   cfg.Daemon.ProxyPort,
				ControlPort: 4502,
				FilterMode:  filter.ModeFiltered,
				Logger:      logToStderr,
			})
			return d.Run(ctx)
		},
	}
}
