package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.2.0-dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gossip",
		Short: "Gossip — bridge between Claude Code and Codex CLI",
	}
	root.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newDevCmd(),
		newCodexCmd(),
		newClaudeCmd(),
		newDaemonCmd(),
		newKillCmd(),
		newStatusCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}
