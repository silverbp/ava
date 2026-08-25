// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

var paymentNoun = resource.Noun{
	Singular: "payment",
	Plural:   "payments",
	Aliases:  []string{"payments", "pay"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Payment).GetId()) }},
		{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentNumber() }},
		{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentType() }},
		{Header: "AMOUNT", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetAmount().GetValue() }},
		{Header: "METHOD", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentMethod() }},
		{Header: "POSTED", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Payment).LedgerTransactionId != nil) }},
		{Header: "INVOICES", Value: func(v proto.Message) string {
			apps := v.(*avav1.Payment).GetApplications()
			ids := make([]string, len(apps))
			for i, a := range apps {
				ids[i] = fmt.Sprintf("%d", a.GetInvoiceId())
			}
			return strings.Join(ids, ",")
		}},
	},
}

func newPaymentCmd() *cobra.Command {
	root := newGroupCmd(paymentNoun, "Record and manage payments")
	root.AddCommand(newListCmd(paymentNoun, listPayments))
	root.AddCommand(newGetCmd(paymentNoun, getPayment))
	root.AddCommand(newPaymentCreateCmd())
	return root
}

func getPayment(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid payment id %q: %w", id, err)
	}
	resp, err := avav1.NewPaymentServiceClient(conn).GetPayment(ctx, &avav1.GetPaymentRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetPayment(), nil
}

func listPayments(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewPaymentServiceClient(conn).ListPayments(ctx, &avav1.ListPaymentsRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetPayments()))
	for i, p := range resp.GetPayments() {
		items[i] = p
	}
	return items, nil
}

func newPaymentCreateCmd() *cobra.Command {
	var contact int64
	var applyRaw []string
	var paymentType, number, date, amount, method string
	var ledgerAccount int32
	var reference, notes string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
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
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetPayment(), paymentNoun.Columns)
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
	resource.Doc{
		Summary: "Record a payment",
		Detail: "--apply invoice_id:amount (repeatable) applies part or all of the " +
			"payment to one or more invoices at once. Add --account (plus a contact " +
			"with --ledger-account already set) to post the payment to the ledger " +
			"atomically.",
		Examples: []resource.Example{
			{Cmd: "avactl payment create --contact 5 --type RECEIVED --number PAY-1 " +
				"--date 2026-01-15 --amount 500.00 --method CASH --apply 12:500.00 --account 10"},
			{Desc: "one payment applied across several invoices",
				Cmd: "avactl payment create --contact 5 --type RECEIVED --number PAY-2 " +
					"--date 2026-01-15 --amount 605.00 --method CASH " +
					"--apply 517:110.00 --apply 518:220.00 --apply 519:275.00"},
		},
	}.Apply(cmd)
	return cmd
}
