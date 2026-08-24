package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/silverbp/ava/internal/config"
)

// publicMethods are reachable without a valid access token — AuthService
// itself, since a caller by definition doesn't have one yet when
// exchanging a code, and may not have a valid one left when refreshing or
// logging out.
var publicMethods = map[string]bool{
	"/ava.v1.AuthService/ExchangeCode": true,
	"/ava.v1.AuthService/RefreshToken": true,
	"/ava.v1.AuthService/Logout":       true,
}

// UnaryInterceptor resolves the calling user for every RPC before it
// reaches a handler. In dev mode, devUser is injected as the caller for
// every request with no verification performed. In passkey mode, the
// caller must present a valid "authorization: Bearer <access token>"
// metadata entry, verified via verifyAccessToken — except for
// publicMethods, which skip straight to the handler in both modes.
func UnaryInterceptor(mode config.AuthMode, devUser *User, verifyAccessToken func(token string) (*User, error)) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if publicMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		switch mode {
		case config.AuthModeDev:
			ctx = ContextWithUser(ctx, devUser)
		case config.AuthModePasskey:
			token, err := bearerToken(ctx)
			if err != nil {
				return nil, err
			}
			u, err := verifyAccessToken(token)
			if err != nil {
				return nil, status.Errorf(codes.Unauthenticated, "invalid access token: %v", err)
			}
			ctx = ContextWithUser(ctx, u)
		default:
			return nil, status.Errorf(codes.Internal, "unknown auth mode %q", mode)
		}
		return handler(ctx, req)
	}
}

func bearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no authorization metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", status.Error(codes.Unauthenticated, "no authorization metadata")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(vals[0], prefix) {
		return "", status.Error(codes.Unauthenticated, `authorization metadata must be "Bearer <token>"`)
	}
	return strings.TrimPrefix(vals[0], prefix), nil
}
