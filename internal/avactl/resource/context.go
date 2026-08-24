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
		Name:    "entity-context",
		Aliases: []string{"entity-contexts", "ctx"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.EntityContext).GetId()) }},
			{Header: "ENTITY", Value: func(v proto.Message) string {
				ec := v.(*avav1.EntityContext)
				return fmt.Sprintf("%s/%d", ec.GetEntityType(), ec.GetEntityId())
			}},
			{Header: "TYPE", Value: func(v proto.Message) string { return v.(*avav1.EntityContext).GetContextType() }},
			{Header: "CONTENT", Value: func(v proto.Message) string { return v.(*avav1.EntityContext).GetContent() }},
			{Header: "SUPERSEDED", Value: func(v proto.Message) string {
				return fmt.Sprintf("%v", v.(*avav1.EntityContext).SupersededById != nil)
			}},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid entity-context id %q: %w", id, err)
			}
			resp, err := avav1.NewEntityContextServiceClient(conn).GetEntityContext(ctx, &avav1.GetEntityContextRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetEntityContext(), nil
		},
		// No List: entity_context is always scoped to one entity_type +
		// entity_id, not a business-wide listing — see `avactl context list`.
	})

	Register(&Resource{
		Name:    "attachment",
		Aliases: []string{"attachments"},
		Columns: []Column{
			{Header: "ID", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Attachment).GetId()) }},
			{Header: "ENTITY", Value: func(v proto.Message) string {
				a := v.(*avav1.Attachment)
				return fmt.Sprintf("%s/%d", a.GetEntityType(), a.GetEntityId())
			}},
			{Header: "FILENAME", Value: func(v proto.Message) string { return v.(*avav1.Attachment).GetOriginalFilename() }},
			{Header: "SIZE", Value: func(v proto.Message) string { return fmt.Sprintf("%d", v.(*avav1.Attachment).GetFileSizeBytes()) }},
			{Header: "CONTENT-TYPE", Value: func(v proto.Message) string { return v.(*avav1.Attachment).GetContentType() }},
		},
		Get: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid attachment id %q: %w", id, err)
			}
			resp, err := avav1.NewAttachmentServiceClient(conn).GetAttachment(ctx, &avav1.GetAttachmentRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetAttachment(), nil
		},
		Delete: func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error) {
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid attachment id %q: %w", id, err)
			}
			resp, err := avav1.NewAttachmentServiceClient(conn).DeleteAttachment(ctx, &avav1.DeleteAttachmentRequest{Id: n})
			if err != nil {
				return nil, err
			}
			return resp.GetAttachment(), nil
		},
		// No List: same reasoning as entity-context — see `avactl context list`.
	})
}
