// Package main is the AgentBridge CLI entry point.
package main

import (
	"fmt"
	"os"
)

var version = "0.2.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	fmt.Fprintln(os.Stderr, "agentbridge: not yet implemented")
	os.Exit(1)
}
