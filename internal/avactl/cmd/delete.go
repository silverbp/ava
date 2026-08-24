package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <resource> <id>",
		Short: "Delete (or deactivate) a resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, ok := resource.Lookup(args[0])
			if !ok {
				return fmt.Errorf("unknown resource type %q", args[0])
			}
			if res.Delete == nil {
				return fmt.Errorf("resource %q does not support delete", args[0])
			}

			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			obj, err := res.Delete(cmd.Context(), conn, args[1])
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, obj, res.Columns)
		},
	}
}
