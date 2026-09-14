// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package cmd

import (
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/resource"
)

var ledgerTransactionNoun = resource.Noun{
	Singular: "ledger-transaction",
	Plural:   "ledger transactions",
	Aliases:  []string{"ledger-transactions", "lt"},
	Columns: []resource.Column{
		resource.Int("ID", (*avav1.LedgerTransaction).GetId),
		resource.Date("DATE", (*avav1.LedgerTransaction).GetTransactionDate),
		resource.Str("DESCRIPTION", (*avav1.LedgerTransaction).GetDescription),
		resource.Int("ENTRIES", func(t *avav1.LedgerTransaction) int { return len(t.GetEntries()) }),
		resource.OptInt("REVERSES", func(t *avav1.LedgerTransaction) *int64 { return t.ReversesLedgerTransactionId }),
	},
}

func newLedgerTransactionCmd() *cobra.Command {
	root := newGroupCmd(ledgerTransactionNoun, "Read and post the double-entry ledger")
	root.AddCommand(
		newListCmd(ledgerTransactionNoun, func(r run) ([]proto.Message, error) {
			resp, err := avav1.NewLedgerTransactionServiceClient(r.conn).ListLedgerTransactions(r.ctx, &avav1.ListLedgerTransactionsRequest{BusinessId: r.businessID})
			return toMessages(resp.GetTransactions()), err
		}),
		newGetCmd(ledgerTransactionNoun, func(r run, id int64) (proto.Message, error) {
			resp, err := avav1.NewLedgerTransactionServiceClient(r.conn).GetLedgerTransaction(r.ctx, &avav1.GetLedgerTransactionRequest{Id: id})
			return resp.GetTransaction(), err
		}),
		newLedgerTransactionPostCmd(),
		newLedgerTransactionReverseCmd(),
	)
	return root
}

func newLedgerTransactionReverseCmd() *cobra.Command {
	var date string
	cmd := newMutateCmd(ledgerTransactionNoun, "reverse", resource.Doc{
		Summary: "Post a new transaction reversing an existing one",
		Detail: "Mirrors every entry of the original transaction with debit and credit swapped; the " +
			"original is never touched. A transaction can be reversed once, and a reversal can't itself " +
			"be reversed. Rejected if the transaction is linked from an invoice or " +
			"payment - correct those through `invoice cancel` / `payment void` instead, which keep " +
			"paid_amount/balance_due in sync.",
		Examples: []resource.Example{
			{Cmd: "avactl ledger-transaction reverse 42"},
			{Cmd: "avactl ledger-transaction reverse 42 --date 2026-02-01", Desc: "original is in a closed period - post the reversal in the open one"},
		},
	}, func(r run, id int64) (proto.Message, error) {
		reversalDate, err := r.optDate("date", &date)
		if err != nil {
			return nil, err
		}
		resp, err := avav1.NewLedgerTransactionServiceClient(r.conn).ReverseLedgerTransaction(r.ctx, &avav1.ReverseLedgerTransactionRequest{Id: id, ReversalDate: reversalDate})
		return resp.GetTransaction(), err
	})
	cmd.Flags().StringVar(&date, "date", "", "date to post the reversal on (YYYY-MM-DD); defaults to the original's date - set it when the original falls in a closed period")
	return cmd
}

func newLedgerTransactionPostCmd() *cobra.Command {
	var date, description, reference string
	var rawEntries []string

	cmd := newNoArgCmd(ledgerTransactionNoun, "post", resource.Doc{
		Summary: "Post a balanced double-entry transaction",
		Detail: "Repeat --entry once per posting line. Posting is atomic - the API " +
			"never produces an unbalanced or partially-posted transaction - and " +
			"permanent: there is no edit or delete, so correcting a mistake means " +
			"`ledger-transaction reverse` rather than undoing this one.",
		Examples: []resource.Example{{Cmd: "avactl ledger-transaction post --date 2026-01-15 " +
			"--entry account=101,debit=500.00 --entry account=400,credit=500.00"}},
	}, func(r run) (proto.Message, error) {
		entries, err := parseEntryFlags(rawEntries)
		if err != nil {
			return nil, err
		}
		txnDate, err := parseDateFlag("date", date)
		if err != nil {
			return nil, err
		}
		resp, err := avav1.NewLedgerTransactionServiceClient(r.conn).CreateLedgerTransaction(r.ctx, &avav1.CreateLedgerTransactionRequest{
			BusinessId:      r.businessID,
			TransactionDate: txnDate,
			Entries:         entries,
			Description:     r.optString("description", &description),
			ReferenceNumber: r.optString("reference", &reference),
		})
		return resp.GetTransaction(), err
	})
	cmd.Flags().StringVar(&date, "date", "", "transaction date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&description, "description", "", "transaction description")
	cmd.Flags().StringVar(&reference, "reference", "", "reference number")
	cmd.Flags().StringArrayVar(&rawEntries, "entry", nil, "account=<id>,debit=<amt> or account=<id>,credit=<amt> (repeatable, at least 2 required)")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("entry")
	return cmd
}
