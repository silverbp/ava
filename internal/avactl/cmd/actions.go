// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

// The four RPC-calling shapes every noun's verbs reduce to. A noun's own
// file supplies one small closure per verb; newGetCmd/newListCmd/
// newMutateCmd/newPdfCmd below wire it into a cobra.Command with
// consistent dial/output/help handling, so every noun's get/list/etc.
// behave and read identically.
type getFunc func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error)
type listFunc func(ctx context.Context, conn *grpc.ClientConn, businessID int64) ([]proto.Message, error)
type mutateFunc func(ctx context.Context, conn *grpc.ClientConn, id string) (proto.Message, error)
type pdfFunc func(ctx context.Context, conn *grpc.ClientConn, id string) ([]byte, error)

// newGroupCmd builds a noun's parent command (e.g. `avactl invoice`).
func newGroupCmd(n resource.Noun, summary string) *cobra.Command {
	cmd := &cobra.Command{Use: n.Singular, Aliases: n.Aliases}
	resource.Doc{Summary: summary}.Apply(cmd)
	return cmd
}

// article returns "an" before a vowel sound, else "a" — so generated help
// text reads "Get an invoice" / "Get a payment" instead of "Get a invoice".
func article(s string) string {
	if len(s) > 0 && strings.ContainsRune("aeiouAEIOU", rune(s[0])) {
		return "an"
	}
	return "a"
}

func newGetCmd(n resource.Noun, fn getFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "get <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			obj, err := fn(cmd.Context(), conn, args[0])
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, obj, n.Columns)
		},
	}
	resource.Doc{
		Summary:  fmt.Sprintf("Get %s %s by id", article(n.Singular), n.Singular),
		Examples: []resource.Example{{Cmd: fmt.Sprintf("avactl %s get 42", n.Singular)}},
	}.Apply(cmd)
	return cmd
}

func newListCmd(n resource.Noun, fn listFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			items, err := fn(cmd.Context(), conn, businessID)
			if err != nil {
				return err
			}
			return output.PrintList(cmd.OutOrStdout(), flagOutput, items, n.Columns)
		},
	}
	resource.Doc{
		Summary:  fmt.Sprintf("List %s", n.Plural),
		Examples: []resource.Example{{Cmd: fmt.Sprintf("avactl %s list", n.Singular)}},
	}.Apply(cmd)
	return cmd
}

// newMutateCmd builds a single-RPC, id-in/object-out verb: deactivate,
// send, cancel, accept, decline, post, ...
func newMutateCmd(n resource.Noun, verb, summary string, fn mutateFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:  verb + " <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			obj, err := fn(cmd.Context(), conn, args[0])
			if err != nil {
				return err
			}
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, obj, n.Columns)
		},
	}
	resource.Doc{
		Summary:  summary,
		Examples: []resource.Example{{Cmd: fmt.Sprintf("avactl %s %s 42", n.Singular, verb)}},
	}.Apply(cmd)
	return cmd
}

func newPdfCmd(n resource.Noun, fn pdfFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "pdf <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, _, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()
			content, err := fn(cmd.Context(), conn, args[0])
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(content)
			return err
		},
	}
	resource.Doc{
		Summary:  fmt.Sprintf("Render %s %s as PDF, written to stdout", article(n.Singular), n.Singular),
		Detail:   "Redirect stdout to a file to save it.",
		Examples: []resource.Example{{Cmd: fmt.Sprintf("avactl %s pdf 42 > %s-42.pdf", n.Singular, n.Singular)}},
	}.Apply(cmd)
	return cmd
}
