// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Package resource is avactl's registry of resource types — the generic
// `get`/`delete` commands dispatch through it by resource name, the same
// way kubectl's RESTMapper resolves "pods" to the Pod type. `create` stays
// resource-specific (its own subcommand with its own flags per type,
// matching how kubectl itself does `create deployment`/`create secret`)
// and isn't part of this registry.
package resource

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// Column renders one field of a resource for table (-o table) output.
type Column struct {
	Header string
	Value  func(v proto.Message) string
}

type GetFunc func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error)
type ListFunc func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error)
type DeleteFunc func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error)
type GetPdfFunc func(ctx context.Context, conn *grpc.ClientConn, id string) ([]byte, error)

type Resource struct {
	Name    string
	Aliases []string
	Columns []Column

	// Get and List back `avactl get`. List may be nil for a resource with
	// no natural business-scoped listing.
	Get  GetFunc
	List ListFunc
	// Delete backs `avactl delete`. May be nil for a resource with no
	// delete/deactivate RPC yet; the delete command returns the
	// (deactivated) object so the caller sees the result.
	Delete DeleteFunc
	// GetPdf backs `avactl get <resource> <id> -o pdf`. Nil for a
	// resource with no PDF rendering.
	GetPdf GetPdfFunc
}

var registry = map[string]*Resource{}

func Register(r *Resource) {
	registry[r.Name] = r
	for _, a := range r.Aliases {
		registry[a] = r
	}
}

func Lookup(name string) (*Resource, bool) {
	r, ok := registry[name]
	return r, ok
}
