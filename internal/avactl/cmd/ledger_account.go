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

var ledgerAccountNoun = resource.Noun{
	Singular: "ledger-account",
	Plural:   "ledger accounts",
	Aliases:  []string{"ledger-accounts", "la"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerAccount).GetId()) }},
		{Header: "CODE", Value: func(v proto.Message) string { return v.(*avav1.LedgerAccount).GetCode() }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.LedgerAccount).GetName() }},
		{Header: "TYPE", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerAccount).GetAccountTypeId()) }},
		{Header: "SYSTEM", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.LedgerAccount).GetIsSystem()) }},
		{Header: "PARENT", Value: func(v proto.Message) string {
			a := v.(*avav1.LedgerAccount)
			if a.ParentAccountId == nil {
				return ""
			}
			return fmt.Sprintf("%d", a.GetParentAccountId())
		}},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.LedgerAccount).GetIsActive()) }},
		{Header: "VERSION", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerAccount).GetResourceVersion()) }},
	},
}

func newLedgerAccountCmd() *cobra.Command {
	root := newGroupCmd(ledgerAccountNoun, "Manage the chart of accounts")

	var includeSubaccounts bool
	listCmd := newListCmd(ledgerAccountNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listLedgerAccounts(ctx, conn, businessID, includeSubaccounts)
	})
	listCmd.Flags().BoolVar(&includeSubaccounts, "all", false, "also include customer/vendor sub-accounts (any account with a parent_account_id) - excluded by default")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(ledgerAccountNoun, getLedgerAccount))
	root.AddCommand(newLedgerAccountCreateCmd())
	root.AddCommand(newLedgerAccountUpdateCmd())
	root.AddCommand(newVersionedMutateCmd(ledgerAccountNoun, "deactivate", "Deactivate a ledger account", deactivateLedgerAccount))
	return root
}

func getLedgerAccount(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid ledger-account id %q: %w", id, err)
	}
	resp, err := avav1.NewLedgerAccountServiceClient(conn).GetLedgerAccount(ctx, &avav1.GetLedgerAccountRequest{Id: int32(n)})
	if err != nil {
		return nil, err
	}
	return resp.GetAccount(), nil
}

func listLedgerAccounts(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeSubaccounts bool) ([]proto.Message, error) {
	resp, err := avav1.NewLedgerAccountServiceClient(conn).ListLedgerAccounts(ctx, &avav1.ListLedgerAccountsRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, 0, len(resp.GetAccounts()))
	for _, a := range resp.GetAccounts() {
		if !includeSubaccounts && a.ParentAccountId != nil {
			continue
		}
		items = append(items, a)
	}
	return items, nil
}

