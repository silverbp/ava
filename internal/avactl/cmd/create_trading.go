// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

// parseLineFlags parses repeatable --line "key=value,key=value" flags,
// shared by create estimate/invoice, into ordered field maps (line_number
// is 1-based position in the flag list).
func parseLineFlags(raw []string) ([]map[string]string, error) {
	lines := make([]map[string]string, 0, len(raw))
	for _, r := range raw {
		fields := map[string]string{}
		for _, part := range strings.Split(r, ",") {
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 1 {
				fields[kv[0]] = "true" // bare flag, e.g. "taxable"
				continue
			}
			fields[kv[0]] = kv[1]
		}
		if _, ok := fields["desc"]; !ok {
			return nil, fmt.Errorf("invalid --line %q: missing desc=", r)
		}
		lines = append(lines, fields)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("at least one --line is required")
	}
	return lines, nil
}

func parseOptionalInt64(fields map[string]string, key string) (*int64, error) {
	v, ok := fields[key]
	if !ok {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return &n, nil
}

func parseOptionalInt32(fields map[string]string, key string) (*int32, error) {
	v, ok := fields[key]
	if !ok {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	n32 := int32(n)
	return &n32, nil
}

func parseDecimalField(fields map[string]string, key string) *avav1.Decimal {
	v, ok := fields[key]
	if !ok {
		return nil
	}
	return &avav1.Decimal{Value: v}
}

func newCreateEstimateCmd() *cobra.Command {
	var customer int64
	var date, expires string
	var notes, terms string
	var rawLines []string

	cmd := &cobra.Command{
		Use:   "estimate",
		Short: "Create an estimate",
		Long: `Create an estimate. Repeat --line once per line item:

  avactl create estimate --customer 5 --date 2026-01-01 --expires 2026-02-01 \
    --line "desc=Consulting,qty=10,price=150.00,taxable,tax-rate=1"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rawFields, err := parseLineFlags(rawLines)
			if err != nil {
				return err
			}
			dateArg, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			expiresArg, err := parseDateFlag(expires)
			if err != nil {
				return err
			}

			var lineItems []*avav1.NewEstimateLineItem
			for i, f := range rawFields {
				serviceID, err := parseOptionalInt64(f, "service")
				if err != nil {
					return err
				}
				taxRateID, err := parseOptionalInt64(f, "tax-rate")
				if err != nil {
					return err
				}
				lineItems = append(lineItems, &avav1.NewEstimateLineItem{
					ServiceId:   serviceID,
					LineNumber:  int32(i + 1),
					Description: f["desc"],
					Quantity:    parseDecimalField(f, "qty"),
					UnitPrice:   parseDecimalField(f, "price"),
					IsTaxable:   f["taxable"] == "true",
					TaxRateId:   taxRateID,
				})
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateEstimateRequest{
				BusinessId:     businessID,
				CustomerId:     customer,
				EstimateDate:   dateArg,
				ExpirationDate: expiresArg,
				LineItems:      lineItems,
			}
			if notes != "" {
				req.Notes = &notes
			}
			if terms != "" {
				req.Terms = &terms
			}

			resp, err := avav1.NewEstimateServiceClient(conn).CreateEstimate(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("estimate")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEstimate(), res.Columns)
		},
	}
	cmd.Flags().Int64Var(&customer, "customer", 0, "customer contact id (required)")
	cmd.Flags().StringVar(&date, "date", "", "estimate date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&expires, "expires", "", "expiration date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&notes, "notes", "", "notes")
	cmd.Flags().StringVar(&terms, "terms", "", "terms")
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,qty=...,price=...[,taxable][,tax-rate=<id>][,service=<id>] (repeatable)")
	_ = cmd.MarkFlagRequired("customer")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("expires")
	_ = cmd.MarkFlagRequired("line")
	return cmd
}

func newCreateInvoiceCmd() *cobra.Command {
	var contact int64
	var invoiceType, invoiceNumber, date, due, notes, terms string
	var estimate int64
	var rawLines []string

	cmd := &cobra.Command{
		Use:   "invoice",
		Short: "Create an invoice",
		Long: `Create an invoice. Repeat --line once per line item; add account=<id>
to every line (plus a contact with --ledger-account already set) to post the
invoice to the ledger atomically:

  avactl create invoice --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 \
    --line "desc=Consulting,qty=10,price=150.00,account=40,taxable,tax-rate=1"`,
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
			res, _ := resource.Lookup("invoice")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetInvoice(), res.Columns)
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
	return cmd
}

func newCreatePaymentCmd() *cobra.Command {
	var contact int64
	var applyRaw []string
	var paymentType, number, date, amount, method string
	var ledgerAccount int32
	var reference, notes string

	cmd := &cobra.Command{
		Use:   "payment",
		Short: "Record a payment",
		Long: `Record a payment. --apply invoice_id:amount (repeatable) applies part or
all of the payment to one or more invoices' paid_amount/balance_due - a
single payment can cover several invoices at once. Add --account (plus a
contact with --ledger-account already set) to post the payment to the
ledger atomically:

  avactl create payment --contact 5 --type RECEIVED --number PAY-1 \
    --date 2026-01-15 --amount 500.00 --method CASH --apply 12:500.00 --account 10

  avactl create payment --contact 5 --type RECEIVED --number PAY-2 \
    --date 2026-01-15 --amount 605.00 --method CASH \
    --apply 517:110.00 --apply 518:220.00 --apply 519:275.00`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dateArg, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			applications, err := parsePaymentApplyFlags(applyRaw)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreatePaymentRequest{
				BusinessId:    businessID,
				ContactId:     contact,
				PaymentType:   paymentType,
				PaymentNumber: number,
				PaymentDate:   dateArg,
				Amount:        &avav1.Decimal{Value: amount},
				PaymentMethod: method,
				Applications:  applications,
			}
			if ledgerAccount != 0 {
				req.LedgerAccountId = &ledgerAccount
			}
			if reference != "" {
				req.ReferenceNumber = &reference
			}
			if notes != "" {
				req.Notes = &notes
			}

			resp, err := avav1.NewPaymentServiceClient(conn).CreatePayment(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("payment")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetPayment(), res.Columns)
		},
	}
	cmd.Flags().Int64Var(&contact, "contact", 0, "customer or vendor contact id (required)")
	cmd.Flags().StringArrayVar(&applyRaw, "apply", nil, "invoice_id:amount to apply this payment against (repeatable)")
	cmd.Flags().StringVar(&paymentType, "type", "RECEIVED", "RECEIVED or MADE")
	cmd.Flags().StringVar(&number, "number", "", "payment number (required)")
	cmd.Flags().StringVar(&date, "date", "", "payment date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&amount, "amount", "", "payment amount (required)")
	cmd.Flags().StringVar(&method, "method", "", "CASH, CHECK, CREDIT_CARD, ACH, WIRE, or OTHER (required)")
	cmd.Flags().Int32Var(&ledgerAccount, "account", 0, "cash/bank ledger account id, to post this payment")
	cmd.Flags().StringVar(&reference, "reference", "", "reference number")
	cmd.Flags().StringVar(&notes, "notes", "", "notes")
	_ = cmd.MarkFlagRequired("contact")
	_ = cmd.MarkFlagRequired("number")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("method")
	return cmd
}

// parsePaymentApplyFlags parses repeatable --apply "invoice_id:amount"
// flags into PaymentApplicationInput values.
func parsePaymentApplyFlags(raw []string) ([]*avav1.PaymentApplicationInput, error) {
	applications := make([]*avav1.PaymentApplicationInput, 0, len(raw))
	for _, r := range raw {
		invoiceID, amount, ok := strings.Cut(r, ":")
		if !ok {
			return nil, fmt.Errorf("invalid --apply %q, want invoice_id:amount", r)
		}
		id, err := strconv.ParseInt(invoiceID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --apply %q: invoice_id: %w", r, err)
		}
		applications = append(applications, &avav1.PaymentApplicationInput{
			InvoiceId:     id,
			AppliedAmount: &avav1.Decimal{Value: amount},
		})
	}
	return applications, nil
}
