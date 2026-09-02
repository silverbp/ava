// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Optimistic concurrency, Kubernetes-style. Every mutable resource carries a
// resource_version the database bumps on each committed write to its row
// (the bump_resource_version trigger in migrations/00001_initial.up.sql);
// a client that read version N and sends it back on an update only wins if
// the row is still at N. The check itself is in SQL (`AND resource_version
// = $expected` on every Update*/Deactivate* query) so it's atomic with the
// write - these two helpers just shape the request field going in and the
// no-row-matched result coming out.

// expectedResourceVersion turns a request's resource_version into the
// nullable precondition the update queries take: 0 (unset) means
// unconditional, like an empty resourceVersion in k8s.
func expectedResourceVersion(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// translateUpdateError is translatePgError plus the one outcome an
// optimistic-concurrency UPDATE adds: no row matched. Every update handler
// reads the row first (for the business-role check), so if the UPDATE then
// matches nothing, the row changed - or was soft-deleted - between that
// read and this write. With a precondition that's ABORTED (the gRPC code
// for a failed compare-and-swap; k8s would say 409 Conflict): nothing was
// written, the caller re-reads and retries. Without one, the only way to
// miss is the row disappearing, so NotFound.
func translateUpdateError(err error, kind string, id int64, expected int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		if expected != 0 {
			return status.Errorf(codes.Aborted,
				"%s %d has been modified since resource_version %d; re-read it and apply your changes to the latest version",
				kind, id, expected)
		}
		return status.Errorf(codes.NotFound, "%s %d not found", kind, id)
	}
	return translatePgError(err)
}
