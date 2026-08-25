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

var bankStatementNoun = resource.Noun{
	Singular: "bank-statement",
	Plural:   "bank statements",
	Aliases:  []string{"bank-statements", "bs"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.BankStatement).GetId()) }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetStatementName() }},
		{Header: "ACCOUNT", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.BankStatement).GetLedgerAccountId()) }},
		{Header: "CLOSING", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetClosingBalance().GetValue() }},
		{Header: "RECONCILED", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetReconciledBalance().GetValue() }},
		{Header: "LINES", Value: func(v proto.Message) string { return fmt.Sprintf("%d", len(v.(*avav1.BankStatement).GetLines())) }},
	},
}

func newBankStatementCmd() *cobra.Command {
	root := newGroupCmd(bankStatementNoun, "Manage bank statements and reconciliation")
	root.AddCommand(newListCmd(bankStatementNoun, listBankStatements))
	root.AddCommand(newGetCmd(bankStatementNoun, getBankStatement))
	root.AddCommand(newBankStatementCreateCmd())
	root.AddCommand(newBankStatementReconcileCmd())
	root.AddCommand(newBankStatementUnreconciledCmd())
	return root
}

func getBankStatement(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bank-statement id %q: %w", id, err)
	}
	resp, err := avav1.NewBankStatementServiceClient(conn).GetBankStatement(ctx, &avav1.GetBankStatementRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetBankStatement(), nil
}

func listBankStatements(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewBankStatementServiceClient(conn).ListBankStatements(ctx, &avav1.ListBankStatementsRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetBankStatements()))
	for i, bs := range resp.GetBankStatements() {
		items[i] = bs
	}
	return items, nil
}

func newBankStatementCreateCmd() *cobra.Command {
	var account int32
	var name, date, opening, closing string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dateArg, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBankStatementServiceClient(conn).CreateBankStatement(cmd.Context(), &avav1.CreateBankStatementRequest{
				BusinessId:      businessID,
				LedgerAccountId: account,
				StatementName:   name,
				StatementDate:   dateArg,
				OpeningBalance:  &avav1.Decimal{Value: opening},
				ClosingBalance:  &avav1.Decimal{Value: closing},
			})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBankStatement(), bankStatementNoun.Columns)
		},
	}
	cmd.Flags().Int32Var(&account, "account", 0, "is_reconcilable ledger account id (required)")
	cmd.Flags().StringVar(&name, "name", "", "statement name/label (required)")
	cmd.Flags().StringVar(&date, "date", "", "statement date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&opening, "opening", "0", "opening balance per the bank statement")
	cmd.Flags().StringVar(&closing, "closing", "0", "closing balance per the bank statement")
	_ = cmd.MarkFlagRequired("account")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("date")
	resource.Doc{
		Summary:  "Create a bank statement to reconcile against",
		Examples: []resource.Example{{Cmd: "avactl bank-statement create --account 10 --name \"Jan 2026\" --date 2026-01-31 --opening 1000.00 --closing 1500.00"}},
	}.Apply(cmd)
	return cmd
}

func newBankStatementReconcileCmd() *cobra.Command {
	var transactionIDs []int64

	cmd := &cobra.Command{
		Use:  "reconcile <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statementID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid bank-statement id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBankStatementServiceClient(conn).ReconcileLedgerTransactions(cmd.Context(), &avav1.ReconcileLedgerTransactionsRequest{
				BankStatementId:      statementID,
				LedgerTransactionIds: transactionIDs,
			})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBankStatement(), bankStatementNoun.Columns)
		},
	}
	cmd.Flags().Int64SliceVar(&transactionIDs, "transaction", nil, "ledger transaction id to reconcile (repeatable, required)")
	_ = cmd.MarkFlagRequired("transaction")
	resource.Doc{
		Summary: "Link ledger transactions to a bank statement",
		Detail:  "Each transaction must already post to the statement's own ledger account.",
		Examples: []resource.Example{
			{Cmd: "avactl bank-statement reconcile 3 --transaction 12 --transaction 13"},
		},
	}.Apply(cmd)
	return cmd
}

func newBankStatementUnreconciledCmd() *cobra.Command {
	var account int32
	var through string

	cmd := &cobra.Command{
		Use:  "unreconciled",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			throughArg, err := parseDateFlag(through)
			if err != nil {
				return err
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBankStatementServiceClient(conn).ListUnreconciledLedgerTransactions(cmd.Context(), &avav1.ListUnreconciledLedgerTransactionsRequest{
				LedgerAccountId: account,
				ThroughDate:     throughArg,
			})
			if err != nil {
				return err
			}
			items := make([]proto.Message, len(resp.GetTransactions()))
			for i, t := range resp.GetTransactions() {
				items[i] = t
			}
			return output.PrintList(cmd.OutOrStdout(), flagOutput, items, ledgerTransactionNoun.Columns)
		},
	}
	cmd.Flags().Int32Var(&account, "account", 0, "ledger account id (required)")
	cmd.Flags().StringVar(&through, "through", "", "list transactions posted through this date, YYYY-MM-DD (required)")
	_ = cmd.MarkFlagRequired("account")
	_ = cmd.MarkFlagRequired("through")
	resource.Doc{
		Summary:  "List ledger transactions not yet linked to any bank statement",
		Detail:   "Candidates for `bank-statement reconcile` on the given account, through the given date.",
		Examples: []resource.Example{{Cmd: "avactl bank-statement unreconciled --account 10 --through 2026-01-31"}},
	}.Apply(cmd)
	return cmd
}
