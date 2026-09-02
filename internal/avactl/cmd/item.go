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

// itemTypeFlagHelp spells out item.item_type for --type; the server validates
// the value, this is only for --help.
const itemTypeFlagHelp = "SERVICE (labour/time), NON_INVENTORY (product, stock not tracked) or INVENTORY (product, stock tracked)"

var itemNoun = resource.Noun{
	Singular: "item",
	Plural:   "items",
	Aliases:  []string{"items"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Item).GetId()) }},
		{Header: "CODE", Value: func(v proto.Message) string { return v.(*avav1.Item).GetItemCode() }},
		{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.Item).GetItemType() }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Item).GetName() }},
		{Header: "PRICE", Value: func(v proto.Message) string { return v.(*avav1.Item).GetRetailPrice().GetValue() }},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Item).GetIsActive()) }},
		{Header: "VERSION", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Item).GetResourceVersion()) }},
	},
}

func newItemCmd() *cobra.Command {
	root := newGroupCmd(itemNoun, "Manage catalog items (services, products, tracked inventory)")

	var includeInactive bool
	listCmd := newListCmd(itemNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listItems(ctx, conn, businessID, includeInactive)
	})
	listCmd.Flags().BoolVar(&includeInactive, "inactive", false, "also include inactive items")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(itemNoun, getItem))
	root.AddCommand(newItemCreateCmd())
	root.AddCommand(newItemUpdateCmd())
	root.AddCommand(newVersionedMutateCmd(itemNoun, "deactivate", "Deactivate an item", deactivateItem))
	return root
}

func getItem(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid item id %q: %w", id, err)
	}
	resp, err := avav1.NewItemServiceClient(conn).GetItem(ctx, &avav1.GetItemRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetItem(), nil
}

func listItems(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeInactive bool) ([]proto.Message, error) {
	resp, err := avav1.NewItemServiceClient(conn).ListItems(ctx, &avav1.ListItemsRequest{BusinessId: businessID, IncludeInactive: includeInactive})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetItems()))
	for i, it := range resp.GetItems() {
		items[i] = it
	}
	return items, nil
}

func deactivateItem(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid item id %q: %w", id, err)
	}
	resp, err := avav1.NewItemServiceClient(conn).DeactivateItem(ctx, &avav1.DeactivateItemRequest{Id: n, ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetItem(), nil
}

func newItemCreateCmd() *cobra.Command {
	var code, itemType, name, description, unit, price, cost string
	var taxable bool
	var defaultTaxRateID int64
	var defaultLedgerAccountID int32

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateItemRequest{
				BusinessId:  businessID,
				ItemCode:    code,
				Name:        name,
				IsTaxable:   taxable,
				RetailPrice: &avav1.Decimal{Value: price},
			}
			if itemType != "" {
				req.ItemType = &itemType
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
			if defaultLedgerAccountID != 0 {
				req.DefaultLedgerAccountId = &defaultLedgerAccountID
			}

			resp, err := avav1.NewItemServiceClient(conn).CreateItem(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetItem(), itemNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "item code (required)")
	cmd.Flags().StringVar(&itemType, "type", "", itemTypeFlagHelp+" (default SERVICE)")
	cmd.Flags().StringVar(&name, "name", "", "item name (required)")
	cmd.Flags().StringVar(&description, "description", "", "item description")
	cmd.Flags().StringVar(&unit, "unit", "", "unit of measure, e.g. HOUR, EACH (default EACH)")
	cmd.Flags().StringVar(&price, "price", "", "retail price (required)")
	cmd.Flags().StringVar(&cost, "cost", "", "cost price")
	cmd.Flags().BoolVar(&taxable, "taxable", false, "taxable by default")
	cmd.Flags().Int64Var(&defaultTaxRateID, "default-tax-rate-id", 0, "default tax_rate id")
	cmd.Flags().Int32Var(&defaultLedgerAccountID, "default-ledger-account-id", 0, "default ledger_account id this item's lines normally post to")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("price")
	resource.Doc{
		Summary: "Create a catalog item",
		Detail:  "--type picks how the business treats the item: " + itemTypeFlagHelp + ".",
		Examples: []resource.Example{
			{Cmd: "avactl item create --code CONSULT --name Consulting --price 150.00"},
			{Cmd: "avactl item create --code WIDGET --type INVENTORY --name Widget --price 25.00 --cost 10.00"},
		},
	}.Apply(cmd)
	return cmd
}

func newItemUpdateCmd() *cobra.Command {
	var resourceVersion int64
	var itemType, name, description, price, cost string
	var taxable bool
	var defaultTaxRateID int64
	var defaultLedgerAccountID int32

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid item id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateItemRequest{Id: id, ResourceVersion: resourceVersion}
			if cmd.Flags().Changed("type") {
				req.ItemType = &itemType
			}
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
			if cmd.Flags().Changed("default-ledger-account-id") {
				req.DefaultLedgerAccountId = &defaultLedgerAccountID
			}

			resp, err := avav1.NewItemServiceClient(conn).UpdateItem(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetItem(), itemNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&itemType, "type", "", "new item type: "+itemTypeFlagHelp)
	cmd.Flags().StringVar(&name, "name", "", "new item name")
	cmd.Flags().StringVar(&description, "description", "", "new item description")
	cmd.Flags().StringVar(&price, "price", "", "new retail price")
	cmd.Flags().StringVar(&cost, "cost", "", "new cost price")
	cmd.Flags().BoolVar(&taxable, "taxable", false, "taxable by default")
	cmd.Flags().Int64Var(&defaultTaxRateID, "default-tax-rate-id", 0, "new default tax_rate id")
	cmd.Flags().Int32Var(&defaultLedgerAccountID, "default-ledger-account-id", 0, "new default ledger_account id this item's lines normally post to")
	addResourceVersionFlag(cmd, &resourceVersion)
	resource.Doc{
		Summary: "Update a catalog item",
		Detail:  "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{
			{Cmd: "avactl item update 7 --price 175.00"},
			{Cmd: "avactl item update 7 --type NON_INVENTORY"},
		},
	}.Apply(cmd)
	return cmd
}
