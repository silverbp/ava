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

var invoiceNoun = resource.Noun{
	Singular: "invoice",
	Plural:   "invoices",
	Aliases:  []string{"invoices", "inv"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Invoice).GetId()) }},
		{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetInvoiceNumber() }},
		{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetInvoiceType() }},
		{Header: "STATUS", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetStatus() }},
		{Header: "TOTAL", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetTotalAmount().GetValue() }},
		{Header: "BALANCE_DUE", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetBalanceDue().GetValue() }},
		{Header: "POSTED", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Invoice).LedgerTransactionId != nil) }},
	},
}

func newInvoiceCmd() *cobra.Command {
	root := newGroupCmd(invoiceNoun, "Manage invoices")

	var includeAll bool
	listCmd := newListCmd(invoiceNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listInvoices(ctx, conn, businessID, includeAll)
	})
	listCmd.Flags().BoolVar(&includeAll, "all", false, "also include paid and cancelled invoices")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(invoiceNoun, getInvoice, getInvoicePdf))
	root.AddCommand(newInvoiceCreateCmd())
	root.AddCommand(newInvoiceUpdateLinesCmd())
	root.AddCommand(newVersionedMutateCmd(invoiceNoun, "send", "Mark an invoice SENT", sendInvoice))
	root.AddCommand(newVersionedMutateCmd(invoiceNoun, "cancel", "Cancel an invoice", cancelInvoice))
	root.AddCommand(newVersionedMutateCmd(invoiceNoun, "mark-overdue", "Mark an invoice OVERDUE", markInvoiceOverdue))
	return root
}

func getInvoice(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
	}
	resp, err := avav1.NewInvoiceServiceClient(conn).GetInvoice(ctx, &avav1.GetInvoiceRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetInvoice(), nil
}

func listInvoices(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeAll bool) ([]proto.Message, error) {
	resp, err := avav1.NewInvoiceServiceClient(conn).ListInvoices(ctx, &avav1.ListInvoicesRequest{BusinessId: businessID, IncludeAll: includeAll})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetInvoices()))
	for i, inv := range resp.GetInvoices() {
		items[i] = inv
	}
	return items, nil
}

func getInvoicePdf(ctx context.Context, conn *grpc.ClientConn, id string) ([]byte, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
	}
	resp, err := avav1.NewInvoiceServiceClient(conn).GetInvoicePdf(ctx, &avav1.GetInvoicePdfRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetContent(), nil
}

func setInvoiceStatus(ctx context.Context, conn *grpc.ClientConn, id, status string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
	}
	resp, err := avav1.NewInvoiceServiceClient(conn).UpdateInvoiceStatus(ctx, &avav1.UpdateInvoiceStatusRequest{Id: n, Status: status, ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetInvoice(), nil
}

func sendInvoice(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "SENT", resourceVersion)
}

func cancelInvoice(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "CANCELLED", resourceVersion)
}

func markInvoiceOverdue(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "OVERDUE", resourceVersion)
}

