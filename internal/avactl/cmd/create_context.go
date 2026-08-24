package cmd

import (
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
	var entityType, url, filename, contentType string
	var entityID, size int64

	cmd := &cobra.Command{
		Use:   "attachment",
		Short: "Attach a file reference to any entity",
		Long: `Register a reference to a file already hosted elsewhere (this project has
no object-storage integration of its own) against any entity.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateAttachmentRequest{
				BusinessId: businessID,
				EntityType: entityType,
				EntityId:   entityID,
				StorageUrl: url,
			}
			if filename != "" {
				req.OriginalFilename = &filename
			}
			if contentType != "" {
				req.ContentType = &contentType
			}
			if size != 0 {
				req.FileSizeBytes = &size
			}

			resp, err := avav1.NewAttachmentServiceClient(conn).CreateAttachment(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("attachment")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetAttachment(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&entityType, "entity-type", "", "target entity type, e.g. invoice, contact (required)")
	cmd.Flags().Int64Var(&entityID, "entity-id", 0, "target entity id (required)")
	cmd.Flags().StringVar(&url, "url", "", "storage URL of the already-hosted file (required)")
	cmd.Flags().StringVar(&filename, "filename", "", "original filename")
	cmd.Flags().StringVar(&contentType, "content-type", "", "MIME content type")
	cmd.Flags().Int64Var(&size, "size", 0, "file size, in bytes")
	_ = cmd.MarkFlagRequired("entity-type")
	_ = cmd.MarkFlagRequired("entity-id")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
