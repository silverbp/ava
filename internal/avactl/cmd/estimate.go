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

var estimateNoun = resource.Noun{
	Singular: "estimate",
	Plural:   "estimates",
	Aliases:  []string{"estimates", "est"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Estimate).GetId()) }},
		{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetEstimateNumber() }},
		{Header: "STATUS", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetStatus() }},
		{Header: "TOTAL", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetTotalAmount().GetValue() }},
	},
}

func newEstimateCmd() *cobra.Command {
	root := newGroupCmd(estimateNoun, "Manage estimates")

	var includeAll bool
	listCmd := newListCmd(estimateNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listEstimates(ctx, conn, businessID, includeAll)
	})
	listCmd.Flags().BoolVar(&includeAll, "all", false, "also include accepted, declined, and expired estimates")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(estimateNoun, getEstimate, getEstimatePdf))
	root.AddCommand(newEstimateCreateCmd())
	root.AddCommand(newEstimateUpdateLinesCmd())
	root.AddCommand(newVersionedMutateCmd(estimateNoun, "send", "Mark an estimate SENT", sendEstimate))
	root.AddCommand(newVersionedMutateCmd(estimateNoun, "accept", "Mark an estimate ACCEPTED", acceptEstimate))
	root.AddCommand(newVersionedMutateCmd(estimateNoun, "decline", "Mark an estimate DECLINED", declineEstimate))
	root.AddCommand(newVersionedMutateCmd(estimateNoun, "expire", "Mark an estimate EXPIRED", expireEstimate))
	return root
}

func getEstimate(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid estimate id %q: %w", id, err)
	}
	resp, err := avav1.NewEstimateServiceClient(conn).GetEstimate(ctx, &avav1.GetEstimateRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetEstimate(), nil
}

func getEstimatePdf(ctx context.Context, conn *grpc.ClientConn, id string) ([]byte, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid estimate id %q: %w", id, err)
	}
	resp, err := avav1.NewEstimateServiceClient(conn).GetEstimatePdf(ctx, &avav1.GetEstimatePdfRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetContent(), nil
}

func listEstimates(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeAll bool) ([]proto.Message, error) {
	resp, err := avav1.NewEstimateServiceClient(conn).ListEstimates(ctx, &avav1.ListEstimatesRequest{BusinessId: businessID, IncludeAll: includeAll})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetEstimates()))
	for i, e := range resp.GetEstimates() {
		items[i] = e
	}
	return items, nil
}

