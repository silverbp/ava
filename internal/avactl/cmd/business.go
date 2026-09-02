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

var businessNoun = resource.Noun{
	Singular: "business",
	Plural:   "businesses",
	Aliases:  []string{"businesses", "biz"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Business).GetId()) }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Business).GetName() }},
		{Header: "CURRENCY", Value: func(v proto.Message) string { return v.(*avav1.Business).GetCurrencyCode() }},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Business).GetIsActive()) }},
		{Header: "VERSION", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Business).GetResourceVersion()) }},
	},
}

var businessInviteNoun = resource.Noun{
	Singular: "invite",
	Plural:   "invites",
	Columns: []resource.Column{
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
}

func newBusinessCmd() *cobra.Command {
	root := newGroupCmd(businessNoun, "Manage businesses")
	root.AddCommand(newListCmd(businessNoun, listMyBusinesses))
	root.AddCommand(newGetCmd(businessNoun, getBusiness))
	root.AddCommand(newBusinessCreateCmd())
	root.AddCommand(newBusinessUpdateCmd())
	root.AddCommand(newVersionedMutateCmd(businessNoun, "deactivate", "Deactivate a business", deactivateBusiness))
	root.AddCommand(newBusinessInviteCmd())
	return root
}

func getBusiness(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid business id %q: %w", id, err)
	}
	resp, err := avav1.NewBusinessServiceClient(conn).GetBusiness(ctx, &avav1.GetBusinessRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetBusiness(), nil
}

// listMyBusinesses ignores businessID: "list businesses" naturally means
// "list businesses I belong to", not "list businesses scoped to a
// business" (which wouldn't be meaningful for this noun).
func listMyBusinesses(ctx context.Context, conn *grpc.ClientConn, _ int64) ([]proto.Message, error) {
	resp, err := avav1.NewBusinessServiceClient(conn).ListMyBusinesses(ctx, &avav1.ListMyBusinessesRequest{})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetMemberships()))
	for i, m := range resp.GetMemberships() {
		items[i] = m.GetBusiness()
	}
	return items, nil
}

func deactivateBusiness(ctx context.Context, conn *grpc.ClientConn, id string, resourceVersion int64) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid business id %q: %w", id, err)
	}
	resp, err := avav1.NewBusinessServiceClient(conn).DeactivateBusiness(ctx, &avav1.DeactivateBusinessRequest{Id: n, ResourceVersion: resourceVersion})
	if err != nil {
		return nil, err
	}
	return resp.GetBusiness(), nil
}

func newBusinessCreateCmd() *cobra.Command {
	var name, taxID, addr1, addr2, city, state, postal, country, phone, email string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateBusinessRequest{Name: name}
			if taxID != "" {
				req.TaxId = &taxID
			}
			if addr1 != "" {
				req.AddressLine1 = &addr1
			}
			if addr2 != "" {
				req.AddressLine2 = &addr2
			}
			if city != "" {
				req.City = &city
			}
			if state != "" {
				req.State = &state
			}
			if postal != "" {
				req.PostalCode = &postal
			}
			if country != "" {
				req.Country = &country
			}
			if phone != "" {
				req.Phone = &phone
			}
			if email != "" {
				req.Email = &email
			}

			resp, err := avav1.NewBusinessServiceClient(conn).CreateBusiness(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBusiness(), businessNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "business name (required)")
	cmd.Flags().StringVar(&taxID, "tax-id", "", "tax id")
	cmd.Flags().StringVar(&addr1, "address1", "", "address line 1")
	cmd.Flags().StringVar(&addr2, "address2", "", "address line 2")
	cmd.Flags().StringVar(&city, "city", "", "city")
	cmd.Flags().StringVar(&state, "state", "", "state/province")
	cmd.Flags().StringVar(&postal, "postal-code", "", "postal code")
	cmd.Flags().StringVar(&country, "country", "", "country")
	cmd.Flags().StringVar(&phone, "phone", "", "phone number")
	cmd.Flags().StringVar(&email, "email", "", "email address")
	_ = cmd.MarkFlagRequired("name")
	resource.Doc{
		Summary:  "Create a business",
		Detail:   "Requires global-admin.",
		Examples: []resource.Example{{Cmd: `avactl business create --name "Acme Co"`}},
	}.Apply(cmd)
	return cmd
}

