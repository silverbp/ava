package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	typepb "google.golang.org/genproto/googleapis/type/date"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newCreateLedgerAccountCmd() *cobra.Command {
	var code, name, description string
	var accountTypeID, parentID, cashFlowCategoryID int32
	var reconcilable, container bool

	cmd := &cobra.Command{
		Use:   "ledger-account",
		Short: "Create a chart-of-accounts entry",
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

			resp, err := avav1.NewLedgerAccountServiceClient(conn).CreateLedgerAccount(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("ledger-account")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAccount(), res.Columns)
		},
	}

	cmd.Flags().StringVar(&code, "code", "", "account code, e.g. 1000 (required)")
	cmd.Flags().StringVar(&name, "name", "", "account name (required)")
	cmd.Flags().StringVar(&description, "description", "", "account description")
	cmd.Flags().Int32Var(&accountTypeID, "account-type", 0, "ledger_account_type id: 1=ASSETS 2=LIABILITIES 3=EQUITY 4=REVENUE 5=EXPENSES 6=TAX_LIABILITY (required)")
	cmd.Flags().Int32Var(&parentID, "parent", 0, "parent ledger account id")
	cmd.Flags().Int32Var(&cashFlowCategoryID, "cash-flow-category", 0, "cash_flow_category id: 1=Operating 2=Investing 3=Financing")
	cmd.Flags().BoolVar(&reconcilable, "reconcilable", false, "mark this account eligible for bank-statement reconciliation")
	cmd.Flags().BoolVar(&container, "container", false, "mark this account as a non-postable grouping node")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("account-type")
	return cmd
}

func newCreateLedgerTransactionCmd() *cobra.Command {
	var date, description, reference string
	var rawEntries []string

	cmd := &cobra.Command{
		Use:   "ledger-transaction",
		Short: "Post a double-entry transaction",
		Long: `Post a double-entry transaction. Repeat --entry once per posting line,
each an account=<id>,debit=<amount> or account=<id>,credit=<amount> pair:

  avactl create ledger-transaction --business 1 --date 2026-01-15 \
    --entry account=101,debit=500.00 \
    --entry account=400,credit=500.00`,
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
			res, _ := resource.Lookup("ledger-transaction")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetTransaction(), res.Columns)
		},
	}

	cmd.Flags().StringVar(&date, "date", "", "transaction date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&description, "description", "", "transaction description")
	cmd.Flags().StringVar(&reference, "reference", "", "reference number")
	cmd.Flags().StringArrayVar(&rawEntries, "entry", nil, "account=<id>,debit=<amt> or account=<id>,credit=<amt> (repeatable, at least 2 required)")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("entry")
	return cmd
}

func parseEntryFlags(raw []string) ([]*avav1.NewLedgerEntry, error) {
	var entries []*avav1.NewLedgerEntry
	for _, r := range raw {
		fields := map[string]string{}
		for _, part := range strings.Split(r, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid --entry %q: expected comma-separated key=value pairs", r)
			}
			fields[kv[0]] = kv[1]
		}

		accountStr, ok := fields["account"]
		if !ok {
			return nil, fmt.Errorf("invalid --entry %q: missing account=", r)
		}
		accountID, err := strconv.ParseInt(accountStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid --entry %q: account must be an integer", r)
		}

		ne := &avav1.NewLedgerEntry{AccountId: int32(accountID)}
		if debit, ok := fields["debit"]; ok {
			ne.DebitAmount = &avav1.Decimal{Value: debit}
		}
		if credit, ok := fields["credit"]; ok {
			ne.CreditAmount = &avav1.Decimal{Value: credit}
		}
		entries = append(entries, ne)
	}
	return entries, nil
}

func parseDateFlag(s string) (*typepb.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", s)
	}
	return &typepb.Date{Year: int32(t.Year()), Month: int32(t.Month()), Day: int32(t.Day())}, nil
}
