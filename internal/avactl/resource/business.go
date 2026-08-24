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
		Name:    "business",
		Aliases: []string{"businesses", "biz"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Business).GetId()) }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Business).GetName() }},
			{Header: "CURRENCY", Value: func(v proto.Message) string { return v.(*avav1.Business).GetCurrencyCode() }},
			{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Business).GetIsActive()) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid business id %q: %w", id, err)
			}
			resp, err := avav1.NewBusinessServiceClient(conn).GetBusiness(ctx, &avav1.GetBusinessRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetBusiness(), nil
		},
		// List ignores businessID: "list businesses" naturally means "list
		// businesses I belong to", not "list businesses scoped to a
		// business" (which wouldn't be meaningful for this resource).
		List: func(ctx context.Context, conn *grpc.ClientConn, _ int64) ([]proto.Message, error) {
			resp, err := avav1.NewBusinessServiceClient(conn).ListMyBusinesses(ctx, &avav1.ListMyBusinessesRequest{})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetMemberships()))
			for i, m := range resp.GetMemberships() {
				items[i] = m.GetBusiness()
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid business id %q: %w", id, err)
			}
			resp, err := avav1.NewBusinessServiceClient(conn).DeactivateBusiness(ctx, &avav1.DeactivateBusinessRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetBusiness(), nil
		},
	})
}
