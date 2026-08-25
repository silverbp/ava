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

var ledgerTransactionNoun = resource.Noun{
	Singular: "ledger-transaction",
	Plural:   "ledger transactions",
	Aliases:  []string{"ledger-transactions", "lt"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerTransaction).GetId()) }},
		{Header: "DATE", Value: func(v proto.Message) string { return formatDate(v.(*avav1.LedgerTransaction).GetTransactionDate()) }},
		{Header: "DESCRIPTION", Value: func(v proto.Message) string { return v.(*avav1.LedgerTransaction).GetDescription() }},
		{Header: "ENTRIES", Value: func(v proto.Message) string { return fmt.Sprintf("%d", len(v.(*avav1.LedgerTransaction).GetEntries())) }},
	},
}

func newLedgerTransactionCmd() *cobra.Command {
	root := newGroupCmd(ledgerTransactionNoun, "Read and post the double-entry ledger")
	root.AddCommand(newListCmd(ledgerTransactionNoun, listLedgerTransactions))
	root.AddCommand(newGetCmd(ledgerTransactionNoun, getLedgerTransaction))
	root.AddCommand(newLedgerTransactionPostCmd())
	return root
}

func getLedgerTransaction(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ledger-transaction id %q: %w", id, err)
	}
	resp, err := avav1.NewLedgerTransactionServiceClient(conn).GetLedgerTransaction(ctx, &avav1.GetLedgerTransactionRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetTransaction(), nil
}

func listLedgerTransactions(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewLedgerTransactionServiceClient(conn).ListLedgerTransactions(ctx, &avav1.ListLedgerTransactionsRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetTransactions()))
	for i, t := range resp.GetTransactions() {
		items[i] = t
	}
	return items, nil
}

func newLedgerTransactionPostCmd() *cobra.Command {
	var date, description, reference string
	var rawEntries []string

	cmd := &cobra.Command{
		Use:  "post",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := parseEntryFlags(rawEntries)
			if err != nil {
				return err
			}
			txnDate, err := parseDateFlag(date)
			if err != nil {
				return err
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateLedgerTransactionRequest{
				BusinessId:      businessID,
				TransactionDate: txnDate,
				Entries:         entries,
			}
			if description != "" {
				req.Description = &description
			}
			if reference != "" {
				req.ReferenceNumber = &reference
			}

			resp, err := avav1.NewLedgerTransactionServiceClient(conn).CreateLedgerTransaction(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetTransaction(), ledgerTransactionNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "transaction date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&description, "description", "", "transaction description")
	cmd.Flags().StringVar(&reference, "reference", "", "reference number")
	cmd.Flags().StringArrayVar(&rawEntries, "entry", nil, "account=<id>,debit=<amt> or account=<id>,credit=<amt> (repeatable, at least 2 required)")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("entry")
	resource.Doc{
		Summary: "Post a balanced double-entry transaction",
		Detail: "Repeat --entry once per posting line. Posting is atomic - the API " +
			"never produces an unbalanced or partially-posted transaction - and, as " +
			"of today, permanent: there is no void/reverse RPC yet, so correcting a " +
			"mistake means posting a new, reversing transaction rather than undoing " +
			"this one.",
		Examples: []resource.Example{{Cmd: "avactl ledger-transaction post --date 2026-01-15 " +
			"--entry account=101,debit=500.00 --entry account=400,credit=500.00"}},
	}.Apply(cmd)
	return cmd
}