func newInvoiceCreateCmd() *cobra.Command {
	var contact int64
	var invoiceType, invoiceNumber, date, due, notes, terms string
	var estimate int64
	var rawLines []string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(rawLines) == 0 && estimate == 0 {
				return fmt.Errorf("either --line or --estimate is required")
			}
			rawFields, err := parseLineFlags(rawLines)
			if err != nil {
				return err
			}
			dateArg, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			dueArg, err := parseDateFlag(due)
			if err != nil {
				return err
			}

			var lineItems []*avav1.NewInvoiceLineItem
			for i, f := range rawFields {
				serviceID, err := parseOptionalInt64(f, "service")
				if err != nil {
					return err
				}
				taxRateID, err := parseOptionalInt64(f, "tax-rate")
				if err != nil {
					return err
				}
				ledgerAccountID, err := parseOptionalInt32(f, "account")
				if err != nil {
					return err
				}
				lineItems = append(lineItems, &avav1.NewInvoiceLineItem{
					ServiceId:       serviceID,
					LedgerAccountId: ledgerAccountID,
					LineNumber:      int32(i + 1),
					Description:     f["desc"],
					Quantity:        parseDecimalField(f, "qty"),
					UnitPrice:       parseDecimalField(f, "price"),
					IsTaxable:       parseOptionalBool(f, "taxable"),
					TaxRateId:       taxRateID,
				})
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateInvoiceRequest{
				BusinessId:  businessID,
				ContactId:   contact,
				InvoiceType: invoiceType,
				InvoiceDate: dateArg,
				DueDate:     dueArg,
				LineItems:   lineItems,
			}
			if invoiceNumber != "" {
				req.InvoiceNumber = &invoiceNumber
			}
			if estimate != 0 {
				req.EstimateId = &estimate
			}
			if notes != "" {
				req.Notes = &notes
			}
			if terms != "" {
				req.Terms = &terms
			}

			resp, err := avav1.NewInvoiceServiceClient(conn).CreateInvoice(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetInvoice(), invoiceNoun.Columns)
		},
	}
	cmd.Flags().Int64Var(&contact, "contact", 0, "customer or vendor contact id (required)")
	cmd.Flags().StringVar(&invoiceType, "type", "SALES", "SALES or PURCHASE")
	cmd.Flags().StringVar(&invoiceNumber, "number", "", "invoice number (required for PURCHASE; auto-generated for SALES)")
	cmd.Flags().StringVar(&date, "date", "", "invoice date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&due, "due", "", "due date, YYYY-MM-DD (required)")
	cmd.Flags().Int64Var(&estimate, "estimate", 0, "estimate id this invoice converts from")
	cmd.Flags().StringVar(&notes, "notes", "", "notes")
	cmd.Flags().StringVar(&terms, "terms", "", "terms")
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,service=<id>[,qty=...][,price=...][,account=<id>][,taxable][,tax-rate=<id>] (repeatable) - price/account/taxable/tax-rate default from the service when omitted. Omit entirely when --estimate is set to build the lines from that estimate instead.")
	_ = cmd.MarkFlagRequired("contact")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("due")
	resource.Doc{
		Summary: "Create an invoice",
		Detail: "Every line needs a ledger_account_id one way or another - either set account=<id> " +
			"explicitly, or set service=<id> on a service that has its own default_ledger_account_id. " +
			"The contact must also have a customer (for SALES) or vendor (for PURCHASE) record with its " +
			"own ledger_account_id set — the invoice posts to the ledger atomically as part of creation. " +
			"Pass --estimate with no --line flags to build the invoice's lines from that estimate's own " +
			"lines instead (service_id/description/qty/price/taxable/tax_rate carried over as-is, " +
			"ledger_account_id resolved fresh from each line's service default).",
		Examples: []resource.Example{
			{Cmd: "avactl invoice create --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 " +
				`--line "desc=Consulting,qty=10,price=150.00,account=40,taxable,tax-rate=1"`},
			{Cmd: "avactl invoice create --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 " +
				`--line "desc=Consulting,service=71,qty=10"`},
			{Cmd: "avactl invoice create --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 --estimate 12"},
		},
	}.Apply(cmd)
	return cmd
}

func newInvoiceUpdateLinesCmd() *cobra.Command {
	var rawLines []string
	var resourceVersion int64

	cmd := &cobra.Command{
		Use:  "update-lines <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid invoice id %q: %w", args[0], err)
			}
			rawFields, err := parseLineFlags(rawLines)
			if err != nil {
				return err
			}

			var lineItems []*avav1.NewInvoiceLineItem
			for i, f := range rawFields {
				serviceID, err := parseOptionalInt64(f, "service")
				if err != nil {
					return err
				}
				taxRateID, err := parseOptionalInt64(f, "tax-rate")
				if err != nil {
					return err
				}
				ledgerAccountID, err := parseOptionalInt32(f, "account")
				if err != nil {
					return err
				}
				lineItems = append(lineItems, &avav1.NewInvoiceLineItem{
					ServiceId:       serviceID,
					LedgerAccountId: ledgerAccountID,
					LineNumber:      int32(i + 1),
					Description:     f["desc"],
					Quantity:        parseDecimalField(f, "qty"),
					UnitPrice:       parseDecimalField(f, "price"),
					IsTaxable:       parseOptionalBool(f, "taxable"),
					TaxRateId:       taxRateID,
				})
			}

			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewInvoiceServiceClient(conn).UpdateInvoiceLineItems(cmd.Context(), &avav1.UpdateInvoiceLineItemsRequest{
				Id:              id,
				LineItems:       lineItems,
				ResourceVersion: resourceVersion,
			})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetInvoice(), invoiceNoun.Columns)
		},
	}
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,service=<id>[,qty=...][,price=...][,account=<id>][,taxable][,tax-rate=<id>] (repeatable) - price/account/taxable/tax-rate default from the service when omitted")
	addResourceVersionFlag(cmd, &resourceVersion)
	_ = cmd.MarkFlagRequired("line")
	resource.Doc{
		Summary: "Replace an invoice's line items",
		Detail: "Replaces the entire line item set - repeat --line once per line item, including ones you're " +
			"keeping unchanged. If the invoice is already posted to the ledger, its linked transaction's " +
			"entries are regenerated in place from the new lines rather than rejecting the edit.",
		Examples: []resource.Example{{Cmd: "avactl invoice update-lines 42 " +
			`--line "desc=Consulting,qty=10,price=150.00,account=40,taxable,tax-rate=1"`}},
	}.Apply(cmd)
	return cmd
}
