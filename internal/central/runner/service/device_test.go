package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These private executor controls exercise producer ordering and output gates;
// they do not prove that the SQL parses, commits, or rolls back in PostgreSQL.
type deviceRow struct {
	values []any
	err    error
}

func (r deviceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("wrong private row arity")
	}
	for n, v := range r.values {
		out := reflect.ValueOf(dest[n]).Elem()
		if v == nil {
			out.SetZero()
		} else {
			out.Set(reflect.ValueOf(v))
		}
	}
	return nil
}

type deviceExecutor struct {
	target                         c.RunnerID
	now, issued, expires           time.Time
	public                         []byte
	hash                           [32]byte
	total, own                     int64
	missing, badToken, missingFact bool
	registered, consumed, enrolled bool
	writes                         []string
}

func (x *deviceExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("unexpected rows query")
}
func (x *deviceExecutor) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(query, "SELECT root_path"):
		if x.missing {
			return deviceRow{err: pgx.ErrNoRows}
		}
		var public []byte
		if x.registered {
			public = x.public
		}
		return deviceRow{values: []any{"/srv", int64(1), int64(1), int64(0), public, x.issued}}
	case query == "SELECT clock_timestamp()":
		return deviceRow{values: []any{x.now}}
	case strings.HasPrefix(query, "SELECT token_hash"):
		if x.badToken {
			return deviceRow{err: pgx.ErrNoRows}
		}
		return deviceRow{values: []any{x.hash[:], x.issued, x.expires}}
	case strings.HasPrefix(query, "SELECT r.id::text"):
		return deviceRow{values: []any{x.target.String(), "runner", "", []byte(`[]`), "/srv", int64(2), int64(1), x.public, &x.now, nil, nil, x.issued, x.now, "offline"}}
	case strings.HasPrefix(query, "SELECT EXISTS"):
		return deviceRow{values: []any{!x.missingFact && x.consumed && x.enrolled}}
	case strings.HasPrefix(query, "SELECT count(*)"):
		return deviceRow{values: []any{x.total, x.own}}
	}
	return deviceRow{err: errors.New("unexpected private query")}
}
func (x *deviceExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	x.writes = append(x.writes, query)
	switch {
	case strings.HasPrefix(query, "UPDATE agenteam_runner.enrollment_tokens"):
		x.consumed = true
	case strings.HasPrefix(query, "UPDATE agenteam_runner.runners"):
		if !x.consumed {
			return pgconn.CommandTag{}, errors.New("key before consumption")
		}
		x.public = append([]byte(nil), args[1].([]byte)...)
	case strings.HasPrefix(query, "INSERT INTO agenteam_runner.identity_events"):
		if len(x.public) != 32 || args[4] != p.PublicKeyFingerprint([32]byte(x.public)) {
			return pgconn.CommandTag{}, errors.New("event before key")
		}
		x.enrolled = true
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type deviceStore struct {
	readerStore
	x       *deviceExecutor
	unknown bool
	attempt f.ID[f.TransactionAttempt]
	last    f.CommitResult
}

func (s *deviceStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign transaction")
	}
	return s.x, nil
}
func (s *deviceStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.readerStore.WithinTx(ctx, cause, fn)
	if s.unknown && result.State() == f.Committed {
		result = f.UnknownResult(s.attempt, cause)
	}
	s.last = result
	return result
}

type deviceAudit struct {
	authority *Authority
	store     *deviceStore
	seen      int
}

func (a *deviceAudit) AppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	if !a.store.x.enrolled || !a.store.x.consumed || entry.Fields().Actor.Details().ServiceName != id.RunnerIdentity || entry.Fields().Actor.Details().Kind != id.Service {
		return ac.AppendReceipt{}, errors.New("audit before enrolled fact or borrowed Human")
	}
	if e := a.authority.CheckAppendInTx(ctx, tx, entry, key); e != nil {
		return ac.AppendReceipt{}, e
	}
	a.seen++
	return ac.AppendReceipt{}, nil
}

func deviceFixture(t *testing.T) (*Service, *deviceStore, *deviceAudit, p.EnrollmentRequest) {
	t.Helper()
	target := sid[c.Runner](t)
	token, _ := p.NewEnrollmentToken()
	hash, _ := token.Digest()
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &deviceExecutor{target: target, now: now, issued: now.Add(-time.Minute), expires: now.Add(time.Minute), hash: hash}
	store := &deviceStore{x: x, attempt: sid[f.TransactionAttempt](t)}
	authority, e := NewAuthority(store, &readerAuthority{})
	if e != nil {
		t.Fatal(e)
	}
	audit := &deviceAudit{authority: authority, store: store}
	s, e := New(authority, audit)
	if e != nil {
		t.Fatal(e)
	}
	public := [32]byte{1, 2, 3}
	request, e := p.NewEnrollmentRequest(p.ID(target.String()), token, public, "/srv", "linux", "amd64")
	if e != nil {
		t.Fatal(e)
	}
	return s, store, audit, request
}

