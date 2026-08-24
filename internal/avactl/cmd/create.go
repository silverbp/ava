// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"github.com/spf13/cobra"
)

// newCreateCmd is the `create` parent. Unlike `get`/`delete`, each resource
// gets its own subcommand with its own flags (like kubectl's `create
// deployment`/`create secret`) rather than a generic dispatch — a
// composite write like posting a double-entry transaction doesn't fit a
// generic flag-to-field mapping.
func newCreateCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "create",
		Short: "Create a resource",
	}
	root.AddCommand(newCreateLedgerAccountCmd())
	root.AddCommand(newCreateLedgerTransactionCmd())
	root.AddCommand(newCreateContactCmd())
	root.AddCommand(newCreateServiceCmd())
	root.AddCommand(newCreateTaxRateCmd())
	root.AddCommand(newCreateEstimateCmd())
	root.AddCommand(newCreateInvoiceCmd())
	root.AddCommand(newCreatePaymentCmd())
	root.AddCommand(newCreateBankStatementCmd())
	root.AddCommand(newCreateEntityContextCmd())
	root.AddCommand(newCreateAttachmentCmd())
	root.AddCommand(newCreateBusinessInviteCmd())
	return root
}
