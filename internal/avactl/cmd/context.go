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
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

// newContextCmd is the `context` parent — entity_context/attachment are
// always scoped to one entity_type + entity_id, not a business-wide
// listing the way every other noun is, so they stay grouped under one
// command rather than becoming two separate noun groups.
func newContextCmd() *cobra.Command {
	root := newGroupCmd(resource.Noun{Singular: "context"}, "Manage AI/user context and attachments for an entity")
	root.AddCommand(newContextListCmd())
	root.AddCommand(newContextGetCmd())
	root.AddCommand(newContextGetAttachmentCmd())
	root.AddCommand(newContextNoteCmd())
	root.AddCommand(newContextAttachCmd())
	root.AddCommand(newContextDownloadCmd())
	root.AddCommand(newContextRemoveAttachmentCmd())
	return root
}

func newContextGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "get <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid entity-context id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewEntityContextServiceClient(conn).GetEntityContext(cmd.Context(), &avav1.GetEntityContextRequest{Id: n})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEntityContext(), nil)
		},
	}
	resource.Doc{
		Summary:  "Get one entity-context row by id",
		Examples: []resource.Example{{Cmd: "avactl context get 7"}},
	}.Apply(cmd)
	return cmd
}

func newContextGetAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "get-attachment <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid attachment id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewAttachmentServiceClient(conn).GetAttachment(cmd.Context(), &avav1.GetAttachmentRequest{Id: n})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAttachment(), nil)
		},
	}
	resource.Doc{
		Summary:  "Get one attachment's metadata by id",
		Detail:   "Metadata only - use `context download` to read its file content.",
		Examples: []resource.Example{{Cmd: "avactl context get-attachment 9"}},
	}.Apply(cmd)
	return cmd
}

func newContextRemoveAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "remove-attachment <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid attachment id %q: %w", args[0], err)
			}
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewAttachmentServiceClient(conn).DeleteAttachment(cmd.Context(), &avav1.DeleteAttachmentRequest{Id: n})
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAttachment(), nil)
		},
	}
	resource.Doc{
		Summary:  "Delete an attachment",
		Examples: []resource.Example{{Cmd: "avactl context remove-attachment 9"}},
	}.Apply(cmd)
	return cmd
}

func newContextDownloadCmd() *cobra.Command {
	var id int64
	var out string

	cmd := &cobra.Command{
		Use:  "download",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			stream, err := avav1.NewAttachmentServiceClient(conn).DownloadAttachment(cmd.Context(), &avav1.DownloadAttachmentRequest{Id: id})
			if err != nil {
				return err
			}

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("creating %s: %w", out, err)
			}
			defer f.Close()

			for {
				msg, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				if _, err := f.Write(msg.GetChunk()); err != nil {
					return fmt.Errorf("writing %s: %w", out, err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "downloaded attachment %d to %s\n", id, out)
			return nil
		},
	}
	cmd.Flags().Int64Var(&id, "id", 0, "attachment id (required)")
	cmd.Flags().StringVar(&out, "out", "", "local path to write the file to (required)")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("out")
	resource.Doc{
		Summary: "Download an attachment's file content",
		Detail: "Streams the attachment's bytes through AttachmentService.DownloadAttachment - " +
			"the only way to read a file's content; ava never hands out a direct storage URL.",
		Examples: []resource.Example{{Cmd: "avactl context download --id 9 --out invoice.pdf"}},
	}.Apply(cmd)
	return cmd
}

func newContextNoteCmd() *cobra.Command {
	var entityType, contextType, content, metadataJSON, source, confidence string
	var entityID int64
	var supersedesRaw []string

	cmd := &cobra.Command{
		Use:  "note",
		Args: cobra.NoArgs,
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
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetEntityContext(), nil)
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
	resource.Doc{
		Summary:  "Attach AI-generated or user context to any entity",
		Examples: []resource.Example{{Cmd: `avactl context note --entity-type invoice --entity-id 42 --content "customer requested a discount"`}},
	}.Apply(cmd)
	return cmd
}

func newContextAttachCmd() *cobra.Command {
	var entityType, path, filename, contentType string
	var entityID int64

	cmd := &cobra.Command{
		Use:  "attach",
		Args: cobra.NoArgs,
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
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAttachment(), nil)
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
	resource.Doc{
		Summary: "Upload a file and attach it to any entity",
		Detail: "Streams a local file's bytes to ava through AttachmentService.UploadAttachment, " +
			"which stores it in ava's own object-storage backend - not a caller-supplied URL.",
		Examples: []resource.Example{{Cmd: "avactl context attach --entity-type invoice --entity-id 42 --file receipt.pdf"}},
	}.Apply(cmd)
	return cmd
}

func newContextListCmd() *cobra.Command {
	var entityType string
	var entityID int64
	var includeSuperseded bool

	cmd := &cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			ctx := cmd.Context()

			ctxResp, err := avav1.NewEntityContextServiceClient(conn).ListEntityContext(ctx, &avav1.ListEntityContextRequest{
				BusinessId:        businessID,
				EntityType:        entityType,
				EntityId:          entityID,
				IncludeSuperseded: includeSuperseded,
			})
			if err != nil {
				return err
			}
			attResp, err := avav1.NewAttachmentServiceClient(conn).ListAttachments(ctx, &avav1.ListAttachmentsRequest{
				BusinessId: businessID,
				EntityType: entityType,
				EntityId:   entityID,
			})
			if err != nil {
				return err
			}

			if flagOutput != output.FormatTable {
				w := cmd.OutOrStdout()
				fmt.Fprintln(w, "--- entity_context ---")
				ctxItems := make([]proto.Message, len(ctxResp.GetEntityContexts()))
				for i, ec := range ctxResp.GetEntityContexts() {
					ctxItems[i] = ec
				}
				if err := output.PrintList(w, flagOutput, ctxItems, nil); err != nil {
					return err
				}
				fmt.Fprintln(w, "--- attachments ---")
				attItems := make([]proto.Message, len(attResp.GetAttachments()))
				for i, a := range attResp.GetAttachments() {
					attItems[i] = a
				}
				return output.PrintList(w, flagOutput, attItems, nil)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "CONTEXT")
			fmt.Fprintln(w, "ID\tTYPE\tCONTENT\tSUPERSEDED")
			for _, ec := range ctxResp.GetEntityContexts() {
				fmt.Fprintf(w, "%d\t%s\t%s\t%v\n", ec.GetId(), ec.GetContextType(), ec.GetContent(), ec.SupersededById != nil)
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, "ATTACHMENTS")
			fmt.Fprintln(w, "ID\tFILENAME\tSIZE\tCONTENT-TYPE")
			for _, a := range attResp.GetAttachments() {
				fmt.Fprintf(w, "%d\t%s\t%d\t%s\n", a.GetId(), a.GetOriginalFilename(), a.GetFileSizeBytes(), a.GetContentType())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "target entity type, e.g. invoice, contact (required)")
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, "target entity id (required)")
	cmd.Flags().BoolVar(&includeSuperseded, "include-superseded", false, "include entity-context rows that have been superseded")
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("entity-id")
	resource.Doc{
		Summary:  "List entity-context and attachments for one entity",
		Examples: []resource.Example{{Cmd: "avactl context list --entity-type invoice --entity-id 42"}},
	}.Apply(cmd)
	return cmd
}
