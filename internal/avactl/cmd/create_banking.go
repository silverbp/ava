package cmd

import (
	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newCreateBankStatementCmd() *cobra.Command {
	var account int32
	var name, date, opening, closing string

	cmd := &cobra.Command{
		Use:   "bank-statement",
		Short: "Create a bank statement to reconcile against",
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
			res, _ := resource.Lookup("bank-statement")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBankStatement(), res.Columns)
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
	return cmd
}
