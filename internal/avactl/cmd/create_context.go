// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newCreateEntityContextCmd() *cobra.Command {
	var entityType, contextType, content, metadataJSON, source, confidence string
	var entityID int64
	var supersedesRaw []string

	cmd := &cobra.Command{
		Use:   "entity-context",
		Short: "Attach AI-generated or user context to any entity",
		RunE: func(cmd *cobra.Command, args []string) error {
			var supersedes []int64
			for _, s := range supersedesRaw {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return err
				}
				supersedes = append(supersedes, n)
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateEntityContextRequest{
				BusinessId:    businessID,
				EntityType:    entityType,
				EntityId:      entityID,
				ContextType:   contextType,
				Content:       content,
				SupersedesIds: supersedes,
			}
			if metadataJSON != "" {
				req.MetadataJson = &metadataJSON
			}
			if source != "" {
				req.Source = &source
			}
			if confidence != "" {
				req.Confidence = &avav1.Decimal{Value: confidence}
			}

			resp, err := avav1.NewEntityContextServiceClient(conn).CreateEntityContext(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("entity-context")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEntityContext(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "target entity type, e.g. invoice, contact, ledger_transaction (required)")
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, "target entity id (required)")
	cmd.Flags().StringVar(&contextType, "context-type", "user_note", "summary, categorization_hint, anomaly, or user_note")
	cmd.Flags().StringVar(&content, "content", "", "context content (required)")
	cmd.Flags().StringVar(&metadataJSON, "metadata", "", "arbitrary metadata, as a JSON object string")
	cmd.Flags().StringVar(&source, "source", "", "source label, e.g. an MCP session id")
	cmd.Flags().StringVar(&confidence, "confidence", "", "confidence, e.g. 0.9")
	cmd.Flags().StringArrayVar(&supersedesRaw, "supersedes", nil, "id of an older entity-context row this one rolls up (repeatable)")
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("entity-id")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func newCreateAttachmentCmd() *cobra.Command {
	var entityType, path, filename, contentType string
	var entityID int64

	cmd := &cobra.Command{
		Use:   "attachment",
		Short: "Upload a file and attach it to any entity",
		Long: `Streams a local file's bytes to ava through AttachmentService.UploadAttachment,
which stores it in ava's own object-storage backend - not a caller-supplied URL.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("opening %s: %w", path, err)
			}
			defer f.Close()

			if filename == "" {
				filename = filepath.Base(path)
			}

			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			stream, err := avav1.NewAttachmentServiceClient(conn).UploadAttachment(cmd.Context())
			if err != nil {
				return err
			}

			meta := &avav1.UploadAttachmentMetadata{
				BusinessId:       businessID,
				EntityType:       entityType,
				EntityId:         entityID,
				OriginalFilename: &filename,
			}
			if contentType != "" {
				meta.ContentType = &contentType
			}
			if err := stream.Send(&avav1.UploadAttachmentRequest{Data: &avav1.UploadAttachmentRequest_Metadata{Metadata: meta}}); err != nil {
				return err
			}

			buf := make([]byte, 256*1024)
			for {
				n, readErr := f.Read(buf)
				if n > 0 {
					if sendErr := stream.Send(&avav1.UploadAttachmentRequest{Data: &avav1.UploadAttachmentRequest_Chunk{Chunk: buf[:n]}}); sendErr != nil {
						return sendErr
					}
				}
				if errors.Is(readErr, io.EOF) {
					break
				}
				if readErr != nil {
					return fmt.Errorf("reading %s: %w", path, readErr)
				}
			}

			resp, err := stream.CloseAndRecv()
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("attachment")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAttachment(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "target entity type, e.g. invoice, contact (required)")
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, "target entity id (required)")
	cmd.Flags().StringVar(&path, "file", "", "local path of the file to upload (required)")
	cmd.Flags().StringVar(&filename, "filename", "", "original filename (default: the uploaded file's base name)")
	cmd.Flags().StringVar(&contentType, "content-type", "", "MIME content type")
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("entity-id")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
