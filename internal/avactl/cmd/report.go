package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
)

// newReportCmd is the `report` parent — read-only financial statements,
// not a CRUD resource, so (like `create`) it stays outside the generic
// get/delete registry.
func newReportCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "report",
		Short: "Run a financial report",
	}
	root.AddCommand(newReportTrialBalanceCmd())
	root.AddCommand(newReportBalanceSheetCmd())
	root.AddCommand(newReportIncomeStatementCmd())
	root.AddCommand(newReportGeneralLedgerCmd())
	root.AddCommand(newReportCustomerStatementCmd())
	return root
}

func newReportTrialBalanceCmd() *cobra.Command {
	var asOf string
	cmd := &cobra.Command{
		Use:   "trial-balance",
		Short: "Trial balance as of a date",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDateFlag(asOf)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			client := avav1.NewReportingServiceClient(conn)

			if flagOutput == output.FormatPDF {
				resp, err := client.GetTrialBalancePdf(cmd.Context(), &avav1.GetTrialBalancePdfRequest{BusinessId: businessID, AsOf: d})
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(resp.GetContent())
				return err
			}

			resp, err := client.GetTrialBalance(cmd.Context(), &avav1.GetTrialBalanceRequest{
				BusinessId: businessID,
				AsOf:       d,
			})
			if err != nil {
				return err
			}
			return printTrialBalance(cmd, resp.GetTrialBalance())
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", time.Now().Format("2006-01-02"), "as-of date, YYYY-MM-DD (default today)")
	return cmd
}

func printTrialBalance(cmd *cobra.Command, tb *avav1.TrialBalance) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, tb, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "CODE\tNAME\tDEBIT\tCREDIT")
	for _, l := range tb.GetLines() {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", l.GetAccountCode(), l.GetAccountName(), l.GetDebit().GetValue(), l.GetCredit().GetValue())
	}
	fmt.Fprintf(w, "TOTAL\t\t%s\t%s\n", tb.GetTotalDebit().GetValue(), tb.GetTotalCredit().GetValue())
	return nil
}

func newReportBalanceSheetCmd() *cobra.Command {
	var asOf string
	cmd := &cobra.Command{
		Use:   "balance-sheet",
		Short: "Balance sheet as of a date",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDateFlag(asOf)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			client := avav1.NewReportingServiceClient(conn)

			if flagOutput == output.FormatPDF {
				resp, err := client.GetBalanceSheetPdf(cmd.Context(), &avav1.GetBalanceSheetPdfRequest{BusinessId: businessID, AsOf: d})
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(resp.GetContent())
				return err
			}

			resp, err := client.GetBalanceSheet(cmd.Context(), &avav1.GetBalanceSheetRequest{
				BusinessId: businessID,
				AsOf:       d,
			})
			if err != nil {
				return err
			}
			return printBalanceSheet(cmd, resp.GetBalanceSheet())
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", time.Now().Format("2006-01-02"), "as-of date, YYYY-MM-DD (default today)")
	return cmd
}

func printBalanceSheet(cmd *cobra.Command, bs *avav1.BalanceSheet) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, bs, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "SECTION\tACCOUNT\tASSET\tLIABILITY")
	for i, s := range bs.GetSections() {
		for _, l := range s.GetAssetLines() {
			fmt.Fprintf(w, "%s\t%s\t%s\t\n", s.GetTitle(), l.GetAccountName(), l.GetBalance().GetValue())
		}
		for _, l := range s.GetLiabilityLines() {
			fmt.Fprintf(w, "%s\t%s\t\t%s\n", s.GetTitle(), l.GetAccountName(), l.GetBalance().GetValue())
		}
		fmt.Fprintf(w, "%s\t(total)\t%s\t%s\n", s.GetTitle(), s.GetTotalAssets().GetValue(), s.GetTotalLiabilities().GetValue())
		switch i {
		case 1:
			fmt.Fprintf(w, "\tNet current assets (liabilities)\t%s\t\n", bs.GetNetCurrentAssets().GetValue())
			fmt.Fprintf(w, "\tTotal assets less current liabilities\t%s\t\n", bs.GetTotalAssetsLessCurrentLiabilities().GetValue())
		case 2:
			fmt.Fprintf(w, "\tTotal net assets (liabilities)\t%s\t\n", bs.GetTotalNetAssets().GetValue())
		}
	}
	fmt.Fprintf(w, "TOTAL\t\t%s\t%s\n", bs.GetTotalAssets().GetValue(), bs.GetTotalLiabilities().GetValue())
	return nil
}

