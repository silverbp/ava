// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package server

import (
	"testing"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

// TestDeactivateContact_CustomerLedgerAccount checks the conditional side
// effect on contact deactivation: a customer's own AR sub-account follows
// it into inactive only once that account carries no balance.
func TestDeactivateContact_CustomerLedgerAccount(t *testing.T) {
	defer failOnPanic(t)
	tc := newTenant(t)
	ctx := tc.ctx

	contacts := newContactService(tc.store)
	invoices := newInvoiceService(tc.store)
	accounts := newLedgerAccountService(tc.store)

	contactID, itemID := tc.customerAndItem()
	custAR := must(contacts.GetContact(ctx, &avav1.GetContactRequest{Id: contactID})).GetContact().GetCustomer().GetLedgerAccountId()

	inv := must(invoices.CreateInvoice(ctx, &avav1.CreateInvoiceRequest{
		BusinessId: tc.businessID, ContactId: contactID, InvoiceType: "SALES", InvoiceDate: dateOf(2026, 1, 1), DueDate: dateOf(2026, 1, 31),
		LineItems: []*avav1.NewDocumentLineItem{{ItemId: itemID, LineNumber: 1}},
	})).GetInvoice()

	// A customer with an outstanding invoice balance keeps its AR account
	// active even once the contact is deactivated.
	must(contacts.DeactivateContact(ctx, &avav1.DeactivateContactRequest{Id: contactID}))
	acct := must(accounts.GetLedgerAccount(ctx, &avav1.GetLedgerAccountRequest{Id: custAR})).GetAccount()
	if !acct.GetIsActive() {
		t.Fatalf("customer AR account went inactive with a non-zero balance")
	}

	// Once the invoice is cancelled (balance back to zero), the same
	// deactivate call also deactivates the now-zero-balance account.
	must(invoices.UpdateInvoiceStatus(ctx, &avav1.UpdateInvoiceStatusRequest{Id: inv.GetId(), Status: "CANCELLED"}))
	must(contacts.DeactivateContact(ctx, &avav1.DeactivateContactRequest{Id: contactID}))
	acct = must(accounts.GetLedgerAccount(ctx, &avav1.GetLedgerAccountRequest{Id: custAR})).GetAccount()
	if acct.GetIsActive() {
		t.Fatalf("customer AR account should have gone inactive at zero balance")
	}
}
