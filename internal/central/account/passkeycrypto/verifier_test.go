package passkeycrypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

const fixtureOrigin = "https://accounts.example.test:8443"
const fixtureRPID = "accounts.example.test"

// virtualAuthenticator only constructs browser/authenticator wire responses. All
// parsing, policy enforcement and signature verification use the real verifier.
type virtualAuthenticator struct {
	key *ecdsa.PrivateKey
	id  []byte
}

type ceremonyOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		RPID                   string `json:"rpId"`
		UserVerification       string `json:"userVerification"`
		AuthenticatorSelection struct {
			UserVerification string `json:"userVerification"`
			ResidentKey      string `json:"residentKey"`
		} `json:"authenticatorSelection"`
	} `json:"publicKey"`
}

func TestRegistrationAndSignedPasskeyLogin(t *testing.T) {
	v, user, authenticator := registeredFixture(t)
	capabilities := v.Capabilities()
	if !capabilities.Available || capabilities.RPID == nil || *capabilities.RPID != fixtureRPID {
		t.Fatalf("HTTPS origin should derive host-only RP ID: %+v", capabilities)
	}
	for _, discoverable := range []bool{false, true} {
		name := "identified"
		if discoverable {
			name = "discoverable"
		}
		t.Run(name, func(t *testing.T) {
			options, session := beginFixtureAssertion(t, v, user, discoverable)
			if options.PublicKey.RPID != fixtureRPID || options.PublicKey.UserVerification != "required" {
				t.Fatalf("unsafe assertion options: %+v", options.PublicKey)
			}
			// Synced authenticators may legitimately keep their counter at zero.
			response := authenticator.assertion(t, options.PublicKey.Challenge, fixtureOrigin, fixtureRPID, user.ID, 0x1d, 0, false)
			lookups := 0
			id, credential, err := v.FinishAssertion(func(rawID, userHandle []byte) (User, error) {
				lookups++
				if !bytes.Equal(rawID, authenticator.id) || !bytes.Equal(userHandle, user.ID) {
					t.Fatal("lookup did not receive authenticator credential ID and user handle")
				}
				return user, nil
			}, session, response)
			if err != nil {
				t.Fatalf("valid signed assertion: %v", err)
			}
			if lookups != 1 || !bytes.Equal(id, user.ID) || !bytes.Equal(credential.ID, authenticator.id) || !json.Valid(credential.PublicData) {
				t.Fatalf("unexpected assertion result: lookup count=%d user=%x credential=%x", lookups, id, credential.ID)
			}
			user.Credentials = []Credential{credential}
		})
	}
}

func TestRegistrationRejectsInvalidCeremony(t *testing.T) {
	for _, name := range []string{"origin", "port", "rp_id", "user_verification", "challenge", "expired", "owner"} {
		t.Run(name, func(t *testing.T) {
			v := New(fixtureOrigin)
			user := fixtureUser()
			authenticator := newVirtualAuthenticator(t)
			rawOptions, session, err := v.BeginRegistration(user)
			if err != nil {
				t.Fatal(err)
			}
			options := decodeFixtureOptions(t, rawOptions)
			origin, rpID, challenge, flags := fixtureOrigin, fixtureRPID, options.PublicKey.Challenge, byte(0x5d)
			switch name {
			case "origin":
				origin = "https://attacker.example.test:8443"
			case "port":
				origin = "https://accounts.example.test:9443"
			case "rp_id":
				rpID = "attacker.example.test"
			case "user_verification":
				flags &^= 0x04
			case "challenge":
				challenge = base64.RawURLEncoding.EncodeToString([]byte("different-server-challenge"))
			case "expired":
				session = expiredFixtureSession(t, session)
			case "owner":
				user.ID = []byte("different-account-owner")
			}
			response := authenticator.registration(t, challenge, origin, rpID, flags)
			if _, err := v.FinishRegistration(user, session, response); err == nil {
				t.Fatalf("accepted registration with invalid %s", name)
			}
		})
	}
}

func TestAssertionRejectsInvalidCeremony(t *testing.T) {
	for _, discoverable := range []bool{false, true} {
		mode := "identified"
		if discoverable {
			mode = "discoverable"
		}
		t.Run(mode, func(t *testing.T) {
			for _, name := range []string{"origin", "port", "rp_id", "signature", "user_verification", "user_presence", "challenge", "expired", "handle", "owner", "unknown_credential", "lookup_failure"} {
				t.Run(name, func(t *testing.T) {
					v, user, authenticator := registeredFixture(t)
					options, session := beginFixtureAssertion(t, v, user, discoverable)
					origin, rpID, challenge, flags := fixtureOrigin, fixtureRPID, options.PublicKey.Challenge, byte(0x1d)
					handle := bytes.Clone(user.ID)
					lookupUser := user
					switch name {
					case "origin":
						origin = "https://attacker.example.test:8443"
					case "port":
						origin = "https://accounts.example.test:9443"
					case "rp_id":
						rpID = "attacker.example.test"
					case "user_verification":
						flags &^= 0x04
					case "user_presence":
						flags &^= 0x01
					case "challenge":
						challenge = base64.RawURLEncoding.EncodeToString([]byte("different-server-challenge"))
					case "expired":
						session = expiredFixtureSession(t, session)
					case "handle":
						handle = []byte("unrelated-user-handle")
					case "owner":
						lookupUser.ID = []byte("different-account-owner")
					case "unknown_credential":
						authenticator.id = []byte("unregistered-credential-id")
					}
					response := authenticator.assertion(t, challenge, origin, rpID, handle, flags, 0, name == "signature")
					id, credential, err := v.FinishAssertion(func(_, _ []byte) (User, error) {
						if name == "lookup_failure" {
							return User{}, errors.New("credential unavailable")
						}
						return lookupUser, nil
					}, session, response)
					if err == nil {
						t.Fatalf("accepted assertion with invalid %s", name)
					}
					if len(id) != 0 || len(credential.ID) != 0 {
						t.Fatal("failed assertion returned an authenticated identity or credential")
					}
				})
			}
		})
	}
}

