// Package passkeycrypto adapts the WebAuthn verifier to account persistence.
// Callers own challenge consumption, purpose/session binding, current account
// authorization and transactional credential updates. SessionData must only be
// loaded from trusted server storage, never supplied by the browser.
package passkeycrypto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	ErrUnavailable = errors.New("passkey unavailable")
	ErrInvalidData = errors.New("invalid passkey data")
)

// Capabilities describes whether the configured public origin supports passkeys.
type Capabilities struct {
	Available bool    `json:"available"`
	Reason    string  `json:"reason"`
	RPID      *string `json:"rp_id"`
}

// Credential contains a searchable ID and the complete library credential JSON.
// PublicData is private server storage; it must be persisted without projection
// or reconstruction, including after every successful assertion.
type Credential struct {
	ID         []byte
	PublicData json.RawMessage
}

// User identifies an account by its immutable, opaque ID (1 to 64 bytes).
// Name and DisplayName are only authenticator display labels.
type User struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []Credential
}

// Lookup retrieves the current owner of rawID from trusted account storage.
// userHandle is untrusted until verified. It can be absent in a bound assertion.
// The returned credentials must be the current, active credentials of the user.
type Lookup func(rawID, userHandle []byte) (User, error)

// Verifier has immutable relying-party configuration and no persistence state.
type Verifier struct {
	engine *webauthn.WebAuthn
	origin string
	caps   Capabilities
}

// New uses a fixed deployment origin. Unsupported origins disable passkeys
// without preventing the account service from supporting password login.
func New(publicOrigin string) *Verifier {
	v := &Verifier{caps: Capabilities{Reason: "secure_origin_required"}}
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return v
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || net.ParseIP(host) != nil {
		v.caps.Reason = "domain_required"
		return v
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && host == "localhost") {
		return v
	}
	if err := protocol.ValidateRPID(host); err != nil {
		v.caps.Reason = "domain_required"
		return v
	}
	// Preserve the complete origin, including a nondefault port. This value is
	// never derived from Host, Forwarded, or other request headers.
	v.origin = u.Scheme + "://" + strings.ToLower(u.Host)
	requireResident := true
	engine, err := webauthn.New(&webauthn.Config{
		RPID:                  host,
		RPDisplayName:         "Agenteam",
		RPOrigins:             []string{v.origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: &requireResident,
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
		},
	})
	if err != nil {
		v.caps.Reason = "domain_required"
		return v
	}
	v.engine = engine
	v.caps = Capabilities{Available: true, Reason: "available", RPID: &host}
	return v
}

func (v *Verifier) Capabilities() Capabilities {
	if v == nil {
		return Capabilities{Reason: "secure_origin_required"}
	}
	caps := v.caps
	if caps.RPID != nil {
		rpID := *caps.RPID
		caps.RPID = &rpID
	}
	return caps
}

// BeginRegistration returns native PublicKeyCredentialCreationOptionsJSON (the
// publicKey object itself) and the opaque server-side SessionData JSON.
func (v *Verifier) BeginRegistration(user User) (json.RawMessage, json.RawMessage, error) {
	if v == nil || v.engine == nil {
		return nil, nil, ErrUnavailable
	}
	u, err := adaptUser(user)
	if err != nil {
		return nil, nil, err
	}
	options, session, err := v.engine.BeginRegistration(u,
		webauthn.WithRegistrationOrigin(v.origin),
		webauthn.WithExclusions(webauthn.Credentials(u.credentials).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return nil, nil, err
	}
	return marshalBegin(options.Response, session)
}

func (v *Verifier) FinishRegistration(user User, sessionData, response json.RawMessage) (Credential, error) {
	session, err := v.readSession(sessionData)
	if err != nil {
		return Credential{}, err
	}
	u, err := adaptUser(user)
	if err != nil {
		return Credential{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return Credential{}, err
	}
	if !v.validClientOrigin(parsed.Response.CollectedClientData.Origin) {
		return Credential{}, fmt.Errorf("%w: origin", ErrInvalidData)
	}
	credential, err := v.engine.CreateCredential(u, session, parsed)
	if err != nil {
		return Credential{}, err
	}
	if credential.Extensions.RK != nil && !*credential.Extensions.RK {
		return Credential{}, fmt.Errorf("%w: resident credential required", ErrInvalidData)
	}
	for _, existing := range u.credentials {
		if bytes.Equal(existing.ID, credential.ID) {
			return Credential{}, fmt.Errorf("%w: credential already registered", ErrInvalidData)
		}
	}
	return persistCredential(credential)
}

// BeginDiscoverableLogin returns native PublicKeyCredentialRequestOptionsJSON
// and SessionData for a username-free login with required user verification.
func (v *Verifier) BeginDiscoverableLogin() (json.RawMessage, json.RawMessage, error) {
	if v == nil || v.engine == nil {
		return nil, nil, ErrUnavailable
	}
	options, session, err := v.engine.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithLoginOrigin(v.origin))
	if err != nil {
		return nil, nil, err
	}
	return marshalBegin(options.Response, session)
}

// BeginAssertion binds the assertion to the current stable user and credentials.
func (v *Verifier) BeginAssertion(user User) (json.RawMessage, json.RawMessage, error) {
	if v == nil || v.engine == nil {
		return nil, nil, ErrUnavailable
	}
	u, err := adaptUser(user)
	if err != nil {
		return nil, nil, err
	}
	options, session, err := v.engine.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithLoginOrigin(v.origin))
	if err != nil {
		return nil, nil, err
	}
	return marshalBegin(options.Response, session)
}