func newBusinessUpdateCmd() *cobra.Command {
	var resourceVersion int64
	var name, taxID, addr1, addr2, city, state, postal, country, phone, email string

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid business id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateBusinessRequest{Id: id, ResourceVersion: resourceVersion}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("tax-id") {
				req.TaxId = &taxID
			}
			if cmd.Flags().Changed("address1") {
				req.AddressLine1 = &addr1
			}
			if cmd.Flags().Changed("address2") {
				req.AddressLine2 = &addr2
			}
			if cmd.Flags().Changed("city") {
				req.City = &city
			}
			if cmd.Flags().Changed("state") {
				req.State = &state
			}
			if cmd.Flags().Changed("postal-code") {
				req.PostalCode = &postal
			}
			if cmd.Flags().Changed("country") {
				req.Country = &country
			}
			if cmd.Flags().Changed("phone") {
				req.Phone = &phone
			}
			if cmd.Flags().Changed("email") {
				req.Email = &email
			}

			resp, err := avav1.NewBusinessServiceClient(conn).UpdateBusiness(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetBusiness(), businessNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new business name")
	cmd.Flags().StringVar(&taxID, "tax-id", "", "new tax id")
	cmd.Flags().StringVar(&addr1, "address1", "", "new address line 1")
	cmd.Flags().StringVar(&addr2, "address2", "", "new address line 2")
	cmd.Flags().StringVar(&city, "city", "", "new city")
	cmd.Flags().StringVar(&state, "state", "", "new state/province")
	cmd.Flags().StringVar(&postal, "postal-code", "", "new postal code")
	cmd.Flags().StringVar(&country, "country", "", "new country")
	cmd.Flags().StringVar(&phone, "phone", "", "new phone number")
	cmd.Flags().StringVar(&email, "email", "", "new email address")
	addResourceVersionFlag(cmd, &resourceVersion)
	resource.Doc{
		Summary:  "Update a business's profile",
		Detail:   "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{{Cmd: "avactl business update 1 --phone 555-0100"}},
	}.Apply(cmd)
	return cmd
}

func newBusinessInviteCmd() *cobra.Command {
	root := newGroupCmd(businessInviteNoun, "Invite people into a business")
	root.AddCommand(newListCmd(businessInviteNoun, listBusinessInvites))
	root.AddCommand(newBusinessInviteCreateCmd())
	root.AddCommand(newMutateCmd(businessInviteNoun, "revoke", "Revoke a business invite", revokeBusinessInvite))
	return root
}

// listBusinessInvites is the current context's business - "list invites"
// naturally means "outstanding invites on the business I'm working in".
func listBusinessInvites(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
	resp, err := avav1.NewBusinessServiceClient(conn).ListBusinessInvites(ctx, &avav1.ListBusinessInvitesRequest{BusinessId: businessID})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetInvites()))
	for i, inv := range resp.GetInvites() {
		items[i] = inv
	}
	return items, nil
}

func revokeBusinessInvite(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid invite id %q: %w", id, err)
	}
	resp, err := avav1.NewBusinessServiceClient(conn).RevokeBusinessInvite(ctx, &avav1.RevokeBusinessInviteRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetInvite(), nil
}

func newBusinessInviteCreateCmd() *cobra.Command {
	var email, role string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBusinessServiceClient(conn).CreateBusinessInvite(cmd.Context(), &avav1.CreateBusinessInviteRequest{
				BusinessId: businessID,
				Email:      email,
				Role:       role,
			})
			if err != nil {
				return err
			}

			if flagOutput != output.FormatTable {
				return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetInvite(), businessInviteNoun.Columns)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Invited %s as %s (invite id %d, expires %s)\n",
				email, role, resp.GetInvite().GetId(), resp.GetInvite().GetExpiresAt().AsTime().Format("2006-01-02"))
			fmt.Fprintf(w, "\nToken (shown once - share it with %s yourself):\n%s\n", email, resp.GetToken())
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "invitee's email (required)")
	cmd.Flags().StringVar(&role, "role", "MEMBER", "OWNER, ADMIN, MEMBER, or VIEWER")
	_ = cmd.MarkFlagRequired("email")
	resource.Doc{
		Summary: "Invite someone into the current business",
		Detail: "Requires global-admin, or OWNER/ADMIN on the business itself. Prints a " +
			"one-time token - copy and send it to the invitee yourself (Slack, text, " +
			"email); ava never sends it anywhere, and it's never shown again after this.",
		Examples: []resource.Example{{Cmd: "avactl business invite create --email jane@example.com --role MEMBER"}},
	}.Apply(cmd)
	return cmd
}
