// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Command ava runs the gRPC API server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/silverbp/ava/internal/config"
	"github.com/silverbp/ava/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	srv, err := server.New(ctx, cfg)
	if err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run() }()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		srv.Close()
	case err := <-errCh:
		if err != nil {
			slog.Error("server exited", "error", err)
			os.Exit(1)
		}
	}
}