// FinishAssertion verifies either a discoverable or a bound assertion. It
// returns the authenticated stable user ID and updated complete credential.
// The caller must consume the challenge and persist the update atomically
// against the same credential/account versions that Lookup returned.
func (v *Verifier) FinishAssertion(lookup Lookup, sessionData, response json.RawMessage) ([]byte, Credential, error) {
	session, err := v.readSession(sessionData)
	if err != nil {
		return nil, Credential{}, err
	}
	if lookup == nil {
		return nil, Credential{}, ErrInvalidData
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, Credential{}, err
	}
	if !v.validClientOrigin(parsed.Response.CollectedClientData.Origin) {
		return nil, Credential{}, fmt.Errorf("%w: origin", ErrInvalidData)
	}
	handle := parsed.Response.UserHandle
	if len(session.UserID) == 0 && len(handle) == 0 {
		return nil, Credential{}, fmt.Errorf("%w: missing user handle", ErrInvalidData)
	}
	user, err := lookup(bytes.Clone(parsed.RawID), bytes.Clone(handle))
	if err != nil {
		return nil, Credential{}, err
	}
	u, err := adaptUser(user)
	if err != nil {
		return nil, Credential{}, err
	}
	if (len(handle) != 0 && !bytes.Equal(handle, u.ID)) || (len(session.UserID) != 0 && !bytes.Equal(session.UserID, u.ID)) {
		return nil, Credential{}, fmt.Errorf("%w: user binding mismatch", ErrInvalidData)
	}
	var credential *webauthn.Credential
	if len(session.UserID) == 0 {
		_, credential, err = v.engine.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
			// The library calls this with the same parsed assertion. Preserve the
			// binding explicitly rather than performing a second database lookup.
			if !bytes.Equal(rawID, parsed.RawID) || !bytes.Equal(userHandle, u.ID) {
				return nil, ErrInvalidData
			}
			return u, nil
		}, session, parsed)
	} else {
		credential, err = v.engine.ValidateLogin(u, session, parsed)
	}
	if err != nil {
		return nil, Credential{}, err
	}
	stored, err := persistCredential(credential)
	if err != nil {
		return nil, Credential{}, err
	}
	// The library retains counter/backup metadata and clone warnings. Synced
	// credentials legitimately use zero counters; those are not rejected.
	return bytes.Clone(u.ID), stored, nil
}

// An origin is a scheme/host/port tuple, not a URL with credentials, a path or
// query. Keep the library's canonical default-port comparison after enforcing
// that syntax; its generic origin matcher also accepts full URLs.
func (v *Verifier) validClientOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	return protocol.IsOriginInHaystack(origin, []string{v.origin})
}

func (v *Verifier) readSession(data json.RawMessage) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	if v == nil || v.engine == nil {
		return session, ErrUnavailable
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("%w: session encoding", ErrInvalidData)
	}
	if session.RelyingPartyID != v.engine.Config.RPID || session.Origin != v.origin || session.UserVerification != protocol.VerificationRequired || session.Expires.IsZero() || !time.Now().Before(session.Expires) {
		return session, fmt.Errorf("%w: session binding or expiry", ErrInvalidData)
	}
	return session, nil
}

type libraryUser struct {
	User
	credentials []webauthn.Credential
}

func (u libraryUser) WebAuthnID() []byte                         { return u.ID }
func (u libraryUser) WebAuthnName() string                       { return u.Name }
func (u libraryUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u libraryUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func adaptUser(user User) (libraryUser, error) {
	u := libraryUser{User: user, credentials: make([]webauthn.Credential, 0, len(user.Credentials))}
	if len(user.ID) == 0 || len(user.ID) > 64 {
		return u, fmt.Errorf("%w: user ID", ErrInvalidData)
	}
	seen := make(map[string]struct{}, len(user.Credentials))
	for _, stored := range user.Credentials {
		var credential webauthn.Credential
		if err := json.Unmarshal(stored.PublicData, &credential); err != nil || len(credential.ID) == 0 || len(credential.PublicKey) == 0 || !bytes.Equal(stored.ID, credential.ID) {
			return u, fmt.Errorf("%w: credential encoding or ID", ErrInvalidData)
		}
		if _, exists := seen[string(credential.ID)]; exists {
			return u, fmt.Errorf("%w: duplicate credential", ErrInvalidData)
		}
		seen[string(credential.ID)] = struct{}{}
		u.credentials = append(u.credentials, credential)
	}
	return u, nil
}

func persistCredential(credential *webauthn.Credential) (Credential, error) {
	data, err := json.Marshal(credential)
	if err != nil {
		return Credential{}, err
	}
	return Credential{ID: bytes.Clone(credential.ID), PublicData: data}, nil
}

func marshalBegin(options any, session *webauthn.SessionData) (json.RawMessage, json.RawMessage, error) {
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, nil, err
	}
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return nil, nil, err
	}
	return optionsJSON, sessionJSON, nil
}
