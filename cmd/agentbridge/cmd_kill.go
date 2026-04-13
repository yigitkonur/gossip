package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

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
			if b, err := os.ReadFile(sd.TuiPidFile()); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && daemon.IsProcessAlive(pid) {
					_ = syscall.Kill(pid, syscall.SIGTERM)
				}
				_ = os.Remove(sd.TuiPidFile())
			}
			killed, err := lc.Kill(3 * time.Second)
			if err != nil {
				return err
			}
			if killed {
				fmt.Println("daemon stopped and killed sentinel written.")
			} else {
				fmt.Println("daemon not running; sentinel written.")
			}
			return nil
		},
	}
}