func deactivateLedgerAccount(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid ledger-account id %q: %w", id, err)
	}
	resp, err := avav1.NewLedgerAccountServiceClient(conn).DeactivateLedgerAccount(ctx, &avav1.DeactivateLedgerAccountRequest{Id: int32(n), ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetAccount(), nil
}

func newLedgerAccountCreateCmd() *cobra.Command {
	var code, name, description string
	var accountTypeID, parentID, cashFlowCategoryID, balanceSheetCategoryID, incomeStatementCategoryID int32
	var reconcilable, container bool

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateLedgerAccountRequest{
				BusinessId:     businessID,
				AccountTypeId:  accountTypeID,
				Code:           code,
				Name:           name,
				IsReconcilable: reconcilable,
				IsContainer:    container,
			}
			if description != "" {
				req.Description = &description
			}
			if parentID != 0 {
				req.ParentAccountId = &parentID
			}
			if cashFlowCategoryID != 0 {
				req.CashFlowCategoryId = &cashFlowCategoryID
			}
			if balanceSheetCategoryID != 0 {
				req.BalanceSheetCategoryId = &balanceSheetCategoryID
			}
			if incomeStatementCategoryID != 0 {
				req.IncomeStatementCategoryId = &incomeStatementCategoryID
			}

			resp, err := avav1.NewLedgerAccountServiceClient(conn).CreateLedgerAccount(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAccount(), ledgerAccountNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "account code, e.g. 1000 (required)")
	cmd.Flags().StringVar(&name, "name", "", "account name (required)")
	cmd.Flags().StringVar(&description, "description", "", "account description")
	cmd.Flags().Int32Var(&accountTypeID, "account-type", 0, "ledger_account_type id: 1=ASSETS 2=LIABILITIES 3=EQUITY 4=REVENUE 5=EXPENSES 6=TAX_LIABILITY (required)")
	cmd.Flags().Int32Var(&parentID, "parent", 0, "parent ledger account id (rolls this account up under a container, e.g. Accounts Receivable)")
	cmd.Flags().Int32Var(&cashFlowCategoryID, "cash-flow-category", 0, "cash_flow_category id: 1=Operating 2=Investing 3=Financing")
	cmd.Flags().Int32Var(&balanceSheetCategoryID, "balance-sheet-category", 0, "balance_sheet_category id (for ASSETS/LIABILITIES/EQUITY/TAX_LIABILITY accounts)")
	cmd.Flags().Int32Var(&incomeStatementCategoryID, "income-statement-category", 0, "income_statement_category id: 1=Revenue 2=Cost of Goods Sold 3=Operating Expenses (for REVENUE/EXPENSES accounts)")
	cmd.Flags().BoolVar(&reconcilable, "reconcilable", false, "mark this account eligible for bank-statement reconciliation")
	cmd.Flags().BoolVar(&container, "container", false, "mark this account as a non-postable roll-up node (e.g. Accounts Receivable)")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("account-type")
	resource.Doc{
		Summary:  "Create a chart-of-accounts entry",
		Examples: []resource.Example{{Cmd: "avactl ledger-account create --code 1000 --name Cash --account-type 1"}},
	}.Apply(cmd)
	return cmd
}

func newLedgerAccountUpdateCmd() *cobra.Command {
	var resourceVersion int64
	var name, description string
	var cashFlowCategoryID, balanceSheetCategoryID, incomeStatementCategoryID int32
	var reconcilable, container bool

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid ledger-account id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateLedgerAccountRequest{Id: int32(id), ResourceVersion: resourceVersion}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				req.Description = &description
			}
			if cmd.Flags().Changed("reconcilable") {
				req.IsReconcilable = &reconcilable
			}
			if cmd.Flags().Changed("container") {
				req.IsContainer = &container
			}
			if cmd.Flags().Changed("cash-flow-category") {
				req.CashFlowCategoryId = &cashFlowCategoryID
			}
			if cmd.Flags().Changed("balance-sheet-category") {
				req.BalanceSheetCategoryId = &balanceSheetCategoryID
			}
			if cmd.Flags().Changed("income-statement-category") {
				req.IncomeStatementCategoryId = &incomeStatementCategoryID
			}

			resp, err := avav1.NewLedgerAccountServiceClient(conn).UpdateLedgerAccount(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAccount(), ledgerAccountNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new account name")
	cmd.Flags().StringVar(&description, "description", "", "new account description")
	cmd.Flags().Int32Var(&cashFlowCategoryID, "cash-flow-category", 0, "new cash_flow_category id: 1=Operating 2=Investing 3=Financing")
	cmd.Flags().Int32Var(&balanceSheetCategoryID, "balance-sheet-category", 0, "new balance_sheet_category id")
	cmd.Flags().Int32Var(&incomeStatementCategoryID, "income-statement-category", 0, "new income_statement_category id: 1=Revenue 2=Cost of Goods Sold 3=Operating Expenses")
	cmd.Flags().BoolVar(&reconcilable, "reconcilable", false, "eligible for bank-statement reconciliation")
	cmd.Flags().BoolVar(&container, "container", false, "a non-postable roll-up node")
	addResourceVersionFlag(cmd, &resourceVersion)
	resource.Doc{
		Summary:  "Update a ledger account",
		Detail:   "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{{Cmd: "avactl ledger-account update 40 --name \"Consulting Revenue\""}},
	}.Apply(cmd)
	return cmd
}
