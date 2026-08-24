package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	"github.com/silverbp/ava/internal/db/sqlcgen"
)

// NewWebAuthn builds the relying-party instance used for every ceremony.
// rpID must be the domain the browser is actually on during the ceremony
// (no scheme/port); origin is the exact scheme+host[+port].
func NewWebAuthn(rpID, rpDisplayName, origin string) (*gowebauthn.WebAuthn, error) {
	return gowebauthn.New(&gowebauthn.Config{
		RPDisplayName: rpDisplayName,
		RPID:          rpID,
		RPOrigins:     []string{origin},
	})
}

// webauthnUser adapts an app_user + its stored credentials to the
// go-webauthn User interface.
type webauthnUser struct {
	id          int64
	email       string
	displayName string
	credentials []gowebauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte                           { return []byte(fmt.Sprintf("%d", u.id)) }
func (u *webauthnUser) WebAuthnName() string                         { return u.email }
func (u *webauthnUser) WebAuthnDisplayName() string                  { return u.displayName }
func (u *webauthnUser) WebAuthnCredentials() []gowebauthn.Credential { return u.credentials }

func loadWebAuthnUser(ctx context.Context, q *sqlcgen.Queries, appUser sqlcgen.AppUser) (*webauthnUser, error) {
	rows, err := q.ListWebAuthnCredentialsForUser(ctx, appUser.ID)
	if err != nil {
		return nil, err
	}
	creds := make([]gowebauthn.Credential, len(rows))
	for i, r := range rows {
		creds[i] = credentialFromRow(r)
	}
	displayName := appUser.Email
	if appUser.DisplayName != nil && *appUser.DisplayName != "" {
		displayName = *appUser.DisplayName
	}
	return &webauthnUser{id: appUser.ID, email: appUser.Email, displayName: displayName, credentials: creds}, nil
}

func credentialFromRow(r sqlcgen.WebauthnCredential) gowebauthn.Credential {
	var transports []protocol.AuthenticatorTransport
	if r.Transports != nil && *r.Transports != "" {
		for _, t := range strings.Split(*r.Transports, ",") {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
	}
	var attestationType string
	if r.AttestationType != nil {
		attestationType = *r.AttestationType
	}
	return gowebauthn.Credential{
		ID:              r.CredentialID,
		PublicKey:       r.PublicKey,
		AttestationType: attestationType,
		Transport:       transports,
		Authenticator: gowebauthn.Authenticator{
			AAGUID:    r.Aaguid,
			SignCount: uint32(r.SignCount),
		},
	}
}

// --- ceremony session store ---
//
// SessionData (the challenge, etc.) must survive between a ceremony's
// begin and finish HTTP requests. In-memory by design, same rationale as
// authcode.go: a ceremony completes within seconds, so this doesn't need
// to survive a server restart or be shared across replicas.

var (
	ceremonyMu    sync.Mutex
	ceremonyStore = map[string]*gowebauthn.SessionData{}
)

func saveCeremonySession(s *gowebauthn.SessionData) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating ceremony session id: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(buf)

	ceremonyMu.Lock()
	defer ceremonyMu.Unlock()
	ceremonyStore[id] = s
	return id, nil
}

func loadCeremonySession(id string) (*gowebauthn.SessionData, bool) {
	ceremonyMu.Lock()
	defer ceremonyMu.Unlock()
	s, ok := ceremonyStore[id]
	delete(ceremonyStore, id) // one-shot
	return s, ok
}

// --- registration ceremony ---

// BeginRegistration starts a passkey registration for a user identified by
// email, creating the app_user row if it doesn't exist yet — passkey
// registration is how a new Ava user signs up, there's no separate
// "create account" step.
func BeginRegistration(ctx context.Context, q *sqlcgen.Queries, w *gowebauthn.WebAuthn, email, displayName string) (*protocol.CredentialCreation, string, error) {
	appUser, err := q.GetAppUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, "", err
		}
		appUser, err = q.CreateAppUser(ctx, sqlcgen.CreateAppUserParams{Email: email, DisplayName: &displayName})
		if err != nil {
			return nil, "", err
		}
	}

	u, err := loadWebAuthnUser(ctx, q, appUser)
	if err != nil {
		return nil, "", err
	}

	creation, session, err := w.BeginRegistration(u, gowebauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
	if err != nil {
		return nil, "", err
	}
	sessionID, err := saveCeremonySession(session)
	if err != nil {
		return nil, "", err
	}
	return creation, sessionID, nil
}