func registeredFixture(t *testing.T) (*Verifier, User, *virtualAuthenticator) {
	t.Helper()
	v, user, authenticator := New(fixtureOrigin), fixtureUser(), newVirtualAuthenticator(t)
	rawOptions, session, err := v.BeginRegistration(user)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	options := decodeFixtureOptions(t, rawOptions)
	if options.PublicKey.RP.ID != fixtureRPID || options.PublicKey.AuthenticatorSelection.UserVerification != "required" || options.PublicKey.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatalf("unsafe registration options: %+v", options.PublicKey)
	}
	response := authenticator.registration(t, options.PublicKey.Challenge, fixtureOrigin, fixtureRPID, 0x5d)
	credential, err := v.FinishRegistration(user, session, response)
	if err != nil {
		t.Fatalf("valid none-attestation registration: %v", err)
	}
	if !bytes.Equal(credential.ID, authenticator.id) || !json.Valid(credential.PublicData) {
		t.Fatal("registration did not return persisted public credential data")
	}
	user.Credentials = []Credential{credential}
	return v, user, authenticator
}

func fixtureUser() User {
	return User{ID: []byte("isolated-passkey-test-user"), Name: "passkey-test@example.test", DisplayName: "Passkey Test"}
}

func beginFixtureAssertion(t *testing.T, v *Verifier, user User, discoverable bool) (ceremonyOptions, json.RawMessage) {
	t.Helper()
	var options, session json.RawMessage
	var err error
	if discoverable {
		options, session, err = v.BeginDiscoverableLogin()
	} else {
		options, session, err = v.BeginAssertion(user)
	}
	if err != nil {
		t.Fatalf("begin assertion: %v", err)
	}
	return decodeFixtureOptions(t, options), session
}

func decodeFixtureOptions(t *testing.T, data json.RawMessage) ceremonyOptions {
	t.Helper()
	var options ceremonyOptions
	if err := json.Unmarshal(data, &options.PublicKey); err != nil {
		t.Fatal(err)
	}
	if options.PublicKey.Challenge == "" {
		t.Fatal("missing credential options challenge")
	}
	return options
}

func newVirtualAuthenticator(t *testing.T) *virtualAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &virtualAuthenticator{key: key, id: id}
}

func (a *virtualAuthenticator) registration(t *testing.T, challenge, origin, rpID string, flags byte) json.RawMessage {
	t.Helper()
	publicKey, err := webauthncbor.Marshal(map[int]any{
		1: 2, 3: -7, -1: 1,
		-2: a.key.X.FillBytes(make([]byte, 32)),
		-3: a.key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	authData := fixtureAuthData(rpID, flags, 0)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.id)))
	authData = append(authData, a.id...)
	authData = append(authData, publicKey...)
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "authData": authData, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return fixtureJSON(t, map[string]any{
		"id": fixtureBase64(a.id), "rawId": fixtureBase64(a.id), "type": "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"attestationObject": fixtureBase64(attestation),
			"clientDataJSON":    fixtureBase64(fixtureClientData(t, "webauthn.create", challenge, origin)),
			"transports":        []string{"internal"},
		},
	})
}

func (a *virtualAuthenticator) assertion(t *testing.T, challenge, origin, rpID string, handle []byte, flags byte, counter uint32, badSignature bool) json.RawMessage {
	t.Helper()
	clientData := fixtureClientData(t, "webauthn.get", challenge, origin)
	clientHash := sha256.Sum256(clientData)
	authData := fixtureAuthData(rpID, flags, counter)
	signedData := append(bytes.Clone(authData), clientHash[:]...)
	digest := sha256.Sum256(signedData)
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if badSignature {
		signature[len(signature)-1] ^= 0x01
	}
	return fixtureJSON(t, map[string]any{
		"id": fixtureBase64(a.id), "rawId": fixtureBase64(a.id), "type": "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON": fixtureBase64(clientData), "authenticatorData": fixtureBase64(authData),
			"signature": fixtureBase64(signature), "userHandle": fixtureBase64(handle),
		},
	})
}

func fixtureAuthData(rpID string, flags byte, counter uint32) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	data := append(bytes.Clone(rpHash[:]), flags)
	return binary.BigEndian.AppendUint32(data, counter)
}

func fixtureClientData(t *testing.T, typ, challenge, origin string) json.RawMessage {
	t.Helper()
	return fixtureJSON(t, map[string]any{"type": typ, "challenge": challenge, "origin": origin, "crossOrigin": false})
}

func fixtureBase64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func fixtureJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func expiredFixtureSession(t *testing.T, session json.RawMessage) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(session, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["expires"]; !ok {
		t.Fatal("session lacks expiry")
	}
	fields["expires"] = fixtureJSON(t, time.Now().Add(-time.Minute))
	return fixtureJSON(t, fields)
}
