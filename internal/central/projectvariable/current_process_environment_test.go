package projectvariable

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled metadata SQL, not PostgreSQL or a substitute Execution authority.
// Any value/decryption/lease query, nested transaction or late lock is a failure.
type currentEnvStore struct {
	*secretAuthorityStore
	t                   *testing.T
	ordinary, secret    secretControlRow
	ids, projects, refs []string
	versions            []int64
	queryErr            error
	afterRead           func()
	writes, acquires    int
}

func (s *currentEnvStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *currentEnvStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	s.acquires++
	return errors.New("late acquire")
}
func (s *currentEnvStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.writes++
	return pgconn.CommandTag{}, errors.New("metadata-only writer")
}
func (s *currentEnvStore) QueryRow(_ context.Context, q string, _ ...any) postgres.Row {
	s.queries++
	if strings.Contains(q, "value") || strings.Contains(q, "credential_leases") || strings.Contains(q, "agenteam_execution.") || strings.Contains(q, "agenteam_agent.") {
		s.t.Fatal("current metadata queried another domain or value")
	}
	if s.afterRead != nil {
		s.afterRead()
	}
	if s.queryErr != nil {
		return secretControlRow{err: s.queryErr}
	}
	switch {
	case strings.Contains(q, "AS command_ordinary"):
		return secretControlRow{values: []any{s.ids}}
	case strings.Contains(q, "AS command_owned"):
		if strings.Contains(q, "WHERE project_id=") {
			s.t.Fatal("foreign reverse reference would be hidden")
		}
		return secretControlRow{values: []any{s.projects, s.refs, s.versions}}
	case strings.Contains(q, "type='variable'"):
		return s.ordinary
	case strings.Contains(q, "type='secret'"):
		return s.secret
	default:
		return secretControlRow{err: errors.New("unexpected current metadata SQL")}
	}
}

type currentEnvOwner struct {
	s       *currentEnvStore
	request c.CurrentProcessEnvironmentRequest
	facts   c.CurrentProcessEnvironmentFacts
	denied  bool
	calls   int
}

