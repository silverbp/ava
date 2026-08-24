// Command avactl is the kubectl-styled CLI for the Ava accounting API.
package main

import (
	"os"

	"github.com/silverbp/ava/internal/avactl/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
