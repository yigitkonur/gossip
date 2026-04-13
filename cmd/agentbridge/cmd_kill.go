package main

import (
	"fmt"
	"syscall"

	"github.com/raysonmeng/agent-bridge/internal/daemon"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
	"github.com/spf13/cobra"
)

func newKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill",
		Short: "Stop the AgentBridge daemon and write the killed sentinel",
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: 4502})
			if err := lc.WriteKilled(); err != nil {
				return err
			}
			pid := lc.ReadPid()
			if pid > 0 && daemon.IsProcessAlive(pid) {
				_ = syscall.Kill(pid, syscall.SIGTERM)
				fmt.Printf("sent SIGTERM to pid %d and wrote killed sentinel\n", pid)
				return nil
			}
			fmt.Println("daemon not running; sentinel written.")
			return nil
		},
	}
}
