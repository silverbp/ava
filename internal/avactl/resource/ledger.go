// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package resource

import (
	"context"
	"fmt"
	"strconv"

	typepb "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

func init() {
	Register(&Resource{
		Name:    "ledger-account",
		Aliases: []string{"ledger-accounts", "la"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerAccount).GetId()) }},
			{Header: "CODE", Value: func(v proto.Message) string { return v.(*avav1.LedgerAccount).GetCode() }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.LedgerAccount).GetName() }},
			{Header: "TYPE", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerAccount).GetAccountTypeId()) }},
			{Header: "SYSTEM", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.LedgerAccount).GetIsSystem()) }},
			{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.LedgerAccount).GetIsActive()) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid ledger-account id %q: %w", id, err)
			}
			resp, err := avav1.NewLedgerAccountServiceClient(conn).GetLedgerAccount(ctx, &avav1.GetLedgerAccountRequest{Id: int32(n)})
			if err != nil {
				return nil, err
			}
			return resp.GetAccount(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewLedgerAccountServiceClient(conn).ListLedgerAccounts(ctx, &avav1.ListLedgerAccountsRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetAccounts()))
			for i, a := range resp.GetAccounts() {
				items[i] = a
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid ledger-account id %q: %w", id, err)
			}
			resp, err := avav1.NewLedgerAccountServiceClient(conn).DeactivateLedgerAccount(ctx, &avav1.DeactivateLedgerAccountRequest{Id: int32(n)})
			if err != nil {
				return nil, err
			}
			return resp.GetAccount(), nil
		},
	})

	Register(&Resource{
		Name:    "ledger-transaction",
		Aliases: []string{"ledger-transactions", "lt"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.LedgerTransaction).GetId()) }},
			{Header: "DATE", Value: func(v proto.Message) string { return formatDate(v.(*avav1.LedgerTransaction).GetTransactionDate()) }},
			{Header: "DESCRIPTION", Value: func(v proto.Message) string { return v.(*avav1.LedgerTransaction).GetDescription() }},
			{Header: "ENTRIES", Value: func(v proto.Message) string { return fmt.Sprintf("%d", len(v.(*avav1.LedgerTransaction).GetEntries())) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid ledger-transaction id %q: %w", id, err)
			}
			resp, err := avav1.NewLedgerTransactionServiceClient(conn).GetLedgerTransaction(ctx, &avav1.GetLedgerTransactionRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetTransaction(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewLedgerTransactionServiceClient(conn).ListLedgerTransactions(ctx, &avav1.ListLedgerTransactionsRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetTransactions()))
			for i, t := range resp.GetTransactions() {
				items[i] = t
			}
			return items, nil
		},
		// No Delete: ledger transactions have no void/delete RPC yet — the
		// schema soft-deletes them but nothing in the API exposes that.
	})
}

func formatDate(d *typepb.Date) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
}
