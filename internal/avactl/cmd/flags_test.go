// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
)

func TestParseLineFlags_ItemRequired(t *testing.T) {
	_, err := parseLineFlags([]string{"desc=Free text,price=10"})
	if err == nil || !strings.Contains(err.Error(), "missing item=") {
		t.Fatalf("free-text line should be rejected, got %v", err)
	}
}

func TestParseLineFlags_UnknownKeyRejected(t *testing.T) {
	_, err := parseLineFlags([]string{"item=71,account=40"})
	if err == nil || !strings.Contains(err.Error(), `unknown key "account"`) {
		t.Fatalf("account= should be rejected as unknown, got %v", err)
	}
}

func TestParseLineFlags_DescOptionalAndBareTaxable(t *testing.T) {
	lines, err := parseLineFlags([]string{"item=71,qty=10,taxable", "item=72,desc=Custom,tax-rate=1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0]["taxable"] != "true" {
		t.Errorf("bare taxable should parse as true, got %q", lines[0]["taxable"])
	}
	if _, ok := lines[0]["desc"]; ok {
		t.Error("desc should be absent when not given (server defaults it from the item)")
	}
	if lines[1]["desc"] != "Custom" {
		t.Errorf("desc = %q, want Custom", lines[1]["desc"])
	}
}

func TestParseLineFlags_AtLeastOne(t *testing.T) {
	if _, err := parseLineFlags(nil); err == nil {
		t.Fatal("no --line should be an error")
	}
}

func TestNewInvoiceLineItems_MapsItemAndLineNumber(t *testing.T) {
	fields, err := parseLineFlags([]string{"item=71,qty=10", "item=72,price=5.00,tax-rate=3"})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := newInvoiceLineItems(fields)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].GetItemId() != 71 || lines[0].GetLineNumber() != 1 || lines[0].GetQuantity().GetValue() != "10" {
		t.Errorf("line 0 = %+v", lines[0])
	}
	if lines[1].GetItemId() != 72 || lines[1].GetLineNumber() != 2 || lines[1].GetTaxRateId() != 3 || lines[1].GetUnitPrice().GetValue() != "5.00" {
		t.Errorf("line 1 = %+v", lines[1])
	}
	if _, err := newInvoiceLineItems([]map[string]string{{"item": "abc"}}); err == nil {
		t.Error("non-integer item= should error")
	}
}
