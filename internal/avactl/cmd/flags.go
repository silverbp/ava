// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	typepb "google.golang.org/genproto/googleapis/type/date"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

// addResourceVersionFlag registers the optimistic-concurrency flag every
// update/deactivate/status verb takes - the same wording everywhere, so
// `--help` reads identically across nouns. 0 (the default) sends no
// precondition; see Business.resource_version in proto/ava/v1/business.proto.
func addResourceVersionFlag(cmd *cobra.Command, dst *int64) {
	cmd.Flags().Int64Var(dst, "resource-version", 0,
		"only apply if the resource is still at this resource_version (the VERSION column of get/list); omit to write unconditionally")
}

// parseDateFlag parses a YYYY-MM-DD flag value into a google.type.Date.
func parseDateFlag(s string) (*typepb.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", s)
	}
	return &typepb.Date{Year: int32(t.Year()), Month: int32(t.Month()), Day: int32(t.Day())}, nil
}

// formatDate renders a google.type.Date back to YYYY-MM-DD for table
// output.
func formatDate(d *typepb.Date) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
}

// lineFlagKeys is every key a --line accepts, for estimate and invoice alike. item is
// required (every line references a catalog item - no free-text lines); the rest override
// that item's defaults. There is deliberately no account= key: an invoice line always posts
// to its item's default_ledger_account_id.
var lineFlagKeys = map[string]bool{
	"item": true, "desc": true, "qty": true, "price": true, "taxable": true, "tax-rate": true,
}

// lineFlagHelp is the shared --line usage string for estimate/invoice create and update-lines.
const lineFlagHelp = "item=<id>[,desc=...][,qty=...][,price=...][,taxable][,tax-rate=<id>] (repeatable) - desc/price/taxable/tax-rate default from the item when omitted"

// parseLineFlags parses repeatable --line "key=value,key=value" flags,
// shared by estimate/invoice create and update-lines, into ordered field
// maps (line_number is 1-based position in the flag list). Unknown keys are
// an error rather than silently dropped, so a stale account=<id> fails loudly.
func parseLineFlags(raw []string) ([]map[string]string, error) {
	lines := make([]map[string]string, 0, len(raw))
	for _, r := range raw {
		fields := map[string]string{}
		for _, part := range strings.Split(r, ",") {
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if !lineFlagKeys[kv[0]] {
				return nil, fmt.Errorf("invalid --line %q: unknown key %q (want one of item, desc, qty, price, taxable, tax-rate)", r, kv[0])
			}
			if len(kv) == 1 {
				fields[kv[0]] = "true" // bare flag, e.g. "taxable"
				continue
			}
			fields[kv[0]] = kv[1]
		}
		if _, ok := fields["item"]; !ok {
			return nil, fmt.Errorf("invalid --line %q: missing item=<id> (every line must reference a catalog item)", r)
		}
		lines = append(lines, fields)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("at least one --line is required")
	}
	return lines, nil
}

// parseRequiredInt64 is parseOptionalInt64 for a key parseLineFlags has
// already guaranteed is present (item).
func parseRequiredInt64(fields map[string]string, key string) (int64, error) {
	n, err := parseOptionalInt64(fields, key)
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, fmt.Errorf("%s= is required", key)
	}
	return *n, nil
}

func parseOptionalInt64(fields map[string]string, key string) (*int64, error) {
	v, ok := fields[key]
	if !ok {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return &n, nil
}

func parseDecimalField(fields map[string]string, key string) *avav1.Decimal {
	v, ok := fields[key]
	if !ok {
		return nil
	}
	return &avav1.Decimal{Value: v}
}

// parseOptionalBool distinguishes "not set at all" (nil - an item's own
// default applies, if the line has one) from an explicit true/false.
func parseOptionalBool(fields map[string]string, key string) *bool {
	v, ok := fields[key]
	if !ok {
		return nil
	}
	b := v == "true"
	return &b
}

// parseEntryFlags parses repeatable --entry "account=<id>,debit=<amt>" or
// "account=<id>,credit=<amt>" flags for `ledger-transaction post`.
func parseEntryFlags(raw []string) ([]*avav1.NewLedgerEntry, error) {
	var entries []*avav1.NewLedgerEntry
	for _, r := range raw {
		fields := map[string]string{}
		for _, part := range strings.Split(r, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid --entry %q: expected comma-separated key=value pairs", r)
			}
			fields[kv[0]] = kv[1]
		}

		accountStr, ok := fields["account"]
		if !ok {
			return nil, fmt.Errorf("invalid --entry %q: missing account=", r)
		}
		accountID, err := strconv.ParseInt(accountStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid --entry %q: account must be an integer", r)
		}

		ne := &avav1.NewLedgerEntry{AccountId: int32(accountID)}
		if debit, ok := fields["debit"]; ok {
			ne.DebitAmount = &avav1.Decimal{Value: debit}
		}
		if credit, ok := fields["credit"]; ok {
			ne.CreditAmount = &avav1.Decimal{Value: credit}
		}
		entries = append(entries, ne)
	}
	return entries, nil
}

// parsePaymentApplyFlags parses repeatable --apply "invoice_id:amount"
// flags into PaymentApplicationInput values.
func parsePaymentApplyFlags(raw []string) ([]*avav1.PaymentApplicationInput, error) {
	applications := make([]*avav1.PaymentApplicationInput, 0, len(raw))
	for _, r := range raw {
		invoiceID, amount, ok := strings.Cut(r, ":")
		if !ok {
			return nil, fmt.Errorf("invalid --apply %q, want invoice_id:amount", r)
		}
		id, err := strconv.ParseInt(invoiceID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --apply %q: invoice_id: %w", r, err)
		}
		applications = append(applications, &avav1.PaymentApplicationInput{
			InvoiceId:     id,
			AppliedAmount: &avav1.Decimal{Value: amount},
		})
	}
	return applications, nil
}