func newReportIncomeStatementCmd() *cobra.Command {
	var start, end string
	cmd := &cobra.Command{
		Use:   "income-statement",
		Short: "Income statement (P&L) over a date range",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := parseDateFlag(start)
			if err != nil {
				return err
			}
			e, err := parseDateFlag(end)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			client := avav1.NewReportingServiceClient(conn)

			if flagOutput == output.FormatPDF {
				resp, err := client.GetIncomeStatementPdf(cmd.Context(), &avav1.GetIncomeStatementPdfRequest{BusinessId: businessID, PeriodStart: s, PeriodEnd: e})
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(resp.GetContent())
				return err
			}

			resp, err := client.GetIncomeStatement(cmd.Context(), &avav1.GetIncomeStatementRequest{
				BusinessId:  businessID,
				PeriodStart: s,
				PeriodEnd:   e,
			})
			if err != nil {
				return err
			}
			return printIncomeStatement(cmd, resp.GetIncomeStatement())
		},
	}
	cmd.Flags().StringVar(&start, "start", "", "period start date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&end, "end", time.Now().Format("2006-01-02"), "period end date, YYYY-MM-DD (default today)")
	_ = cmd.MarkFlagRequired("start")
	return cmd
}

func printIncomeStatement(cmd *cobra.Command, is *avav1.IncomeStatement) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, is, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "REVENUE")
	for _, l := range is.GetRevenue() {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", l.GetAccountCode(), l.GetAccountName(), l.GetAmount().GetValue())
	}
	fmt.Fprintf(w, "  TOTAL REVENUE\t\t%s\n", is.GetTotalRevenue().GetValue())
	fmt.Fprintln(w, "COST OF GOODS SOLD")
	for _, l := range is.GetCostOfGoodsSold() {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", l.GetAccountCode(), l.GetAccountName(), l.GetAmount().GetValue())
	}
	fmt.Fprintf(w, "  TOTAL COST OF GOODS SOLD\t\t%s\n", is.GetTotalCostOfGoodsSold().GetValue())
	fmt.Fprintf(w, "GROSS PROFIT\t\t%s\n", is.GetGrossProfit().GetValue())
	fmt.Fprintln(w, "OPERATING EXPENSES")
	for _, l := range is.GetOperatingExpenses() {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", l.GetAccountCode(), l.GetAccountName(), l.GetAmount().GetValue())
	}
	fmt.Fprintf(w, "  TOTAL OPERATING EXPENSES\t\t%s\n", is.GetTotalOperatingExpenses().GetValue())
	fmt.Fprintf(w, "TOTAL EXPENSES\t\t%s\n", is.GetTotalExpenses().GetValue())
	fmt.Fprintf(w, "NET INCOME\t\t%s\n", is.GetNetIncome().GetValue())
	return nil
}

func newReportGeneralLedgerCmd() *cobra.Command {
	var account int32
	var start, end string
	cmd := &cobra.Command{
		Use:   "general-ledger",
		Short: "General-ledger detail for one account over a date range",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := parseDateFlag(start)
			if err != nil {
				return err
			}
			e, err := parseDateFlag(end)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			client := avav1.NewReportingServiceClient(conn)

			if flagOutput == output.FormatPDF {
				resp, err := client.GetGeneralLedgerPdf(cmd.Context(), &avav1.GetGeneralLedgerPdfRequest{BusinessId: businessID, AccountId: account, PeriodStart: s, PeriodEnd: e})
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(resp.GetContent())
				return err
			}

			resp, err := client.GetGeneralLedger(cmd.Context(), &avav1.GetGeneralLedgerRequest{
				BusinessId:  businessID,
				AccountId:   account,
				PeriodStart: s,
				PeriodEnd:   e,
			})
			if err != nil {
				return err
			}
			return printGeneralLedger(cmd, resp.GetGeneralLedger())
		},
	}
	cmd.Flags().Int32Var(&account, "account", 0, "ledger account id (required)")
	cmd.Flags().StringVar(&start, "start", "0001-01-01", "period start date, YYYY-MM-DD (default: inception)")
	cmd.Flags().StringVar(&end, "end", time.Now().Format("2006-01-02"), "period end date, YYYY-MM-DD (default today)")
	_ = cmd.MarkFlagRequired("account")
	return cmd
}

