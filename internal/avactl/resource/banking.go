// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

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
		Name:    "bank-statement",
		Aliases: []string{"bank-statements", "bs"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.BankStatement).GetId()) }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetStatementName() }},
			{Header: "ACCOUNT", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.BankStatement).GetLedgerAccountId()) }},
			{Header: "CLOSING", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetClosingBalance().GetValue() }},
			{Header: "RECONCILED", Value: func(v proto.Message) string { return v.(*avav1.BankStatement).GetReconciledBalance().GetValue() }},
			{Header: "LINES", Value: func(v proto.Message) string { return fmt.Sprintf("%d", len(v.(*avav1.BankStatement).GetLines())) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid bank-statement id %q: %w", id, err)
			}
			resp, err := avav1.NewBankStatementServiceClient(conn).GetBankStatement(ctx, &avav1.GetBankStatementRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetBankStatement(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewBankStatementServiceClient(conn).ListBankStatements(ctx, &avav1.ListBankStatementsRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetBankStatements()))
			for i, bs := range resp.GetBankStatements() {
				items[i] = bs
			}
			return items, nil
		},
	})
}
