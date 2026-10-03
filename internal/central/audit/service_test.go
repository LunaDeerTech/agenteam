package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	contract "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"log/slog"
	"strings"
	"testing"
)

func testEntry(t *testing.T, session, trace, attempt string) contract.Entry {
	t.Helper()
	u, _ := foundation.ParseID[identity.User]("01900000-0000-7000-8000-000000000001")
	ss, _ := foundation.ParseID[identity.Session](session)
	actor, _ := identity.NewHuman(u, ss)
	resource, _ := contract.NewResource(contract.SecretResource, "01900000-0000-7000-8000-000000000004")
	metadata, _ := contract.SecretMutationMetadata(contract.SecretCreate, 1, []contract.ChangedField{contract.ValueChanged})
	e, err := contract.NewEntry(contract.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: contract.SecretCreate, Outcome: contract.Success, Resource: resource, Metadata: metadata, Associations: contract.Associations{HTTPTraceID: trace, RequestID: attempt}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

type noQueryStore struct{ Store }
type sessionPort func(context.Context, foundation.Tx, identity.Actor) error

func (f sessionPort) RequireCurrentSession(ctx context.Context, tx foundation.Tx, a identity.Actor) error {
	return f(ctx, tx, a)
}

type systemPort func(context.Context, foundation.Tx, identity.Actor, identity.AccessIntent) (identity.AccessGrant, error)

func (f systemPort) AuthorizeSystem(ctx context.Context, tx foundation.Tx, a identity.Actor, i identity.AccessIntent) (identity.AccessGrant, error) {
	return f(ctx, tx, a, i)
}
func TestReadAuthorizationRejectionFailureAndCancellationBeforeQuery(t *testing.T) {
	keys, _ := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	actor := testEntry(t, "01900000-0000-7000-8000-000000000002", "", "").Fields().Actor
	cases := []struct {
		name    string
		session sessionPort
		system  systemPort
		code    foundation.Code
	}{
		{"rejected", func(context.Context, foundation.Tx, identity.Actor) error {
			return foundation.NewFault(foundation.SessionRevoked, foundation.NotStarted)
		}, nil, foundation.SessionRevoked},
		{"failure", func(context.Context, foundation.Tx, identity.Actor) error { return errors.New("private-driver-canary") }, nil, foundation.DependencyUnavailable},
		{"unbound", func(context.Context, foundation.Tx, identity.Actor) error { return nil }, nil, foundation.DependencyUnbound},
		{"forged-grant", func(context.Context, foundation.Tx, identity.Actor) error { return nil }, func(context.Context, foundation.Tx, identity.Actor, identity.AccessIntent) (identity.AccessGrant, error) {
			return identity.AccessGrant{}, nil
		}, foundation.DependencyUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, err := New(&noQueryStore{}, keys, Authorizations{Sessions: tc.session, System: tc.system})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.List(context.Background(), actor, identity.SystemScope(), contract.Filter{}, foundation.PageRequest{Cursor: "bad", Limit: 1})
			var f *foundation.Fault
			if !errors.As(err, &f) || f.Code != tc.code {
				t.Fatal("authorization did not precede cursor/query")
			}
			assertSafe(t, err, "private-driver-canary")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service, _ := New(&noQueryStore{}, keys, Authorizations{Sessions: sessionPort(func(ctx context.Context, _ foundation.Tx, _ identity.Actor) error { return ctx.Err() })})
	_, err := service.List(ctx, actor, identity.SystemScope(), contract.Filter{}, foundation.DefaultPageRequest())
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != foundation.DependencyUnavailable {
		t.Fatal("cancelled authority reached query")
	}
}
func TestSemanticDigestStablePrincipalButDistinctAttempt(t *testing.T) {
	a := "01900000-0000-7000-8000-000000000002"
	b := "01900000-0000-7000-8000-000000000003"
	first, _ := SemanticDigest(testEntry(t, a, a, a))
	replay, _ := SemanticDigest(testEntry(t, b, b, a))
	attempt, _ := SemanticDigest(testEntry(t, b, b, b))
	if first != replay || first == attempt {
		t.Fatal("session/trace or real attempt semantic boundary changed")
	}
	identity, err := foundation.NewCommandIdentity("system", []string{"01900000-0000-7000-8000-000000000001"}, "secret.create", "raw-business-key-canary")
	if err != nil {
		t.Fatal(err)
	}
	key, err := CommandAppendKey(contract.SecretProducer, identity, 0)
	if err != nil || !strings.HasPrefix(key.Details().CauseRef, "sha256:") {
		t.Fatal("command append key missing digest")
	}
	assertSafe(t, key, "raw-business-key-canary")
}
func TestAuthorizationErrorSafeAndFaultIdentity(t *testing.T) {
	raw := errors.New("private-driver-canary")
	err := unavailable(raw)
	if !errors.Is(err, raw) {
		t.Fatal("cause identity lost")
	}
	assertSafe(t, err, raw.Error())
	f := foundation.NewFault(foundation.SessionRevoked, foundation.NotStarted)
	err = portError(f)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != foundation.SessionRevoked {
		t.Fatal("authorization classification lost")
	}
}

func TestUnresolvedCommitErrorPreservesUnknownAndSafeCause(t *testing.T) {
	raw := errors.New("private-verification-canary")
	err := failure(foundation.CommitUnknown, "cleanup_unknown", raw)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != foundation.CommitUnknown || fault.CommitState != foundation.Unknown {
		t.Fatal("unresolved commit error lost its public unknown state")
	}
	if !errors.Is(err, raw) {
		t.Fatal("verification cause identity lost")
	}
	assertSafe(t, err, raw.Error())
	assertSafe(t, fault, raw.Error())
	ordinary := unavailable(raw)
	if !errors.As(ordinary, &fault) || fault.CommitState != foundation.NotStarted {
		t.Fatal("ordinary pre-operation failure acquired an unknown commit state")
	}
}
func assertSafe(t *testing.T, v any, canary string) {
	t.Helper()
	for _, outer := range []any{v, struct{ private any }{v}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, outer), canary) {
				t.Fatal("format leaks")
			}
		}
		raw, _ := json.Marshal(outer)
		if strings.Contains(string(raw), canary) {
			t.Fatal("JSON leaks")
		}
		for _, text := range []bool{true, false} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewJSONHandler(&b, nil)
			if text {
				h = slog.NewTextHandler(&b, nil)
			}
			slog.New(h).LogAttrs(context.Background(), slog.LevelInfo, "projection", slog.Any("value", outer))
			if strings.Contains(b.String(), canary) {
				t.Fatal("slog leaks")
			}
		}
	}
}
