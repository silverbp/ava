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

	Register(&Resource{
		Name:    "business-invite",
		Aliases: []string{"business-invites", "invites"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.BusinessInvite).GetId()) }},
			{Header: "EMAIL", Value: func(v proto.Message) string { return v.(*avav1.BusinessInvite).GetEmail() }},
			{Header: "ROLE", Value: func(v proto.Message) string { return v.(*avav1.BusinessInvite).GetRole() }},
			{Header: "ACCEPTED", Value: func(v proto.Message) string {
				return fmt.Sprintf("%v", v.(*avav1.BusinessInvite).AcceptedAt != nil)
			}},
			{Header: "REVOKED", Value: func(v proto.Message) string {
				return fmt.Sprintf("%v", v.(*avav1.BusinessInvite).RevokedAt != nil)
			}},
		},
		// List is the current context's business — "list invites" naturally
		// means "outstanding invites on the business I'm working in".
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewBusinessServiceClient(conn).ListBusinessInvites(ctx, &avav1.ListBusinessInvitesRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetInvites()))
			for i, inv := range resp.GetInvites() {
				items[i] = inv
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid invite id %q: %w", id, err)
			}
			resp, err := avav1.NewBusinessServiceClient(conn).RevokeBusinessInvite(ctx, &avav1.RevokeBusinessInviteRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetInvite(), nil
		},
	})
}
