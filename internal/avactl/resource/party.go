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
		Name:    "contact",
		Aliases: []string{"contacts"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Contact).GetId()) }},
			{Header: "NUMBER", Value: func(v proto.Message) string { return v.(*avav1.Contact).GetContactNumber() }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Contact).GetName() }},
			{Header: "CUSTOMER", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsCustomer()) }},
			{Header: "VENDOR", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsVendor()) }},
			{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Contact).GetIsActive()) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid contact id %q: %w", id, err)
			}
			resp, err := avav1.NewContactServiceClient(conn).GetContact(ctx, &avav1.GetContactRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetContact(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewContactServiceClient(conn).ListContacts(ctx, &avav1.ListContactsRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetContacts()))
			for i, c := range resp.GetContacts() {
				items[i] = c
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid contact id %q: %w", id, err)
			}
			resp, err := avav1.NewContactServiceClient(conn).DeactivateContact(ctx, &avav1.DeactivateContactRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetContact(), nil
		},
	})

	Register(&Resource{
		Name:    "service",
		Aliases: []string{"services", "svc"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Service).GetId()) }},
			{Header: "CODE", Value: func(v proto.Message) string { return v.(*avav1.Service).GetServiceCode() }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.Service).GetName() }},
			{Header: "PRICE", Value: func(v proto.Message) string { return v.(*avav1.Service).GetRetailPrice().GetValue() }},
			{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.Service).GetIsActive()) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid service id %q: %w", id, err)
			}
			resp, err := avav1.NewServiceCatalogServiceClient(conn).GetService(ctx, &avav1.GetServiceRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetService(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewServiceCatalogServiceClient(conn).ListServices(ctx, &avav1.ListServicesRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetServices()))
			for i, svc := range resp.GetServices() {
				items[i] = svc
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid service id %q: %w", id, err)
			}
			resp, err := avav1.NewServiceCatalogServiceClient(conn).DeactivateService(ctx, &avav1.DeactivateServiceRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetService(), nil
		},
	})

	Register(&Resource{
		Name:    "tax-rate",
		Aliases: []string{"tax-rates"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.TaxRate).GetId()) }},
			{Header: "NAME", Value: func(v proto.Message) string { return v.(*avav1.TaxRate).GetName() }},
			{Header: "RATE", Value: func(v proto.Message) string { return v.(*avav1.TaxRate).GetRate().GetValue() }},
			{Header: "LIABILITY_ACCOUNT", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.TaxRate).GetTaxLiabilityAccountId()) }},
			{Header: "ACTIVE", Value: func(v proto.Message) string { return fmt.Sprintf("%v", v.(*avav1.TaxRate).GetIsActive()) }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid tax-rate id %q: %w", id, err)
			}
			resp, err := avav1.NewTaxRateServiceClient(conn).GetTaxRate(ctx, &avav1.GetTaxRateRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetTaxRate(), nil
		},
		List: func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error) {
			resp, err := avav1.NewTaxRateServiceClient(conn).ListTaxRates(ctx, &avav1.ListTaxRatesRequest{BusinessId: businessID})
			if err != nil {
				return nil, err
			}
			items := make([]proto.Message, len(resp.GetTaxRates()))
			for i, tr := range resp.GetTaxRates() {
				items[i] = tr
			}
			return items, nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid tax-rate id %q: %w", id, err)
			}
			resp, err := avav1.NewTaxRateServiceClient(conn).DeactivateTaxRate(ctx, &avav1.DeactivateTaxRateRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetTaxRate(), nil
		},
	})
}
