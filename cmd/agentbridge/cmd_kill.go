package main

import (
	"fmt"
	"os"
	"os/exec"
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
			_ = sd.Ensure()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: 4502})
			if err := lc.WriteKilled(); err != nil {
				return err
			}
			if b, err := os.ReadFile(sd.TuiPidFile()); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && daemon.IsProcessAlive(pid) && isManagedCodexProcess(pid) {
					_ = syscall.Kill(pid, syscall.SIGTERM)
					deadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						if !daemon.IsProcessAlive(pid) {
							break
						}
						time.Sleep(200 * time.Millisecond)
					}
					if daemon.IsProcessAlive(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
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

func isManagedCodexProcess(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	cmd := strings.TrimSpace(string(out))
	return strings.Contains(cmd, "codex") && strings.Contains(cmd, "tui_app_server")
}
