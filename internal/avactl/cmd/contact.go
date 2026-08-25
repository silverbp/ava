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

var contactNoun = resource.Noun{
	Singular: "contact",
	Plural:   "contacts",
	Aliases:  []string{"contacts"},
	Columns: []resource.Column{
		{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Contact).GetId()) }},
		{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Contact).GetContactNumber() }},
		{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Contact).GetName() }},
		{Header: "CUSTOMER", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsCustomer()) }},
		{Header: "VENDOR", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsVendor()) }},
		{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsActive()) }},
	},
}

func newContactCmd() *cobra.Command {
	root := newGroupCmd(contactNoun, "Manage customer/vendor contacts")

	var includeInactive bool
	listCmd := newListCmd(contactNoun, func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
		return listContacts(ctx, conn, businessID, includeInactive)
	})
	listCmd.Flags().BoolVar(&includeInactive, "inactive", false, "also include inactive contacts")
	root.AddCommand(listCmd)

	root.AddCommand(newGetCmd(contactNoun, getContact))
	root.AddCommand(newContactCreateCmd())
	root.AddCommand(newContactUpdateCmd())
	root.AddCommand(newMutateCmd(contactNoun, "deactivate", "Deactivate a contact", deactivateContact))
	return root
}

func getContact(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid contact id %q: %w", id, err)
	}
	resp, err := avav1.NewContactServiceClient(conn).GetContact(ctx, &avav1.GetContactRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetContact(), nil
}

func listContacts(ctx context.Context, conn *grpc.ClientConn, businessID int64, includeInactive bool) ([]proto.Message, error) {
	resp, err := avav1.NewContactServiceClient(conn).ListContacts(ctx, &avav1.ListContactsRequest{BusinessId: businessID, IncludeInactive: includeInactive})
	if err != nil {
		return nil, err
	}
	items := make([]proto.Message, len(resp.GetContacts()))
	for i, c := range resp.GetContacts() {
		items[i] = c
	}
	return items, nil
}

func deactivateContact(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid contact id %q: %w", id, err)
	}
	resp, err := avav1.NewContactServiceClient(conn).DeactivateContact(ctx, &avav1.DeactivateContactRequest{Id: n})
	if err != nil {
		return nil, err
	}
	return resp.GetContact(), nil
}

