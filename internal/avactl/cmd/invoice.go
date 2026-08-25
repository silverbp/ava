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
	root.AddCommand(newListCmd(invoiceNoun, listInvoices))
	root.AddCommand(newGetCmd(invoiceNoun, getInvoice))
	root.AddCommand(newInvoiceCreateCmd())
	root.AddCommand(newMutateCmd(invoiceNoun, "send", "Mark an invoice SENT", sendInvoice))
	root.AddCommand(newMutateCmd(invoiceNoun, "cancel", "Cancel an invoice", cancelInvoice))
	root.AddCommand(newMutateCmd(invoiceNoun, "mark-overdue", "Mark an invoice OVERDUE", markInvoiceOverdue))
	root.AddCommand(newPdfCmd(invoiceNoun, getInvoicePdf))
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

func listInvoices(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewInvoiceServiceClient(conn).ListInvoices(ctx, &avav1.ListInvoicesRequest{BusinessId: businessID})
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

func setInvoiceStatus(ctx context.Context, conn *grpc.ClientConn, id, status string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
	}
	resp, err := avav1.NewInvoiceServiceClient(conn).UpdateInvoiceStatus(ctx, &avav1.UpdateInvoiceStatusRequest{Id: n, Status: status})
	if err != nil {
		return nil, err
	}
	return resp.GetInvoice(), nil
}

func sendInvoice(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "SENT")
}

func cancelInvoice(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "CANCELLED")
}

func markInvoiceOverdue(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	return setInvoiceStatus(ctx, conn, id, "OVERDUE")
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
					IsTaxable:       f["taxable"] == "true",
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
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,qty=...,price=...[,account=<id>][,taxable][,tax-rate=<id>][,service=<id>] (repeatable)")
	_ = cmd.MarkFlagRequired("contact")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("due")
	_ = cmd.MarkFlagRequired("line")
	resource.Doc{
		Summary: "Create an invoice",
		Detail: "Add account=<id> to every line (plus a contact with --ledger-account " +
			"already set) to post the invoice to the ledger atomically.",
		Examples: []resource.Example{{Cmd: "avactl invoice create --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 " +
			`--line "desc=Consulting,qty=10,price=150.00,account=40,taxable,tax-rate=1"`}},
	}.Apply(cmd)
	return cmd
}
