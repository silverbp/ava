// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <resource> [id]",
		Short: "Display one or many resources",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, ok := resource.Lookup(args[0])
			if !ok {
				return fmt.Errorf("unknown resource type %q", args[0])
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			ctx := cmd.Context()

			if len(args) == 2 {
				if flagOutput == output.FormatPDF {
					if res.GetPdf == nil {
						return fmt.Errorf("resource %q does not support -o pdf", args[0])
					}
					content, err := res.GetPdf(ctx, conn, args[1])
					if err != nil {
						return err
					}
					_, err = cmd.OutOrStdout().Write(content)
					return err
				}
				if res.Get == nil {
					return fmt.Errorf("resource %q does not support get-by-id", args[0])
				}
				obj, err := res.Get(ctx, conn, args[1])
				if err != nil {
					return err
				}
				return output.PrintOne(cmd.OutOrStdout(), flagOutput, obj, res.Columns)
			}

			if res.List == nil {
				return fmt.Errorf("resource %q does not support listing", args[0])
			}
			items, err := res.List(ctx, conn, businessID)
			if err != nil {
				return err
			}
			return output.PrintList(cmd.OutOrStdout(), flagOutput, items, res.Columns)
		},
	}
}
