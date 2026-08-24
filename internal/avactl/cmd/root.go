// Package cmd is avactl's cobra command tree, modeled on kubectl:
// verb-first resource commands (get/create/delete/config/login), -o
// json|yaml|table, and a kubeconfig-shaped ~/.avactl/config for server +
// business context.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/apiclient"
	"github.com/silverbp/ava/internal/avactl/config"
	"github.com/silverbp/ava/internal/avactl/output"
)

var (
	flagServer   string
	flagInsecure bool
	flagBusiness int64
	flagOutput   string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "avactl",
		Short:         "avactl controls the Ava accounting API",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringVar(&flagServer, "server", "", "override the current context's server address")
	root.PersistentFlags().BoolVar(&flagInsecure, "insecure", false, "disable TLS when overriding --server (a local dev server has no certificate)")
	root.PersistentFlags().Int64Var(&flagBusiness, "business", 0, "override the current context's business id")
	root.PersistentFlags().StringVarP(&flagOutput, "output", "o", output.FormatTable, "output format: table|json|yaml")

	root.AddCommand(newGetCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newCreateCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newLoginCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newCloseCmd())
	root.AddCommand(newReconcileCmd())
	root.AddCommand(newContextCmd())
	root.AddCommand(newAcceptInviteCmd())
	root.AddCommand(newWhoamiCmd())
	root.AddCommand(newAdminCmd())
	return root
}

// resolveTarget resolves the server address, whether to dial it without
// TLS, and the business id to operate on: --server/--business flags take
// priority, falling back to the current ~/.avactl/config context.
func resolveTarget() (server string, insecureTransport bool, businessID int64, err error) {
	cfg, err := config.Load()
	if err != nil {
		return "", false, 0, err
	}

	server, insecureTransport, businessID, currentErr := cfg.Current()
	if flagServer != "" {
		server = flagServer
		insecureTransport = flagInsecure
	}
	if flagBusiness != 0 {
		businessID = flagBusiness
	}
	if server == "" {
		if currentErr != nil {
			return "", false, 0, currentErr
		}
		return "", false, 0, fmt.Errorf("no server address: pass --server or set one via `avactl config set-context`")
	}
	return server, insecureTransport, businessID, nil
}

func dial() (*grpc.ClientConn, string, int64, error) {
	server, insecureTransport, businessID, err := resolveTarget()
	if err != nil {
		return nil, "", 0, err
	}

	accessToken, err := resolveAccessToken(server, insecureTransport)
	if err != nil {
		return nil, "", 0, err
	}

	conn, err := apiclient.Dial(server, insecureTransport, accessToken)
	if err != nil {
		return nil, "", 0, fmt.Errorf("connecting to %s: %w", server, err)
	}
	return conn, server, businessID, nil
}

// resolveAccessToken returns the current context's access token,
// refreshing (and persisting the refresh) first if it's expired or about
// to expire. Returns "" with no error if the context has no user at all —
// e.g. one talking to a dev-bypass server, where no token is needed.
func resolveAccessToken(server string, insecureTransport bool) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	creds, userName, ok, err := cfg.CurrentUserCredentials()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	if time.Now().Before(creds.AccessTokenExpiry.Add(-30 * time.Second)) {
		return creds.AccessToken, nil
	}

	conn, err := apiclient.Dial(server, insecureTransport, "")
	if err != nil {
		return "", fmt.Errorf("connecting to %s to refresh session: %w", server, err)
	}
	defer conn.Close()

	resp, err := avav1.NewAuthServiceClient(conn).RefreshToken(context.Background(), &avav1.RefreshTokenRequest{
		RefreshToken: creds.RefreshToken,
		ClientName:   clientName(),
	})
	if err != nil {
		return "", fmt.Errorf("refreshing session (try `avactl login` again): %w", err)
	}

	newCreds := config.UserCredentials{
		RefreshToken:      resp.GetRefreshToken(),
		AccessToken:       resp.GetAccessToken(),
		AccessTokenExpiry: resp.GetAccessTokenExpiresAt().AsTime(),
	}
	cfg.SetUserCredentials(userName, newCreds)
	if err := cfg.Save(); err != nil {
		return "", fmt.Errorf("saving refreshed session: %w", err)
	}
	return newCreds.AccessToken, nil
}

func clientName() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return "avactl on " + host
}