func printGeneralLedger(cmd *cobra.Command, gl *avav1.GeneralLedger) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, gl, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s %s\n", gl.GetAccountCode(), gl.GetAccountName())
	fmt.Fprintln(w, "DATE\tTXN\tDEBIT\tCREDIT\tBALANCE")
	for _, l := range gl.GetLines() {
		d := l.GetTransactionDate()
		fmt.Fprintf(w, "%04d-%02d-%02d\t%d\t%s\t%s\t%s\n",
			d.GetYear(), d.GetMonth(), d.GetDay(),
			l.GetLedgerTransactionId(), l.GetDebit().GetValue(), l.GetCredit().GetValue(), l.GetRunningBalance().GetValue())
	}
	fmt.Fprintf(w, "ENDING BALANCE\t\t\t\t%s\n", gl.GetEndingBalance().GetValue())
	return nil
}

func newReportCustomerStatementCmd() *cobra.Command {
	var contact int64
	var start, end string
	cmd := &cobra.Command{
		Use:   "customer-statement",
		Short: "Invoice/payment activity, running balance, and aging for one contact",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := parseDateFlag(start)
			if err != nil {
				return err
			}
			e, err := parseDateFlag(end)
			if err != nil {
				return err
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			client := avav1.NewReportingServiceClient(conn)

			if flagOutput == output.FormatPDF {
				resp, err := client.GetCustomerStatementPdf(cmd.Context(), &avav1.GetCustomerStatementPdfRequest{ContactId: contact, PeriodStart: s, PeriodEnd: e})
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(resp.GetContent())
				return err
			}

			resp, err := client.GetCustomerStatement(cmd.Context(), &avav1.GetCustomerStatementRequest{ContactId: contact, PeriodStart: s, PeriodEnd: e})
			if err != nil {
				return err
			}
			return printCustomerStatement(cmd, resp.GetStatement())
		},
	}
	cmd.Flags().Int64Var(&contact, "contact", 0, "contact id (required)")
	cmd.Flags().StringVar(&start, "start", "0001-01-01", "activity period start date, YYYY-MM-DD (default: inception)")
	cmd.Flags().StringVar(&end, "end", time.Now().Format("2006-01-02"), "activity period end date, YYYY-MM-DD (default today); aging is always as of this date")
	_ = cmd.MarkFlagRequired("contact")
	return cmd
}

func printCustomerStatement(cmd *cobra.Command, st *avav1.CustomerStatement) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, st, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s\n", st.GetContactName())
	fmt.Fprintln(w, "DATE\tDESCRIPTION\tDEBIT\tCREDIT\tBALANCE")
	for _, a := range st.GetActivity() {
		d := a.GetDate()
		fmt.Fprintf(w, "%04d-%02d-%02d\t%s\t%s\t%s\t%s\n",
			d.GetYear(), d.GetMonth(), d.GetDay(), a.GetDescription(), a.GetDebit().GetValue(), a.GetCredit().GetValue(), a.GetRunningBalance().GetValue())
	}
	fmt.Fprintf(w, "ENDING BALANCE\t\t\t\t%s\n", st.GetEndingBalance().GetValue())
	fmt.Fprintln(w)
	fmt.Fprintln(w, "AGING\tCURRENT\t1-30\t31-60\t61-90\t90+")
	labels := make([]string, len(st.GetAgingBuckets()))
	amounts := make([]string, len(st.GetAgingBuckets()))
	for i, b := range st.GetAgingBuckets() {
		labels[i] = b.GetLabel()
		amounts[i] = b.GetAmount().GetValue()
	}
	fmt.Fprintf(w, "\t%s\t%s\t%s\t%s\t%s\n", amounts[0], amounts[1], amounts[2], amounts[3], amounts[4])
	return nil
}
