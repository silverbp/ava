package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newCreateBusinessInviteCmd() *cobra.Command {
	var email, role string

	cmd := &cobra.Command{
		Use:   "business-invite",
		Short: "Invite someone into the current business",
		Long: `Requires global-admin, or OWNER/ADMIN on the business itself. Prints a
one-time token — copy and send it to the invitee yourself (Slack, text, email);
ava never sends it anywhere, and it's never shown again after this.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBusinessServiceClient(conn).CreateBusinessInvite(cmd.Context(), &avav1.CreateBusinessInviteRequest{
				BusinessId: businessID,
				Email:      email,
				Role:       role,
			})
			if err != nil {
				return err
			}

			if flagOutput != output.FormatTable {
				res, _ := resource.Lookup("business-invite")
				return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetInvite(), res.Columns)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Invited %s as %s (invite id %d, expires %s)\n",
				email, role, resp.GetInvite().GetId(), resp.GetInvite().GetExpiresAt().AsTime().Format("2006-01-02"))
			fmt.Fprintf(w, "\nToken (shown once — share it with %s yourself):\n%s\n", email, resp.GetToken())
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "invitee's email (required)")
	cmd.Flags().StringVar(&role, "role", "MEMBER", "OWNER, ADMIN, MEMBER, or VIEWER")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func newAcceptInviteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accept-invite <token>",
		Short: "Accept a business invite",
		Long: `Redeems a business-invite token — you must already be signed in
("avactl login") as the email the invite was sent to.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewBusinessServiceClient(conn).AcceptBusinessInvite(cmd.Context(), &avav1.AcceptBusinessInviteRequest{Token: args[0]})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Joined %q (id %d) as %s\n", resp.GetBusiness().GetName(), resp.GetBusiness().GetId(), resp.GetRole())
			return nil
		},
	}
}
