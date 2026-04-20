package main

import (
	"fmt"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create .gossip/ defaults in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := config.NewService("")
			created, err := svc.InitDefaults()
			if err != nil {
				return err
			}
			if len(created) == 0 {
				fmt.Println("No files created — .gossip/ already populated.")
				return nil
			}
			for _, p := range created {
				fmt.Println("created:", p)
			}
			return nil
		},
	}
}
