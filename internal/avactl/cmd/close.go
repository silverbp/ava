package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	typepb "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
)

func formatDateArg(d *typepb.Date) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
}

// newCloseCmd is the `close` parent — trigger/reverse/list are period-close
// verbs, not CRUD, so (like `create`/`report`) it stays outside the
// generic get/delete registry.
func newCloseCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "close",
		Short: "Trigger, reverse, or list period closes",
	}
	root.AddCommand(newCloseTriggerCmd())
	root.AddCommand(newCloseReverseCmd())
	root.AddCommand(newCloseListCmd())
	return root
}

func newCloseTriggerCmd() *cobra.Command {
	var periodEnd string
	cmd := &cobra.Command{
		Use:   "trigger",
		Short: "Close the books through a date",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parseDateFlag(periodEnd)
			if err != nil {
				return err
			}
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewPeriodCloseServiceClient(conn).TriggerClose(cmd.Context(), &avav1.TriggerCloseRequest{
				BusinessId: businessID,
				PeriodEnd:  d,
			})
			if err != nil {
				return err
			}
			return printPeriodClose(cmd, resp.GetPeriodClose())
		},
	}
	cmd.Flags().StringVar(&periodEnd, "through", "", "close through this date, YYYY-MM-DD (required)")
	_ = cmd.MarkFlagRequired("through")
	return cmd
}

func newCloseReverseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reverse <period-close-id>",
		Short: "Reverse a period close",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id int64
			if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
				return fmt.Errorf("invalid period-close id %q", args[0])
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewPeriodCloseServiceClient(conn).ReverseClose(cmd.Context(), &avav1.ReverseCloseRequest{Id: id})
			if err != nil {
				return err
			}
			return printPeriodClose(cmd, resp.GetPeriodClose())
		},
	}
	return cmd
}

func newCloseListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List a business's close history",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewPeriodCloseServiceClient(conn).ListPeriodCloses(cmd.Context(), &avav1.ListPeriodClosesRequest{BusinessId: businessID})
			if err != nil {
				return err
			}
			if flagOutput != output.FormatTable {
				items := make([]proto.Message, len(resp.GetPeriodCloses()))
				for i, pc := range resp.GetPeriodCloses() {
					items[i] = pc
				}
				return output.PrintList(cmd.OutOrStdout(), flagOutput, items, nil)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "ID\tPERIOD_START\tPERIOD_END\tREVERSED\tENTRIES")
			for _, pc := range resp.GetPeriodCloses() {
				fmt.Fprintf(w, "%d\t%s\t%s\t%v\t%d\n",
					pc.GetId(), formatDateArg(pc.GetPeriodStart()), formatDateArg(pc.GetPeriodEnd()),
					pc.GetReversedAt() != nil, len(pc.GetGeneratedLedgerTransactionIds()))
			}
			return nil
		},
	}
}

func printPeriodClose(cmd *cobra.Command, pc *avav1.PeriodClose) error {
	if flagOutput != output.FormatTable {
		return output.PrintOne(cmd.OutOrStdout(), flagOutput, pc, nil)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "ID\tPERIOD_START\tPERIOD_END\tREVERSED\tENTRIES")
	fmt.Fprintf(w, "%d\t%s\t%s\t%v\t%d\n",
		pc.GetId(), formatDateArg(pc.GetPeriodStart()), formatDateArg(pc.GetPeriodEnd()),
		pc.GetReversedAt() != nil, len(pc.GetGeneratedLedgerTransactionIds()))
	return nil
}
