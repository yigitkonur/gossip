package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/statedir"
)

type daemonKillLifecycle interface {
	WriteKilled() error
	Kill(gracefulTimeout time.Duration) (bool, error)
}

func writeKilledAndStopDaemon(lc daemonKillLifecycle, gracefulTimeout time.Duration) (bool, error) {
	if err := lc.WriteKilled(); err != nil {
		return false, err
	}
	return lc.Kill(gracefulTimeout)
}

func newKillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kill",
		Short: "Stop the Gossip daemon and write the killed sentinel",
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			_ = sd.Ensure()
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: resolvedControlPort(sd)})
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
					if daemon.IsProcessAlive(pid) && isManagedCodexProcess(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
				_ = os.Remove(sd.TuiPidFile())
			}
			killed, err := writeKilledAndStopDaemon(lc, 3*time.Second)
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
	return isManagedCodexCommand(strings.TrimSpace(string(out)))
}

func isManagedCodexCommand(cmd string) bool {
	if cmd == "" {
		return false
	}
	args := strings.Fields(cmd)
	if len(args) == 0 {
		return false
	}
	return isCodexExecutable(args[0]) && hasFlagValue(args, "--enable", "tui_app_server") && hasFlagWithValue(args, "--remote")
}

func isCodexExecutable(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return false
	}
	if i := strings.LastIndex(arg, "/"); i >= 0 {
		arg = arg[i+1:]
	}
	return arg == "codex"
}

func hasFlagValue(args []string, flag, value string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
		if strings.HasPrefix(args[i], flag+"=") && strings.TrimPrefix(args[i], flag+"=") == value {
			return true
		}
	}
	return false
}

func hasFlagWithValue(args []string, flag string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == flag && i+1 < len(args) && strings.TrimSpace(args[i+1]) != "" {
			return true
		}
		if strings.HasPrefix(args[i], flag+"=") && strings.TrimSpace(strings.TrimPrefix(args[i], flag+"=")) != "" {
			return true
		}
	}
	return false
}
