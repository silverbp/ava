// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package cmd

import (
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/resource"
)

var periodCloseNoun = resource.Noun{
	Singular: "close",
	Plural:   "period closes",
	Columns: []resource.Column{
		resource.Int("ID", (*avav1.PeriodClose).GetId),
		resource.Date("PERIOD_START", (*avav1.PeriodClose).GetPeriodStart),
		resource.Date("PERIOD_END", (*avav1.PeriodClose).GetPeriodEnd),
		resource.Bool("REVERSED", func(pc *avav1.PeriodClose) bool { return pc.GetReversedAt() != nil }),
		resource.Int("ENTRIES", func(pc *avav1.PeriodClose) int { return len(pc.GetGeneratedLedgerTransactionIds()) }),
	},
}

// newCloseCmd is the `close` parent — trigger/reverse/list are period-close
// verbs rather than CRUD, but they still reduce to the generic verb shapes.
func newCloseCmd() *cobra.Command {
	root := newGroupCmd(periodCloseNoun, "Trigger, reverse, or list period closes")
	root.AddCommand(
		newCloseTriggerCmd(),
		newMutateCmd(periodCloseNoun, "reverse", resource.Doc{
			Summary:  "Reverse a period close",
			Examples: []resource.Example{{Cmd: "avactl close reverse 5"}},
		}, func(r run, id int64) (proto.Message, error) {
			resp, err := avav1.NewPeriodCloseServiceClient(r.conn).ReverseClose(r.ctx, &avav1.ReverseCloseRequest{Id: id})
			return resp.GetPeriodClose(), err
		}),
		newTableCmd(periodCloseNoun, "list", resource.Doc{Summary: "List a business's close history"}, periodCloseNoun.Columns, func(r run) ([]proto.Message, error) {
			resp, err := avav1.NewPeriodCloseServiceClient(r.conn).ListPeriodCloses(r.ctx, &avav1.ListPeriodClosesRequest{BusinessId: r.businessID})
			return toMessages(resp.GetPeriodCloses()), err
		}),
	)
	return root
}

func newCloseTriggerCmd() *cobra.Command {
	var periodEnd string
	cmd := newNoArgCmd(periodCloseNoun, "trigger", resource.Doc{
		Summary:  "Close the books through a date",
		Examples: []resource.Example{{Cmd: "avactl close trigger --through 2026-01-31"}},
	}, func(r run) (proto.Message, error) {
		d, err := parseDateFlag("through", periodEnd)
		if err != nil {
			return nil, err
		}
		resp, err := avav1.NewPeriodCloseServiceClient(r.conn).TriggerClose(r.ctx, &avav1.TriggerCloseRequest{BusinessId: r.businessID, PeriodEnd: d})
		return resp.GetPeriodClose(), err
	})
	cmd.Flags().StringVar(&periodEnd, "through", "", "close through this date, YYYY-MM-DD (required)")
	_ = cmd.MarkFlagRequired("through")
	return cmd
}
