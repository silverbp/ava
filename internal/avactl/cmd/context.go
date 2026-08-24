package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
)

// newContextCmd is the `context` parent — entity_context/attachment are
// always scoped to one entity_type + entity_id, not a business-wide
// listing, so (unlike most resources) they don't fit the generic `get`
// registry's List and get their own query command.
func newContextCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "context",
		Short: "List AI/user context and attachments for an entity",
	}
	root.AddCommand(newContextListCmd())
	root.AddCommand(newContextDownloadAttachmentCmd())
	return root
}

func newContextDownloadAttachmentCmd() *cobra.Command {
	var id int64
	var out string

	cmd := &cobra.Command{
		Use:   "download-attachment",
		Short: "Download an attachment's file content",
		Long: `Streams an attachment's bytes through AttachmentService.DownloadAttachment -
the only way to read a file's content; ava never hands out a direct storage URL.`,
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
	return cmd
}

func newContextListCmd() *cobra.Command {
	var entityType string
	var entityID int64
	var includeSuperseded bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List entity-context and attachments for one entity",
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
	return cmd
}
