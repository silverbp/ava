package cmd

import (
	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

// newReconcileCmd links ledger transactions to a bank statement — not a
// CRUD verb, so (like `create`/`report`/`close`) it stays outside the
// generic get/delete registry.
func newReconcileCmd() *cobra.Command {
	var statementID int64
	var transactionIDs []int64

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Link ledger transactions to a bank statement",
		Long: `Link one or more ledger transactions to a bank statement, marking them
reconciled. Each transaction must already post to the statement's own
ledger account.

  avactl reconcile --statement 3 --transaction 12 --transaction 13`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			res, _ := resource.Lookup("bank-statement")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBankStatement(), res.Columns)
		},
	}
	cmd.Flags().Int64Var(&statementID, "statement", 0, "bank statement id (required)")
	cmd.Flags().Int64SliceVar(&transactionIDs, "transaction", nil, "ledger transaction id to reconcile (repeatable, required)")
	_ = cmd.MarkFlagRequired("statement")
	_ = cmd.MarkFlagRequired("transaction")
	return cmd
}
