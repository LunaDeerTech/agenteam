package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This read-only adapter deliberately has no mutation/network implementation.
// The source callback can replace the durable version while Secret is read,
// reproducing the CAS race without issuing a credential from a public DTO.
type oauthSourceStore struct {
	Store
	tx                               f.Tx
	held                             bool
	token                            oauthTokenSnapshot
	transactions, unknownAt, queries int
	attempt                          f.ID[f.TransactionAttempt]
}

func (s *oauthSourceStore) WithinTx(ctx context.Context, cause f.TransactionCause, body func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	s.tx, s.held = f.NewTx(), false
	defer func() { s.tx, s.held = f.Tx{}, false }()
	if err := body(ctx, s.tx); err != nil {
		var fault *f.Fault
		if errors.As(err, &fault) {
			return f.NotCommittedResult(fault)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	if s.transactions == s.unknownAt {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}

func (s *oauthSourceStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || len(locks) == 0 {
		return fail(f.InvalidState)
	}
	s.held = true
	return nil
}

func (s *oauthSourceStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !s.held {
		return nil, fail(f.InvalidState)
	}
	return s, nil
}

func (s *oauthSourceStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	v := s.token
	return oauthLookupRow(func(dest ...any) error {
		if !s.tx.Valid() || !s.held || len(dest) != 7 {
			return fail(f.InvalidState)
		}
		*dest[0].(*f.Version), *dest[1].(*string), *dest[2].(*string) = v.version, v.issuer, v.resource
		*dest[3].(*string), *dest[4].(*time.Time), *dest[5].(*bool), *dest[6].(*f.Version) = v.request, v.expires, v.refreshable, v.connectionVersion
		return nil
	})
}

func oauthReadSource(t *testing.T) (*Service, *oauthCredentialSource, *oauthSourceStore, sc.SecretMaterial) {
	t.Helper()
	store := &oauthSourceStore{token: oauthTokenSnapshot{version: 4, connectionVersion: 3, issuer: "https://issuer.example", resource: "https://resource.example/mcp", request: contentTailID[f.Request](t, "14").String(), expires: time.Now().UTC().Add(time.Hour), refreshable: true}, attempt: contentTailID[f.TransactionAttempt](t, "15")}
	s := &Service{store: store}
	source := &oauthCredentialSource{service: s, credential: contentTailID[sc.Credential](t, "11").String(), binding: RuntimeConnectionBinding{ProjectID: contentTailID[id.Project](t, "12"), ConfigID: contentTailID[ConfigIdentity](t, "13").String(), ConnectionID: contentTailID[ConnectionIdentity](t, "16").String(), ConfigRevision: 2}, caller: oauthRefreshCaller{startup: contentTailID[f.Request](t, "17").String()}}
	source.require = func(ctx context.Context, tx f.Tx) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if tx != store.tx || !store.held {
			return fail(f.Forbidden)
		}
		return nil
	}
	raw, err := json.Marshal(map[string]any{"format": 1, "kind": "token", "metadata": protocol.OAuthMetadata{Issuer: store.token.issuer, Resource: store.token.resource, AuthorizationEndpoint: "https://issuer.example/authorize", TokenEndpoint: "https://issuer.example/token", TokenAuthMethod: "none"}, "client_id": "fixture-client", "redirect_uri": "https://app.example/callback", "access_token": "fixture-access", "refresh_token": "fixture-refresh", "expiry": store.token.expires})
	if err != nil {
		t.Fatal(err)
	}
	material, err := sc.NewSecretMaterial(raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(material.Destroy)
	source.read = func(context.Context) (sc.SecretMaterial, error) { return material, nil }
	return s, source, store, material
}

func TestMCPOAuthRefreshSourceBindsBothSidesOfSecretRead(t *testing.T) {
	t.Run("access-copy", func(t *testing.T) {
		s, source, store, material := oauthReadSource(t)
		access, err := s.readOAuthCredential(context.Background(), source)
		defer access.Destroy()
		if err != nil || store.transactions != 2 || store.queries != 2 {
			t.Fatal("original source did not complete both reads", err)
		}
		if err = access.Use(func(raw []byte) error {
			if string(raw) != "fixture-access" {
				t.Fatal("bundle escaped as access")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err = material.Use(func([]byte) error { t.Fatal("source read copy remained live"); return nil }); err == nil {
			t.Fatal("source material was not destroyed")
		}
	})
	for name, drift := range map[string]func(*oauthTokenSnapshot){
		"secret-version":     func(v *oauthTokenSnapshot) { v.version++ },
		"connection-version": func(v *oauthTokenSnapshot) { v.connectionVersion++ },
		"original-request":   func(v *oauthTokenSnapshot) { v.request = "changed" },
		"resource":           func(v *oauthTokenSnapshot) { v.resource += "/other" },
	} {
		t.Run(name, func(t *testing.T) {
			s, source, store, material := oauthReadSource(t)
			source.read = func(context.Context) (sc.SecretMaterial, error) { drift(&store.token); return material, nil }
			access, err := s.readOAuthCredential(context.Background(), source)
			defer access.Destroy()
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.Code != f.ConfirmationStale {
				t.Fatal("stale Secret read reached refresh or access", err)
			}
		})
	}
	t.Run("issuer-does-not-match-ledger", func(t *testing.T) {
		s, source, store, _ := oauthReadSource(t)
		store.token.issuer = "https://other.example"
		access, err := s.readOAuthCredential(context.Background(), source)
		defer access.Destroy()
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.ConfirmationStale {
			t.Fatal("foreign issuer projected access", err)
		}
	})
}

func TestMCPOAuthRefreshSourcePreservesUnknownAndRechecksAuthority(t *testing.T) {
	for _, at := range []int{1, 2} {
		t.Run(map[int]string{1: "before-read", 2: "after-read"}[at], func(t *testing.T) {
			s, source, store, material := oauthReadSource(t)
			store.unknownAt = at
			reads := 0
			source.read = func(context.Context) (sc.SecretMaterial, error) { reads++; return material, nil }
			access, err := s.readOAuthCredential(context.Background(), source)
			defer access.Destroy()
			var original *UnknownError
			if !errors.As(err, &original) || original.AttemptID() != store.attempt || reads != at-1 || store.transactions != at {
				t.Fatal("unknown metadata lookup admitted read/retry or lost original attempt", err)
			}
		})
	}
	t.Run("withdrawn-after-secret-read", func(t *testing.T) {
		s, source, store, material := oauthReadSource(t)
		authorized := true
		source.require = func(context.Context, f.Tx) error {
			if !authorized {
				return fail(f.Forbidden)
			}
			return nil
		}
		source.read = func(context.Context) (sc.SecretMaterial, error) { authorized = false; return material, nil }
		access, err := s.readOAuthCredential(context.Background(), source)
		defer access.Destroy()
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.Forbidden || store.queries != 1 {
			t.Fatal("withdrawn caller reached second token read or refresh", err)
		}
	})
	t.Run("mixed-public-callers", func(t *testing.T) {
		s, source, store, _ := oauthReadSource(t)
		source.caller.user = contentTailID[id.User](t, "18").String()
		_, err := s.readOAuthCredential(context.Background(), source)
		if err == nil || store.transactions != 0 {
			t.Fatal("mixed caller fields became source authority")
		}
	})
}
