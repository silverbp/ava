// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

var taxRateNoun = resource.Noun{
	Singular: "tax-rate",
	Plural:   "tax rates",
	Aliases:  []string{"tax-rates"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.TaxRate).GetId()) }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.TaxRate).GetName() }},
		{Header: "RATE", Value: func(v proto.Message) string { return v.(*avav1.TaxRate).GetRate().GetValue() }},
		{Header: "LIABILITY_ACCOUNT", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.TaxRate).GetTaxLiabilityAccountId()) }},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.TaxRate).GetIsActive()) }},
	},
}

func newTaxRateCmd() *cobra.Command {
	root := newGroupCmd(taxRateNoun, "Manage named tax rates")
	root.AddCommand(newListCmd(taxRateNoun, listTaxRates))
	root.AddCommand(newGetCmd(taxRateNoun, getTaxRate))
	root.AddCommand(newTaxRateCreateCmd())
	root.AddCommand(newTaxRateUpdateCmd())
	root.AddCommand(newVersionedMutateCmd(taxRateNoun, "deactivate", "Deactivate a tax rate", deactivateTaxRate))
	return root
}

func getTaxRate(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tax-rate id %q: %w", id, err)
	}
	resp, err := avav1.NewTaxRateServiceClient(conn).GetTaxRate(ctx, &avav1.GetTaxRateRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetTaxRate(), nil
}

func listTaxRates(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewTaxRateServiceClient(conn).ListTaxRates(ctx, &avav1.ListTaxRatesRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetTaxRates()))
	for i, tr := range resp.GetTaxRates() {
		items[i] = tr
	}
	return items, nil
}

func deactivateTaxRate(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid tax-rate id %q: %w", id, err)
	}
	resp, err := avav1.NewTaxRateServiceClient(conn).DeactivateTaxRate(ctx, &avav1.DeactivateTaxRateRequest{Id: n, ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetTaxRate(), nil
}

func newTaxRateCreateCmd() *cobra.Command {
	var name, rate string
	var liabilityAccount int32

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewTaxRateServiceClient(conn).CreateTaxRate(cmd.Context(), &avav1.CreateTaxRateRequest{
				BusinessId:            businessID,
				Name:                  name,
				Rate:                  &avav1.Decimal{Value: rate},
				TaxLiabilityAccountId: liabilityAccount,
			})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetTaxRate(), taxRateNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "tax rate name, e.g. \"Sales Tax\" (required)")
	cmd.Flags().StringVar(&rate, "rate", "", "rate as a decimal fraction, e.g. 0.0825 for 8.25% (required)")
	cmd.Flags().Int32Var(&liabilityAccount, "liability-account", 0, "TAX_LIABILITY ledger account id this rate is collected into (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("rate")
	_ = cmd.MarkFlagRequired("liability-account")
	resource.Doc{
		Summary:  "Create a named tax rate",
		Examples: []resource.Example{{Cmd: `avactl tax-rate create --name "Sales Tax" --rate 0.0825 --liability-account 30`}},
	}.Apply(cmd)
	return cmd
}

func newTaxRateUpdateCmd() *cobra.Command {
	var resourceVersion int64
	var name, rate string

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid tax-rate id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateTaxRateRequest{Id: id, ResourceVersion: resourceVersion}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("rate") {
				req.Rate = &avav1.Decimal{Value: rate}
			}
			resp, err := avav1.NewTaxRateServiceClient(conn).UpdateTaxRate(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetTaxRate(), taxRateNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&rate, "rate", "", "new rate as a decimal fraction")
	addResourceVersionFlag(cmd, &resourceVersion)
	resource.Doc{
		Summary:  "Update a tax rate's name or rate",
		Detail:   "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{{Cmd: "avactl tax-rate update 3 --rate 0.09"}},
	}.Apply(cmd)
	return cmd
}
