// Package config loads server configuration from the environment.
package config

import (
	"fmt"
	"os"
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
	// PublicBaseURL is this server's own externally-reachable origin, e.g.
	// "https://ava.silverblueprints.net" — used both as the WebAuthn
	// relying-party origin and to build absolute links in the auth pages.
	PublicBaseURL string
	// RPID is the WebAuthn relying party ID: the domain passkeys are scoped
	// to, e.g. "ava.silverblueprints.net" (no scheme/port). Must exactly
	// match (or be a registrable-domain suffix of) the origin the browser
	// is actually on, or the ceremony fails.
	RPID string
	// JWTSecret signs and verifies access tokens (HMAC-SHA256). Required.
	JWTSecret string
	// BootstrapAdminEmail, if set, is granted global-admin status at server
	// startup — but only if no global admin exists yet (see
	// auth.EnsureBootstrapAdmin); a legitimate later transfer via
	// UserService.SetGlobalAdmin sticks even if this stays set. Optional —
	// leave unset once a real admin exists.
	BootstrapAdminEmail string
	// StorageEndpoint is the object-storage backend's S3-compatible API
	// address (host:port, no scheme) - SeaweedFS locally, see
	// docker-compose.yml's seaweedfs service and internal/storage.
	StorageEndpoint string
	// StorageAccessKey/StorageSecretKey authenticate to the storage
	// backend. SeaweedFS accepts any non-empty pair when it hasn't been
	// configured with a real identity file (true for the local dev
	// compose setup); a real deploy should use real credentials.
	StorageAccessKey string
	StorageSecretKey string
	// StorageBucket is the bucket attachments are stored in.
	StorageBucket string
	// StorageUseSSL selects http vs https to the storage endpoint. False
	// for the local dev SeaweedFS container.
	StorageUseSSL bool
}

func Load() (Config, error) {
	cfg := Config{
		PostgresDSN:   getEnv("AVA_POSTGRES_DSN", "postgres://ava:ava@localhost:5432/ava?sslmode=disable"),
		GRPCAddr:      getEnv("AVA_GRPC_ADDR", ":9090"),
		HTTPAddr:      getEnv("AVA_HTTP_ADDR", ":9091"),
		PublicBaseURL: getEnv("AVA_PUBLIC_BASE_URL", "https://ava.silverblueprints.net"),
		RPID:          getEnv("AVA_RP_ID", "ava.silverblueprints.net"),
		JWTSecret:     getEnv("AVA_JWT_SECRET", ""),

		BootstrapAdminEmail: getEnv("AVA_BOOTSTRAP_ADMIN_EMAIL", ""),

		StorageEndpoint:  getEnv("AVA_STORAGE_ENDPOINT", "localhost:8333"),
		StorageAccessKey: getEnv("AVA_STORAGE_ACCESS_KEY", "seaweedfs"),
		StorageSecretKey: getEnv("AVA_STORAGE_SECRET_KEY", "seaweedfs"),
		StorageBucket:    getEnv("AVA_STORAGE_BUCKET", "ava-attachments"),
		StorageUseSSL:    getEnv("AVA_STORAGE_USE_SSL", "false") == "true",
	}

	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("AVA_JWT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
