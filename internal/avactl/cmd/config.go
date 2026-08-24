package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/silverbp/ava/internal/avactl/config"
)

func newConfigCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "config",
		Short: "Modify avactl's ~/.avactl/config file",
	}
	root.AddCommand(newConfigViewCmd())
	root.AddCommand(newConfigSetContextCmd())
	root.AddCommand(newConfigUseContextCmd())
	root.AddCommand(newConfigGetContextsCmd())
	return root
}

func newConfigViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view",
		Short: "Print the resolved ~/.avactl/config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}

func newConfigSetContextCmd() *cobra.Command {
	var server string
	var insecure bool
	var business int64

	cmd := &cobra.Command{
		Use:   "set-context <name>",
		Short: "Create or update a context (server + business)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cfg.SetContext(args[0], server, insecure, business)
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "context %q set\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gRPC server address, e.g. localhost:9090")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (a local dev server has no certificate)")
	cmd.Flags().Int64Var(&business, "business", 0, "business id to operate on")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("business")
	return cmd
}

func newConfigUseContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use-context <name>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.UseContext(args[0]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "switched to context %q\n", args[0])
			return nil
		},
	}
}

func newConfigGetContextsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-contexts",
		Short: "List available contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			for _, nc := range cfg.Contexts {
				marker := "  "
				if nc.Name == cfg.CurrentContext {
					marker = "* "
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s\t%s\t%d\n", marker, nc.Name, nc.Context.Cluster, nc.Context.Business)
			}
			return nil
		},
	}
}
