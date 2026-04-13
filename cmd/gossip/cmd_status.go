package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yigitkonur/gossip/internal/daemon"
	"github.com/yigitkonur/gossip/internal/statedir"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print current daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			sd := statedir.New("")
			lc := daemon.NewLifecycle(daemon.LifecycleOptions{StateDir: sd, ControlPort: controlPort()})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, lc.HealthURL(), nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println("daemon: not running")
				return nil
			}
			defer resp.Body.Close()
			var status map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&status)
			out, _ := json.MarshalIndent(status, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}
