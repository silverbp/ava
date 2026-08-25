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

var serviceNoun = resource.Noun{
	Singular: "service",
	Plural:   "services",
	Aliases:  []string{"services", "svc"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Service).GetId()) }},
		{Header: "CODE", Value: func(v proto.Message) string { return v.(*avav1.Service).GetServiceCode() }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Service).GetName() }},
		{Header: "PRICE", Value: func(v proto.Message) string { return v.(*avav1.Service).GetRetailPrice().GetValue() }},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Service).GetIsActive()) }},
	},
}

func newServiceCmd() *cobra.Command {
	root := newGroupCmd(serviceNoun, "Manage catalog services/products")

	var includeInactive bool
	listCmd := newListCmd(serviceNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listServices(ctx, conn, businessID, includeInactive)
	})
	listCmd.Flags().BoolVar(&includeInactive, "inactive", false, "also include inactive services")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(serviceNoun, getService))
	root.AddCommand(newServiceCreateCmd())
	root.AddCommand(newServiceUpdateCmd())
	root.AddCommand(newMutateCmd(serviceNoun, "deactivate", "Deactivate a service", deactivateService))
	return root
}

func getService(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid service id %q: %w", id, err)
	}
	resp, err := avav1.NewServiceCatalogServiceClient(conn).GetService(ctx, &avav1.GetServiceRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetService(), nil
}

func listServices(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeInactive bool) ([]proto.Message, error) {
	resp, err := avav1.NewServiceCatalogServiceClient(conn).ListServices(ctx, &avav1.ListServicesRequest{BusinessId: businessID, IncludeInactive: includeInactive})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetServices()))
	for i, svc := range resp.GetServices() {
		items[i] = svc
	}
	return items, nil
}

func deactivateService(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid service id %q: %w", id, err)
	}
	resp, err := avav1.NewServiceCatalogServiceClient(conn).DeactivateService(ctx, &avav1.DeactivateServiceRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetService(), nil
}

func newServiceCreateCmd() *cobra.Command {
	var code, name, description, unit, price, cost string
	var taxable bool
	var defaultTaxRateID int64

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateServiceRequest{
				BusinessId:  businessID,
				ServiceCode: code,
				Name:        name,
				IsTaxable:   taxable,
				RetailPrice: &avav1.Decimal{Value: price},
			}
			if description != "" {
				req.Description = &description
			}
			if unit != "" {
				req.UnitOfMeasure = &unit
			}
			if cost != "" {
				req.CostPrice = &avav1.Decimal{Value: cost}
			}
			if defaultTaxRateID != 0 {
				req.DefaultTaxRateId = &defaultTaxRateID
			}

			resp, err := avav1.NewServiceCatalogServiceClient(conn).CreateService(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetService(), serviceNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "service code (required)")
	cmd.Flags().StringVar(&name, "name", "", "service name (required)")
	cmd.Flags().StringVar(&description, "description", "", "service description")
	cmd.Flags().StringVar(&unit, "unit", "", "unit of measure, e.g. HOUR, EACH (default EACH)")
	cmd.Flags().StringVar(&price, "price", "", "retail price (required)")
	cmd.Flags().StringVar(&cost, "cost", "", "cost price")
	cmd.Flags().BoolVar(&taxable, "taxable", false, "taxable by default")
	cmd.Flags().Int64Var(&defaultTaxRateID, "default-tax-rate-id", 0, "default tax_rate id")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("price")
	resource.Doc{
		Summary:  "Create a catalog service/product",
		Examples: []resource.Example{{Cmd: "avactl service create --code CONSULT --name Consulting --price 150.00"}},
	}.Apply(cmd)
	return cmd
}

func newServiceUpdateCmd() *cobra.Command {
	var name, description, price, cost string
	var taxable bool
	var defaultTaxRateID int64

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid service id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateServiceRequest{Id: id}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				req.Description = &description
			}
			if cmd.Flags().Changed("price") {
				req.RetailPrice = &avav1.Decimal{Value: price}
			}
			if cmd.Flags().Changed("cost") {
				req.CostPrice = &avav1.Decimal{Value: cost}
			}
			if cmd.Flags().Changed("taxable") {
				req.IsTaxable = &taxable
			}
			if cmd.Flags().Changed("default-tax-rate-id") {
				req.DefaultTaxRateId = &defaultTaxRateID
			}

			resp, err := avav1.NewServiceCatalogServiceClient(conn).UpdateService(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetService(), serviceNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new service name")
	cmd.Flags().StringVar(&description, "description", "", "new service description")
	cmd.Flags().StringVar(&price, "price", "", "new retail price")
	cmd.Flags().StringVar(&cost, "cost", "", "new cost price")
	cmd.Flags().BoolVar(&taxable, "taxable", false, "taxable by default")
	cmd.Flags().Int64Var(&defaultTaxRateID, "default-tax-rate-id", 0, "new default tax_rate id")
	resource.Doc{
		Summary:  "Update a catalog service/product",
		Detail:   "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{{Cmd: "avactl service update 7 --price 175.00"}},
	}.Apply(cmd)
	return cmd
}
