// Package config loads server configuration from the environment.
package config

import (
	"fmt"
	"os"
)

// AuthMode selects how the gRPC auth interceptor resolves the calling user.
type AuthMode string

const (
	// AuthModeDev resolves every RPC to a fixed local dev user, with no
	// passkey/WebAuthn setup required. Never valid alongside
	// AVA_ENV=production.
	AuthModeDev AuthMode = "dev"
	// AuthModePasskey verifies a real access token issued after a WebAuthn
	// ceremony (see internal/auth/webauthn.go, internal/server/httpauth.go).
	AuthModePasskey AuthMode = "passkey"
)

type Config struct {
	// PostgresDSN is the connection string for the ava Postgres database.
	PostgresDSN string
	// GRPCAddr is the listen address for the gRPC server.
	GRPCAddr string
	// HTTPAddr is the listen address for the plain HTTP server that hosts
	// the WebAuthn registration/login pages and callback endpoints. A
	// reverse proxy (see deploy/Caddyfile) fronts both this and GRPCAddr on
	// one public domain + port.
	HTTPAddr string
	// AuthMode selects dev-bypass vs. real passkey auth.
	AuthMode AuthMode
	// Env is "development" or "production" — used only to refuse starting
	// with AuthModeDev in production.
	Env string
	// PublicBaseURL is this server's own externally-reachable origin, e.g.
	// "https://ava.silverblueprints.net" — used both as the WebAuthn
	// relying-party origin and to build absolute links in the auth pages.
	// Ignored when AuthMode is AuthModeDev.
	PublicBaseURL string
	// RPID is the WebAuthn relying party ID: the domain passkeys are scoped
	// to, e.g. "ava.silverblueprints.net" (no scheme/port). Must exactly
	// match (or be a registrable-domain suffix of) the origin the browser
	// is actually on, or the ceremony fails. Ignored when AuthMode is
	// AuthModeDev.
	RPID string
	// JWTSecret signs and verifies access tokens (HMAC-SHA256). Required
	// (and validated non-empty) whenever AuthMode is AuthModePasskey.
	JWTSecret string
}

func Load() (Config, error) {
	cfg := Config{
		PostgresDSN:   getEnv("AVA_POSTGRES_DSN", "postgres://ava:ava@localhost:5432/ava?sslmode=disable"),
		GRPCAddr:      getEnv("AVA_GRPC_ADDR", ":9090"),
		HTTPAddr:      getEnv("AVA_HTTP_ADDR", ":9091"),
		AuthMode:      AuthMode(getEnv("AVA_AUTH_MODE", string(AuthModeDev))),
		Env:           getEnv("AVA_ENV", "development"),
		PublicBaseURL: getEnv("AVA_PUBLIC_BASE_URL", "https://ava.silverblueprints.net"),
		RPID:          getEnv("AVA_RP_ID", "ava.silverblueprints.net"),
		JWTSecret:     getEnv("AVA_JWT_SECRET", ""),
	}

	if cfg.AuthMode != AuthModeDev && cfg.AuthMode != AuthModePasskey {
		return Config{}, fmt.Errorf("invalid AVA_AUTH_MODE %q: must be %q or %q", cfg.AuthMode, AuthModeDev, AuthModePasskey)
	}
	if cfg.AuthMode == AuthModeDev && cfg.Env == "production" {
		return Config{}, fmt.Errorf("refusing to start: AVA_AUTH_MODE=dev is not allowed when AVA_ENV=production")
	}
	if cfg.AuthMode == AuthModePasskey && cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("AVA_JWT_SECRET is required when AVA_AUTH_MODE=passkey")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