func newContactCreateCmd() *cobra.Command {
	var contactNumber, name, email, phone string
	var ledgerAccount, paymentTerms int32
	var isCustomer, isVendor bool
	var creditLimit string
	var addr1, addr2, city, state, postal, country string

	cmd := &cobra.Command{
		Use:  "create",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateContactRequest{
				BusinessId:    businessID,
				ContactNumber: contactNumber,
				Name:          name,
				IsCustomer:    isCustomer,
				IsVendor:      isVendor,
			}
			if email != "" {
				req.Email = &email
			}
			if phone != "" {
				req.Phone = &phone
			}
			if ledgerAccount != 0 {
				req.LedgerAccountId = &ledgerAccount
			}
			if paymentTerms != 0 {
				req.PaymentTermsDays = &paymentTerms
			}
			if creditLimit != "" {
				req.CreditLimit = &avav1.Decimal{Value: creditLimit}
			}
			if addr1 != "" {
				req.BillingAddressLine1 = &addr1
			}
			if addr2 != "" {
				req.BillingAddressLine2 = &addr2
			}
			if city != "" {
				req.BillingCity = &city
			}
			if state != "" {
				req.BillingState = &state
			}
			if postal != "" {
				req.BillingPostalCode = &postal
			}
			if country != "" {
				req.BillingCountry = &country
			}

			resp, err := avav1.NewContactServiceClient(conn).CreateContact(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetContact(), contactNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&contactNumber, "contact-number", "", "unique contact number (required)")
	cmd.Flags().StringVar(&name, "name", "", "contact name (required)")
	cmd.Flags().StringVar(&email, "email", "", "email address")
	cmd.Flags().StringVar(&phone, "phone", "", "phone number")
	cmd.Flags().BoolVar(&isCustomer, "customer", true, "this contact is a customer")
	cmd.Flags().BoolVar(&isVendor, "vendor", false, "this contact is a vendor")
	cmd.Flags().Int32Var(&ledgerAccount, "ledger-account", 0, "this contact's AR/AP ledger account id, for posting invoices/payments")
	cmd.Flags().Int32Var(&paymentTerms, "payment-terms", 0, "default payment terms, in days")
	cmd.Flags().StringVar(&creditLimit, "credit-limit", "", "credit limit")
	cmd.Flags().StringVar(&addr1, "address1", "", "billing address line 1")
	cmd.Flags().StringVar(&addr2, "address2", "", "billing address line 2")
	cmd.Flags().StringVar(&city, "city", "", "billing city")
	cmd.Flags().StringVar(&state, "state", "", "billing state/province")
	cmd.Flags().StringVar(&postal, "postal-code", "", "billing postal code")
	cmd.Flags().StringVar(&country, "country", "", "billing country")
	_ = cmd.MarkFlagRequired("contact-number")
	_ = cmd.MarkFlagRequired("name")
	resource.Doc{
		Summary:  "Create a customer/vendor contact",
		Examples: []resource.Example{{Cmd: "avactl contact create --contact-number C-1 --name \"Acme Co\" --ledger-account 12"}},
	}.Apply(cmd)
	return cmd
}

func newContactUpdateCmd() *cobra.Command {
	var name, email, phone string
	var ledgerAccount, paymentTerms int32
	var creditLimit string
	var addr1, addr2, city, state, postal, country string

	cmd := &cobra.Command{
		Use:  "update <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid contact id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.UpdateContactRequest{Id: id}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("email") {
				req.Email = &email
			}
			if cmd.Flags().Changed("phone") {
				req.Phone = &phone
			}
			if cmd.Flags().Changed("ledger-account") {
				req.LedgerAccountId = &ledgerAccount
			}
			if cmd.Flags().Changed("payment-terms") {
				req.PaymentTermsDays = &paymentTerms
			}
			if cmd.Flags().Changed("credit-limit") {
				req.CreditLimit = &avav1.Decimal{Value: creditLimit}
			}
			if cmd.Flags().Changed("address1") {
				req.BillingAddressLine1 = &addr1
			}
			if cmd.Flags().Changed("address2") {
				req.BillingAddressLine2 = &addr2
			}
			if cmd.Flags().Changed("city") {
				req.BillingCity = &city
			}
			if cmd.Flags().Changed("state") {
				req.BillingState = &state
			}
			if cmd.Flags().Changed("postal-code") {
				req.BillingPostalCode = &postal
			}
			if cmd.Flags().Changed("country") {
				req.BillingCountry = &country
			}

			resp, err := avav1.NewContactServiceClient(conn).UpdateContact(cmd.Context(), req)
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetContact(), contactNoun.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new contact name")
	cmd.Flags().StringVar(&email, "email", "", "new email address")
	cmd.Flags().StringVar(&phone, "phone", "", "new phone number")
	cmd.Flags().Int32Var(&ledgerAccount, "ledger-account", 0, "new AR/AP ledger account id")
	cmd.Flags().Int32Var(&paymentTerms, "payment-terms", 0, "new default payment terms, in days")
	cmd.Flags().StringVar(&creditLimit, "credit-limit", "", "new credit limit")
	cmd.Flags().StringVar(&addr1, "address1", "", "new billing address line 1")
	cmd.Flags().StringVar(&addr2, "address2", "", "new billing address line 2")
	cmd.Flags().StringVar(&city, "city", "", "new billing city")
	cmd.Flags().StringVar(&state, "state", "", "new billing state/province")
	cmd.Flags().StringVar(&postal, "postal-code", "", "new billing postal code")
	cmd.Flags().StringVar(&country, "country", "", "new billing country")
	resource.Doc{
		Summary:  "Update a contact",
		Detail:   "Only flags you pass are sent - omit a flag to leave that field unchanged.",
		Examples: []resource.Example{{Cmd: "avactl contact update 5 --phone 555-0100"}},
	}.Apply(cmd)
	return cmd
}