func TestRunnerDevicePureEnrollmentAtomicOutputGate(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		s, store, audit, request := deviceFixture(t)
		store.unknown = unknown
		out, e := s.Enroll(context.Background(), request)
		if audit.seen != 1 || !store.x.enrolled || len(store.x.writes) != 3 {
			t.Fatal("enrollment did not reach the complete same-Tx producer")
		}
		if unknown {
			var original commitFailure
			if !errors.As(e, &original) || original.result.AttemptID() != store.attempt || original.result.Cause().Details().Owner != "runner-device" {
				t.Fatal("unknown lost original attempt/cause")
			}
			if _, encode := p.EncodeEnrollmentResponse(out); encode == nil {
				t.Fatal("unknown leaked success projection")
			}
		} else if e != nil || out.RunnerID() != request.RunnerID() || out.Version() != "2" || out.CredentialGeneration() != "1" || out.PublicKeyFingerprint() != p.PublicKeyFingerprint(request.PublicKey()) {
			t.Fatal("wrong known projection", e)
		}
		if len(store.held) != 2 || store.held[0].Mode != f.Exclusive || store.held[1].Mode != f.Exclusive || f.CompareLockKeys(store.held[0].Key, store.held[1].Key) >= 0 {
			t.Fatal("device omitted sorted management/connection gates")
		}
	}
}

func TestRunnerDevicePureEnrollmentRejectsMissingCredentialAndFacts(t *testing.T) {
	for _, mode := range []string{"unknown", "wrong-token", "already-enrolled", "expired", "not-issued", "missing-fact"} {
		t.Run(mode, func(t *testing.T) {
			s, store, audit, request := deviceFixture(t)
			switch mode {
			case "unknown":
				store.x.missing = true
			case "wrong-token":
				store.x.badToken = true
			case "already-enrolled":
				store.x.registered, store.x.public = true, make([]byte, 32)
			case "expired":
				store.x.expires = store.x.now
			case "not-issued":
				store.x.issued = store.x.now.Add(time.Microsecond)
			case "missing-fact":
				store.x.missingFact = true
			}
			out, e := s.Enroll(context.Background(), request)
			var ff *f.Fault
			want := f.Unauthenticated
			if mode == "missing-fact" {
				want = f.Forbidden
			}
			if !errors.As(e, &ff) || ff.Code != want || audit.seen != 0 {
				t.Fatal("credential or fact rejection lost", e)
			}
			if _, encode := p.EncodeEnrollmentResponse(out); encode == nil || mode != "missing-fact" && len(store.x.writes) != 0 {
				t.Fatal("rejected credential published or changed facts")
			}
		})
	}
}

func TestRunnerDevicePureChallengeCapacityAndStop(t *testing.T) {
	for _, mode := range []string{"success", "own-cap", "global-cap", "unbound", "unknown", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _, enroll := deviceFixture(t)
			store.x.registered, store.x.public = true, make([]byte, 32)
			request, _ := p.NewChallengeRequest(enroll.RunnerID())
			switch mode {
			case "own-cap":
				store.x.own = 4
			case "global-cap":
				store.x.total = 16384
			case "unbound":
				store.x.registered = false
			case "unknown":
				store.unknown = true
			case "stopped":
				s.Stop()
			}
			out, e := s.Challenge(context.Background(), request)
			if mode == "success" {
				if e != nil || !out.Nonce().Valid() || len(store.x.writes) != 2 || !strings.Contains(store.x.writes[0], "LIMIT 128") || out.ExpiresAt() != p.Instant(store.x.now.Add(30*time.Second).Format(time.RFC3339Nano)) {
					t.Fatal("bounded challenge", e)
				}
			} else {
				if e == nil || out.Nonce().Valid() {
					t.Fatal("rejected/unknown challenge published nonce")
				}
				if mode == "stopped" && store.calls != 0 || mode == "unbound" && len(store.x.writes) != 0 {
					t.Fatal("invalid admission opened maintenance")
				}
			}
		})
	}
}

func TestRunnerDevicePureEnrollmentProofCannotBeForged(t *testing.T) {
	s, store, _, request := deviceFixture(t)
	registration, _ := id.RegisterService(id.RunnerIdentity)
	event := sid[c.IdentityEvent](t)
	actor, _ := registration.Actor(event.String(), id.SystemScope())
	fp := f.Digest(p.PublicKeyFingerprint(request.PublicKey()))
	metadata, _ := ac.RunnerMetadata(ac.RunnerEnroll, ac.RunnerMetadataFields{RunnerID: store.x.target.String(), Version: 2, CredentialGeneration: 1, ChangedFields: []string{"credential"}, PublicKeyFingerprint: &fp})
	resource, _ := ac.NewResource(ac.RunnerResource, store.x.target.String())
	entry, e := ac.NewEntry(ac.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: ac.RunnerEnroll, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RunnerID: store.x.target.String()}})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := ac.NewAppendKey(ac.RunnerProducer, event.String(), 0)
	tx := f.NewTx()
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), enrollmentProofKey{}, &enrollmentProof{authority: s.state().authority, tx: f.NewTx(), entry: entry, key: key, event: event}), context.WithValue(context.Background(), enrollmentProofKey{}, &enrollmentProof{authority: &Authority{}, tx: tx, entry: entry, key: key, event: event})} {
		e := s.state().authority.CheckAppendInTx(ctx, tx, entry, key)
		var ff *f.Fault
		if !errors.As(e, &ff) || ff.Code != f.Forbidden {
			t.Fatal("public service actor/foreign proof authorized an Audit", e)
		}
	}
}
