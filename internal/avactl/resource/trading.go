package resource

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

func init() {
	Register(&Resource{
		Name:    "estimate",
		Aliases: []string{"estimates", "est"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Estimate).GetId()) }},
			{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetEstimateNumber() }},
			{Header: "STATUS", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetStatus() }},
			{Header: "TOTAL", Value: func(v proto.Message) string { return v.(*avav1.Estimate).GetTotalAmount().GetValue() }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid estimate id %q: %w", id, err)
			}
			resp, err := avav1.NewEstimateServiceClient(conn).GetEstimate(ctx, &avav1.GetEstimateRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetEstimate(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewEstimateServiceClient(conn).ListEstimates(ctx, &avav1.ListEstimatesRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetEstimates()))
			for i, e := range resp.GetEstimates() {
				items[i] = e
			}
			return items, nil
		},
	})

	Register(&Resource{
		Name:    "invoice",
		Aliases: []string{"invoices", "inv"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Invoice).GetId()) }},
			{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetInvoiceNumber() }},
			{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetInvoiceType() }},
			{Header: "STATUS", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetStatus() }},
			{Header: "TOTAL", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetTotalAmount().GetValue() }},
			{Header: "BALANCE_DUE", Value: func(v proto.Message) string { return v.(*avav1.Invoice).GetBalanceDue().GetValue() }},
			{Header: "POSTED", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Invoice).LedgerTransactionId != nil) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
			}
			resp, err := avav1.NewInvoiceServiceClient(conn).GetInvoice(ctx, &avav1.GetInvoiceRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetInvoice(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewInvoiceServiceClient(conn).ListInvoices(ctx, &avav1.ListInvoicesRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetInvoices()))
			for i, inv := range resp.GetInvoices() {
				items[i] = inv
			}
			return items, nil
		},
		GetPdf: func(ctx context.Context, conn *grpc.ClientConn, id string) ([]byte, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid invoice id %q: %w", id, err)
			}
			resp, err := avav1.NewInvoiceServiceClient(conn).GetInvoicePdf(ctx, &avav1.GetInvoicePdfRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetContent(), nil
		},
	})

	Register(&Resource{
		Name:    "payment",
		Aliases: []string{"payments", "pay"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Payment).GetId()) }},
			{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentNumber() }},
			{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentType() }},
			{Header: "AMOUNT", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetAmount().GetValue() }},
			{Header: "METHOD", Value: func(v proto.Message) string { return v.(*avav1.Payment).GetPaymentMethod() }},
			{Header: "POSTED", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Payment).LedgerTransactionId != nil) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid payment id %q: %w", id, err)
			}
			resp, err := avav1.NewPaymentServiceClient(conn).GetPayment(ctx, &avav1.GetPaymentRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetPayment(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewPaymentServiceClient(conn).ListPayments(ctx, &avav1.ListPaymentsRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetPayments()))
			for i, p := range resp.GetPayments() {
				items[i] = p
			}
			return items, nil
		},
	})
}