// FinishRegistration completes the ceremony begun by BeginRegistration,
// persisting the new credential.
func FinishRegistration(ctx context.Context, q *sqlcgen.Queries, w *gowebauthn.WebAuthn, email, sessionID string, r *http.Request) (*User, error) {
	session, ok := loadCeremonySession(sessionID)
	if !ok {
		return nil, fmt.Errorf("registration session expired or not found")
	}

	appUser, err := q.GetAppUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	u, err := loadWebAuthnUser(ctx, q, appUser)
	if err != nil {
		return nil, err
	}

	cred, err := w.FinishRegistration(u, *session, r)
	if err != nil {
		return nil, err
	}

	var transportsPtr *string
	if len(cred.Transport) > 0 {
		strs := make([]string, len(cred.Transport))
		for i, t := range cred.Transport {
			strs[i] = string(t)
		}
		joined := strings.Join(strs, ",")
		transportsPtr = &joined
	}
	var attestationTypePtr *string
	if cred.AttestationType != "" {
		attestationTypePtr = &cred.AttestationType
	}

	if _, err := q.CreateWebAuthnCredential(ctx, sqlcgen.CreateWebAuthnCredentialParams{
		UserID:          appUser.ID,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		AttestationType: attestationTypePtr,
		Transports:      transportsPtr,
		Aaguid:          cred.Authenticator.AAGUID,
		SignCount:       int64(cred.Authenticator.SignCount),
	}); err != nil {
		return nil, err
	}

	return &User{ID: appUser.ID, Email: appUser.Email}, nil
}

// --- login ceremony ---
//
// Usernameless/discoverable: the browser's own passkey UI picks which
// credential to use (including via cross-device QR/BLE — handled entirely
// by the browser, nothing here implements that transport), so there's no
// "enter your email" step before this.

func BeginLoginCeremony(w *gowebauthn.WebAuthn) (*protocol.CredentialAssertion, string, error) {
	assertion, session, err := w.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	sessionID, err := saveCeremonySession(session)
	if err != nil {
		return nil, "", err
	}
	return assertion, sessionID, nil
}

func FinishLoginCeremony(ctx context.Context, q *sqlcgen.Queries, w *gowebauthn.WebAuthn, sessionID string, r *http.Request) (*User, error) {
	session, ok := loadCeremonySession(sessionID)
	if !ok {
		return nil, fmt.Errorf("login session expired or not found")
	}

	handler := func(rawID, userHandle []byte) (gowebauthn.User, error) {
		var id int64
		if _, err := fmt.Sscanf(string(userHandle), "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid user handle: %w", err)
		}
		appUser, err := q.GetAppUser(ctx, id)
		if err != nil {
			return nil, err
		}
		return loadWebAuthnUser(ctx, q, appUser)
	}

	validatedUser, validatedCred, err := w.FinishPasskeyLogin(handler, *session, r)
	if err != nil {
		return nil, err
	}
	wu, ok := validatedUser.(*webauthnUser)
	if !ok {
		return nil, fmt.Errorf("unexpected user type %T from FinishPasskeyLogin", validatedUser)
	}

	// Clone-detection bookkeeping: persist the authenticator's post-ceremony
	// sign count (FinishPasskeyLogin already rejected the login if it looked
	// cloned; this just keeps our stored baseline current for next time).
	credRow, err := q.GetWebAuthnCredentialByCredentialID(ctx, validatedCred.ID)
	if err != nil {
		return nil, err
	}
	if err := q.UpdateWebAuthnCredentialSignCount(ctx, sqlcgen.UpdateWebAuthnCredentialSignCountParams{
		ID:        credRow.ID,
		SignCount: int64(validatedCred.Authenticator.SignCount),
	}); err != nil {
		return nil, err
	}

	return &User{ID: wu.id, Email: wu.email}, nil
}