func (o *currentEnvOwner) RequireCurrentProcessEnvironmentInTx(_ context.Context, tx f.Tx, r c.CurrentProcessEnvironmentRequest) (c.CurrentProcessEnvironmentFacts, error) {
	o.calls++
	if o.denied || tx != o.s.tx || r != o.request {
		return c.CurrentProcessEnvironmentFacts{}, fault(f.Forbidden)
	}
	return o.facts.Clone(), nil
}
func newCurrentEnvControl(t *testing.T) (*CurrentProcessEnvironmentProvider, *currentEnvStore, *currentEnvOwner) {
	t.Helper()
	r := c.CurrentProcessEnvironmentRequest{ProjectID: testID[i.Project](2), AgentID: testID[i.Agent](3), ExecutionID: testID[i.Execution](4), CallBindingDigest: digest([]byte("complete-original-call"))}
	s := &currentEnvStore{secretAuthorityStore: &secretAuthorityStore{tx: f.NewTx(), locks: r.RequiredLocks()}, t: t, ids: []string{}, projects: []string{}, refs: []string{}, versions: []int64{}}
	o := &currentEnvOwner{s: s, request: r, facts: c.CurrentProcessEnvironmentFacts{AgentVersion: 3, AllowedSecretVariableIDs: []i.ProjectVariableID{}}}
	at, _ := f.ParseInstant("2026-10-12T10:00:00Z")
	ordinary, secret := testID[i.ProjectVariable](5), testID[i.ProjectVariable](6)
	s.ordinary = secretControlRow{values: []any{ordinary.String(), r.ProjectID.String(), c.VariableType, "ORDINARY", "description", int64(1), at.Time(), at.Time(), (*time.Time)(nil), (*string)(nil), (*int64)(nil)}}
	cred, cv := testID[sc.Credential](7).String(), int64(2)
	s.secret = secretControlRow{values: []any{secret.String(), r.ProjectID.String(), c.SecretVariableType, "SECRET", "description", int64(2), at.Time(), at.Time(), (*time.Time)(nil), &cred, &cv}}
	p, err := NewCurrentProcessEnvironment(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o
}
func TestCurrentProcessEnvironmentMetadataOnlyEmpty(t *testing.T) {
	for _, mode := range []string{"empty", "ordinary-empty-value", "ordinary-nonempty-value", "selected-secret"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o := newCurrentEnvControl(t)
			// Both stored ordinary values have identical metadata: the provider must
			// reject both without fetching either value. An unselected Secret can exist.
			if strings.HasPrefix(mode, "ordinary-") {
				s.ids = []string{testID[i.ProjectVariable](5).String()}
			}
			if mode == "selected-secret" {
				o.facts.AllowedSecretVariableIDs = []i.ProjectVariableID{testID[i.ProjectVariable](6)}
				s.projects = []string{o.request.ProjectID.String()}
				s.refs = []string{o.facts.AllowedSecretVariableIDs[0].String()}
				s.versions = []int64{3}
			}
			err := p.RequireEmptyInTx(context.Background(), s.tx, o.request)
			if mode == "empty" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				code(t, err, f.CapabilityUnsupported)
			}
			if o.calls != 2 || s.writes != 0 || s.acquires != 0 || s.transactions != 0 {
				t.Fatal("did not use original metadata-only owner transaction")
			}
		})
	}
}
func TestCurrentProcessEnvironmentRejectsIncompleteMetadata(t *testing.T) {
	for _, mode := range []string{"missing-ref", "extra-ref", "foreign-project", "stale-version", "missing-secret", "bad-credential", "bad-secret-name", "duplicate-name", "bad-ordinary", "unsorted-ordinary", "ordinary-cap", "metadata-read-failure"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o := newCurrentEnvControl(t)
			secret := testID[i.ProjectVariable](6)
			o.facts.AllowedSecretVariableIDs = []i.ProjectVariableID{secret}
			s.projects = []string{o.request.ProjectID.String()}
			s.refs = []string{secret.String()}
			s.versions = []int64{3}
			switch mode {
			case "missing-ref":
				s.projects = nil
				s.refs = nil
				s.versions = nil
			case "extra-ref":
				o.facts.AllowedSecretVariableIDs = []i.ProjectVariableID{}
			case "foreign-project":
				s.projects[0] = testID[i.Project](99).String()
			case "stale-version":
				s.versions[0] = 2
			case "missing-secret":
				s.secret = secretControlRow{err: pgx.ErrNoRows}
			case "bad-credential":
				s.secret.values[9] = (*string)(nil)
			case "bad-secret-name":
				s.secret.values[3] = "invalid name"
			case "duplicate-name":
				s.ids = []string{testID[i.ProjectVariable](5).String()}
				s.secret.values[3] = "ORDINARY"
			case "bad-ordinary":
				s.ids = []string{testID[i.ProjectVariable](5).String()}
				s.ordinary.values[5] = int64(0)
			case "unsorted-ordinary":
				s.ids = []string{testID[i.ProjectVariable](5).String(), testID[i.ProjectVariable](5).String()}
			case "ordinary-cap":
				s.ids = make([]string, c.MaxVariables+1)
			case "metadata-read-failure":
				s.queryErr = errors.New("read unavailable")
			}
			err := p.RequireEmptyInTx(context.Background(), s.tx, o.request)
			if err == nil {
				t.Fatal("missing/corrupt metadata became empty")
			}
			var problem *f.Fault
			if errors.As(err, &problem) && problem.Code == f.CapabilityUnsupported {
				t.Fatal("invalid metadata classified as valid nonempty set")
			}
			if s.writes != 0 || s.acquires != 0 || s.transactions != 0 {
				t.Fatal("metadata participant changed lifecycle")
			}
		})
	}
}
func TestCurrentProcessEnvironmentOriginalWitnessAndCancellation(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "missing-lock", "wrong-call", "denied", "nil-secret-set", "duplicate-secret-set", "retired-during-read", "changed-version-during-read", "cancelled", "cancel-during-read"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o := newCurrentEnvControl(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx, r := s.tx, o.request
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-lock":
				s.locks = s.locks[:len(s.locks)-1]
			case "wrong-call":
				r.CallBindingDigest = digest([]byte("different-original-call"))
			case "denied":
				o.denied = true
			case "nil-secret-set":
				o.facts.AllowedSecretVariableIDs = nil
			case "duplicate-secret-set":
				id := testID[i.ProjectVariable](6)
				o.facts.AllowedSecretVariableIDs = []i.ProjectVariableID{id, id}
			case "retired-during-read":
				s.afterRead = func() { o.denied = true }
			case "changed-version-during-read":
				s.afterRead = func() { o.facts.AgentVersion++ }
			case "cancelled":
				cancel()
			case "cancel-during-read":
				s.afterRead = cancel
			}
			if p.RequireEmptyInTx(ctx, tx, r) == nil {
				t.Fatal("unproven/retired/cancelled current empty accepted")
			}
			if s.writes != 0 || s.acquires != 0 || s.transactions != 0 {
				t.Fatal("participant acquired new work")
			}
		})
	}
}
