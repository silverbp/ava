// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/silverbp/ava/internal/avactl/resource"
	"github.com/silverbp/ava/internal/avactl/version"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "version",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "avactl %s\n", version.Version)
			fmt.Fprintf(w, "  git commit: %s\n", version.GitCommit)
			fmt.Fprintf(w, "  built:      %s\n", version.BuildDate)
			fmt.Fprintf(w, "  go version: %s\n", runtime.Version())
			fmt.Fprintf(w, "  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
	resource.Doc{
		Summary:  "Print version and build information",
		Detail:   "Same version `avactl --version` prints, plus the git commit, build date, Go version, and platform it was built with.",
		Examples: []resource.Example{{Cmd: "avactl version"}},
	}.Apply(cmd)
	return cmd
}
