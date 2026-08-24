// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Package server wires the gRPC server: the Postgres-backed Store, the auth
// interceptor, and one implementation per proto service.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/config"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/storage"
)

type Server struct {
	cfg        config.Config
	store      *db.Store
	grpc       *grpc.Server
	httpServer *http.Server
}

func New(ctx context.Context, cfg config.Config) (*Server, error) {
	store, err := db.NewStore(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, err
	}

	blobs, err := storage.New(cfg.StorageEndpoint, cfg.StorageAccessKey, cfg.StorageSecretKey, cfg.StorageBucket, cfg.StorageUseSSL)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("configuring object storage: %w", err)
	}
	if err := blobs.EnsureBucket(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("configuring object storage: %w", err)
	}

	if cfg.BootstrapAdminEmail != "" {
		admin, err := auth.EnsureBootstrapAdmin(ctx, store, cfg.BootstrapAdminEmail)
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("bootstrapping global admin: %w", err)
		}
		if admin != nil {
			slog.Info("bootstrapped global admin", "user_id", admin.ID, "email", admin.Email)
		}
	}

	verifyAccessToken := func(token string) (*auth.User, error) {
		return auth.VerifyAccessToken(cfg.JWTSecret, token)
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(verifyAccessToken)),
		grpc.StreamInterceptor(auth.StreamInterceptor(verifyAccessToken)),
	)

	avav1.RegisterBusinessServiceServer(grpcServer, newBusinessService(store))
	avav1.RegisterLedgerAccountServiceServer(grpcServer, newLedgerAccountService(store))
	avav1.RegisterLedgerTransactionServiceServer(grpcServer, newLedgerTransactionService(store))
	avav1.RegisterReportingServiceServer(grpcServer, newReportingService(store))
	avav1.RegisterPeriodCloseServiceServer(grpcServer, newPeriodCloseService(store))
	avav1.RegisterContactServiceServer(grpcServer, newContactService(store))
	avav1.RegisterServiceCatalogServiceServer(grpcServer, newServiceCatalogService(store))
	avav1.RegisterTaxRateServiceServer(grpcServer, newTaxRateService(store))
	avav1.RegisterEstimateServiceServer(grpcServer, newEstimateService(store))
	avav1.RegisterInvoiceServiceServer(grpcServer, newInvoiceService(store))
	avav1.RegisterPaymentServiceServer(grpcServer, newPaymentService(store))
	avav1.RegisterBankStatementServiceServer(grpcServer, newBankStatementService(store))
	avav1.RegisterEntityContextServiceServer(grpcServer, newEntityContextService(store))
	avav1.RegisterAttachmentServiceServer(grpcServer, newAttachmentService(store, blobs))
	avav1.RegisterAuthServiceServer(grpcServer, newAuthService(store, cfg))
	avav1.RegisterUserServiceServer(grpcServer, newUserService(store))

	// Reflection lets grpcurl/grpcui introspect the API without shipping
	// .proto files alongside every deploy. Revisit gating this before a
	// real production launch.
	reflection.Register(grpcServer)

	authMux, err := newAuthMux(store, cfg)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("configuring auth HTTP server: %w", err)
	}
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: authMux,
	}

	return &Server{cfg: cfg, store: store, grpc: grpcServer, httpServer: httpServer}, nil
}

// Run serves both listeners until either exits; an error from one stops
// the other.
func (s *Server) Run() error {
	lis, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.GRPCAddr, err)
	}

	errCh := make(chan error, 2)
	go func() {
		slog.Info("ava gRPC server listening", "addr", s.cfg.GRPCAddr)
		errCh <- s.grpc.Serve(lis)
	}()
	go func() {
		slog.Info("ava auth HTTP server listening", "addr", s.cfg.HTTPAddr)
		err := s.httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	return <-errCh
}

func (s *Server) Close() {
	s.grpc.GracefulStop()
	_ = s.httpServer.Close()
	s.store.Close()
}
