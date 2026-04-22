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
		Long: paintedBanner() + "\n" +
			"Gossip is a local, single-binary bridge that lets Claude Code and\n" +
			"OpenAI Codex collaborate on the same workstation.\n\n" +
			"Typical flow:\n" +
			"  1. gossip init      scaffold .gossip/ and verify dependencies\n" +
			"  2. gossip codex     attach the Codex TUI via the local proxy\n" +
			"  3. Claude Code auto-invokes 'gossip claude' via the MCP plugin",
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
		newBridgeCmd(),
		newHookCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), ui.cyan(version))
		},
	}
}
