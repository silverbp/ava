package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/moneypb"
)

// --- EntityContextService ---

type entityContextService struct {
	avav1.UnimplementedEntityContextServiceServer
	store *db.Store
}

func newEntityContextService(store *db.Store) *entityContextService {
	return &entityContextService{store: store}
}

func (s *entityContextService) GetEntityContext(ctx context.Context, req *avav1.GetEntityContextRequest) (*avav1.GetEntityContextResponse, error) {
	ec, err := s.store.Queries.GetEntityContext(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "entity context %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, ec.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	pb, err := entityContextToProto(ec)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting entity context: %v", err)
	}
	return &avav1.GetEntityContextResponse{EntityContext: pb}, nil
}

func (s *entityContextService) ListEntityContext(ctx context.Context, req *avav1.ListEntityContextRequest) (*avav1.ListEntityContextResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListEntityContextForEntity(ctx, sqlcgen.ListEntityContextForEntityParams{
		BusinessID:        req.GetBusinessId(),
		EntityType:        req.GetEntityType(),
		EntityID:          req.GetEntityId(),
		IncludeSuperseded: req.GetIncludeSuperseded(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListEntityContextResponse{}
	for _, ec := range rows {
		pb, err := entityContextToProto(ec)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting entity context: %v", err)
		}
		resp.EntityContexts = append(resp.EntityContexts, pb)
	}
	return resp, nil
}

func (s *entityContextService) CreateEntityContext(ctx context.Context, req *avav1.CreateEntityContextRequest) (*avav1.CreateEntityContextResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetContextType() == "" || req.GetContent() == "" {
		return nil, status.Error(codes.InvalidArgument, "context_type and content are required")
	}
	if err := validateEntityRef(ctx, s.store.Queries, req.GetBusinessId(), req.GetEntityType(), req.GetEntityId()); err != nil {
		return nil, err
	}

	var metadata []byte
	if req.MetadataJson != nil {
		if !json.Valid([]byte(req.GetMetadataJson())) {
			return nil, status.Error(codes.InvalidArgument, "metadata_json is not valid JSON")
		}
		metadata = []byte(req.GetMetadataJson())
	}
	confidence, err := moneypb.ToNumeric(req.Confidence)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid confidence: %v", err)
	}

	var created sqlcgen.EntityContext
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		created, err = q.CreateEntityContext(ctx, sqlcgen.CreateEntityContextParams{
			BusinessID:      req.GetBusinessId(),
			EntityType:      req.GetEntityType(),
			EntityID:        req.GetEntityId(),
			ContextType:     req.GetContextType(),
			Content:         req.GetContent(),
			Metadata:        metadata,
			Source:          req.Source,
			Confidence:      confidence,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}
		for _, oldID := range req.GetSupersedesIds() {
			if err := q.SupersedeEntityContext(ctx, sqlcgen.SupersedeEntityContextParams{ID: oldID, SupersededByID: &created.ID}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, translatePgError(err)
	}

	pb, err := entityContextToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting entity context: %v", err)
	}
	return &avav1.CreateEntityContextResponse{EntityContext: pb}, nil
}

func entityContextToProto(ec sqlcgen.EntityContext) (*avav1.EntityContext, error) {
	confidence, err := moneypb.ToProto(ec.Confidence)
	if err != nil {
		return nil, err
	}
	pb := &avav1.EntityContext{
		Id:              ec.ID,
		BusinessId:      ec.BusinessID,
		EntityType:      ec.EntityType,
		EntityId:        ec.EntityID,
		ContextType:     ec.ContextType,
		Content:         ec.Content,
		Source:          ec.Source,
		Confidence:      confidence,
		SupersededById:  ec.SupersededByID,
		CreatedByUserId: ec.CreatedByUserID,
		CreatedAt:       timestampProto(ec.CreatedAt),
	}
	if len(ec.Metadata) > 0 {
		s := string(ec.Metadata)
		pb.MetadataJson = &s
	}
	return pb, nil
}

// --- AttachmentService ---

type attachmentService struct {
	avav1.UnimplementedAttachmentServiceServer
	store *db.Store
}

func newAttachmentService(store *db.Store) *attachmentService {
	return &attachmentService{store: store}
}

func (s *attachmentService) GetAttachment(ctx context.Context, req *avav1.GetAttachmentRequest) (*avav1.GetAttachmentResponse, error) {
	a, err := s.store.Queries.GetAttachment(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "attachment %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, a.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	return &avav1.GetAttachmentResponse{Attachment: attachmentToProto(a)}, nil
}

func (s *attachmentService) ListAttachments(ctx context.Context, req *avav1.ListAttachmentsRequest) (*avav1.ListAttachmentsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListAttachmentsForEntity(ctx, sqlcgen.ListAttachmentsForEntityParams{
		BusinessID: req.GetBusinessId(),
		EntityType: req.GetEntityType(),
		EntityID:   req.GetEntityId(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListAttachmentsResponse{}
	for _, a := range rows {
		resp.Attachments = append(resp.Attachments, attachmentToProto(a))
	}
	return resp, nil
}

func (s *attachmentService) CreateAttachment(ctx context.Context, req *avav1.CreateAttachmentRequest) (*avav1.CreateAttachmentResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetStorageUrl() == "" {
		return nil, status.Error(codes.InvalidArgument, "storage_url is required")
	}
	if err := validateEntityRef(ctx, s.store.Queries, req.GetBusinessId(), req.GetEntityType(), req.GetEntityId()); err != nil {
		return nil, err
	}

	var displaySequence int32
	if req.DisplaySequence != nil {
		displaySequence = req.GetDisplaySequence()
	}

	created, err := s.store.Queries.CreateAttachment(ctx, sqlcgen.CreateAttachmentParams{
		BusinessID:       req.GetBusinessId(),
		EntityType:       req.GetEntityType(),
		EntityID:         req.GetEntityId(),
		OriginalFilename: req.OriginalFilename,
		StorageUrl:       req.GetStorageUrl(),
		ContentType:      req.ContentType,
		FileSizeBytes:    req.FileSizeBytes,
		DisplaySequence:  displaySequence,
		CreatedByUserID:  &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.CreateAttachmentResponse{Attachment: attachmentToProto(created)}, nil
}

func (s *attachmentService) DeleteAttachment(ctx context.Context, req *avav1.DeleteAttachmentRequest) (*avav1.DeleteAttachmentResponse, error) {
	existing, err := s.store.Queries.GetAttachment(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "attachment %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	deleted, err := s.store.Queries.DeleteAttachment(ctx, req.GetId())
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.DeleteAttachmentResponse{Attachment: attachmentToProto(deleted)}, nil
}

func attachmentToProto(a sqlcgen.Attachment) *avav1.Attachment {
	return &avav1.Attachment{
		Id:               a.ID,
		BusinessId:       a.BusinessID,
		EntityType:       a.EntityType,
		EntityId:         a.EntityID,
		OriginalFilename: a.OriginalFilename,
		StorageUrl:       a.StorageUrl,
		ContentType:      a.ContentType,
		FileSizeBytes:    a.FileSizeBytes,
		DisplaySequence:  a.DisplaySequence,
		CreatedByUserId:  a.CreatedByUserID,
		CreatedAt:        timestampProto(a.CreatedAt),
	}
}