func setEstimateStatus(ctx context.Context, conn *grpc.ClientConn, id, status string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid estimate id %q: %w", id, err)
	}
	resp, err := avav1.NewEstimateServiceClient(conn).UpdateEstimateStatus(ctx, &avav1.UpdateEstimateStatusRequest{Id: n, Status: status, ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetEstimate(), nil
}

func sendEstimate(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setEstimateStatus(ctx, conn, id, "SENT", resourceVersion)
}

func acceptEstimate(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setEstimateStatus(ctx, conn, id, "ACCEPTED", resourceVersion)
}

func declineEstimate(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setEstimateStatus(ctx, conn, id, "DECLINED", resourceVersion)
}

func expireEstimate(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	return setEstimateStatus(ctx, conn, id, "EXPIRED", resourceVersion)
}

func newEstimateCreateCmd() *cobra.Command {
	var customer int64
	var date, expires string
	var notes, terms string
	var rawLines []string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rawFields, err := parseLineFlags(rawLines)
			if err != nil {
				return err
			}
			dateArg, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			expiresArg, err := parseDateFlag(expires)
			if err != nil {
				return err
			}

			var lineItems []*avav1.NewEstimateLineItem
			for i, f := range rawFields {
				serviceID, err := parseOptionalInt64(f, "service")
				if err != nil {
					return err
				}
				taxRateID, err := parseOptionalInt64(f, "tax-rate")
				if err != nil {
					return err
				}
				lineItems = append(lineItems, &avav1.NewEstimateLineItem{
					ServiceId:   serviceID,
					LineNumber:  int32(i + 1),
					Description: f["desc"],
					Quantity:    parseDecimalField(f, "qty"),
					UnitPrice:   parseDecimalField(f, "price"),
					IsTaxable:   parseOptionalBool(f, "taxable"),
					TaxRateId:   taxRateID,
				})
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateEstimateRequest{
				BusinessId:     businessID,
				CustomerId:     customer,
				EstimateDate:   dateArg,
				ExpirationDate: expiresArg,
				LineItems:      lineItems,
			}
			if notes != "" {
				req.Notes = &notes
			}
			if terms != "" {
				req.Terms = &terms
			}

			resp, err := avav1.NewEstimateServiceClient(conn).CreateEstimate(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEstimate(), estimateNoun.Columns)
		},
	}
	cmd.Flags().Int64Var(&customer, "customer", 0, "customer contact id (required)")
	cmd.Flags().StringVar(&date, "date", "", "estimate date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&expires, "expires", "", "expiration date, YYYY-MM-DD (required)")
	cmd.Flags().StringVar(&notes, "notes", "", "notes")
	cmd.Flags().StringVar(&terms, "terms", "", "terms")
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,service=<id>[,qty=...][,price=...][,taxable][,tax-rate=<id>] (repeatable) - price/taxable/tax-rate default from the service when omitted")
	_ = cmd.MarkFlagRequired("customer")
	_ = cmd.MarkFlagRequired("date")
	_ = cmd.MarkFlagRequired("expires")
	_ = cmd.MarkFlagRequired("line")
	resource.Doc{
		Summary: "Create an estimate",
		Detail:  "Repeat --line once per line item. price/taxable/tax-rate default from service=<id>'s catalog entry when omitted.",
		Examples: []resource.Example{
			{Cmd: "avactl estimate create --customer 5 --date 2026-01-01 --expires 2026-02-01 " + `--line "desc=Consulting,qty=10,price=150.00,taxable,tax-rate=1"`},
			{Cmd: "avactl estimate create --customer 5 --date 2026-01-01 --expires 2026-02-01 " + `--line "desc=Consulting,service=71,qty=10"`},
		},
	}.Apply(cmd)
	return cmd
}

func newEstimateUpdateLinesCmd() *cobra.Command {
	var rawLines []string
	var resourceVersion int64

	cmd := &cobra.Command{
		Use:  "update-lines <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid estimate id %q: %w", args[0], err)
			}
			rawFields, err := parseLineFlags(rawLines)
			if err != nil {
				return err
			}

			var lineItems []*avav1.NewEstimateLineItem
			for i, f := range rawFields {
				serviceID, err := parseOptionalInt64(f, "service")
				if err != nil {
					return err
				}
				taxRateID, err := parseOptionalInt64(f, "tax-rate")
				if err != nil {
					return err
				}
				lineItems = append(lineItems, &avav1.NewEstimateLineItem{
					ServiceId:   serviceID,
					LineNumber:  int32(i + 1),
					Description: f["desc"],
					Quantity:    parseDecimalField(f, "qty"),
					UnitPrice:   parseDecimalField(f, "price"),
					IsTaxable:   parseOptionalBool(f, "taxable"),
					TaxRateId:   taxRateID,
				})
			}

			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewEstimateServiceClient(conn).UpdateEstimateLineItems(cmd.Context(), &avav1.UpdateEstimateLineItemsRequest{
				Id:              id,
				LineItems:       lineItems,
				ResourceVersion: resourceVersion,
			})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEstimate(), estimateNoun.Columns)
		},
	}
	cmd.Flags().StringArrayVar(&rawLines, "line", nil, "desc=...,service=<id>[,qty=...][,price=...][,taxable][,tax-rate=<id>] (repeatable) - price/taxable/tax-rate default from the service when omitted")
	addResourceVersionFlag(cmd, &resourceVersion)
	_ = cmd.MarkFlagRequired("line")
	resource.Doc{
		Summary: "Replace an estimate's line items",
		Detail:  "Replaces the entire line item set - repeat --line once per line item, including ones you're keeping unchanged.",
		Examples: []resource.Example{{Cmd: "avactl estimate update-lines 42 " +
			`--line "desc=Consulting,qty=10,price=150.00,taxable,tax-rate=1"`}},
	}.Apply(cmd)
	return cmd
}
