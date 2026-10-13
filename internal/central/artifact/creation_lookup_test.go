package artifact

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original live-Tx/ledger fixture controls Store returns; strict command
// and metadata decoders consume full rows. No real Project grant, ProcessGuard,
// PostgreSQL COMMIT or Object I/O is claimed by these tests.
type creationLookupStore struct {
	*artifactCallTestStore
	rows           *projectAuditFixture
	ownerReads     int
	businessReads  int
	businessWrites int
	preflight      func()
}

func (s *creationLookupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.artifactCallTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return creationLookupExecutor{s, tx}, nil
}

func (s *creationLookupStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	row := s.rows.store.QueryRow(ctx, query, args...)
	return projectAuditScan(func(out ...any) error {
		err := row.Scan(out...)
		if s.preflight != nil {
			s.preflight()
		}
		return err
	})
}

type creationLookupExecutor struct {
	store *creationLookupStore
	tx    f.Tx
}

func (e creationLookupExecutor) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	return nil, errors.New("LookupCreation must not open Rows or payloads")
}
func (e creationLookupExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s := e.store
	if _, err := s.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	if strings.Contains(query, "agenteam_artifact.project_") || strings.HasPrefix(query, "SELECT EXISTS") && strings.Contains(query, "agenteam_artifact.cleanup") {
		return s.fixture.row(ctx, e.tx, query, args)
	}
	s.businessReads++
	locks := createLocks(s.rows.command.identity, s.rows.command.ref.ArtifactID.String())
	s.mu.Lock()
	held := append([]f.LockRequest(nil), s.live[e.tx]...)
	s.mu.Unlock()
	for n, required := range locks {
		if n == 1 && s.rows.missingCommand {
			continue
		}
		found := false
		for _, lock := range held {
			found = found || lock.Key.Canonical() == required.Key.Canonical() && lock.Mode == required.Mode
		}
		if !found {
			return cleanupOwnerRow{err: errors.New("command/ref query before complete original locks")}
		}
	}
	if strings.HasPrefix(query, "SELECT id,project_id,creation_cause") {
		s.ownerReads++
		c := s.rows.storedCommand
		if len(args) != 1 || args[0] != c.ref.ArtifactID.String() {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		return cleanupOwnerRow{values: []any{c.ref.ArtifactID.String(), c.ref.ProjectID.String(), c.cause, c.originalActor, c.object.String(), "", string(c.identity.Key()), c.state, "existing", int64(1)}}
	}
	if strings.Contains(query, "FROM agenteam_artifact.commands ") && len(args) == 1 {
		if hash, ok := args[0].([]byte); ok && !bytes.Equal(hash, digestRaw(digestBytes([]byte(s.rows.command.identity.Canonical())))) {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
	}
	return s.rows.store.QueryRow(ctx, query, args...)
}
func (e creationLookupExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(query, "agenteam_artifact.project_") {
		e.store.businessWrites++
		return pgconn.CommandTag{}, errors.New("LookupCreation attempted a business write")
	}
	return e.store.fixture.exec(ctx, e.tx, query, args)
}

type creationLookupAuthority struct {
	*artifactCallFixture
	deny, drift bool
	intents     []i.AccessIntent
}

func (a *creationLookupAuthority) DiscoverInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	deps, err := a.artifactCallFixture.DiscoverInTx(ctx, tx, subject)
	if err == nil && a.drift && ctx.Value(artifactCallRegistrationKey{}) == nil {
		return oc.NewAccessDependencies(digestBytes([]byte("changed discovery")), deps.Locks())
	}
	return deps, err
}
func (a *creationLookupAuthority) AuthorizeInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	a.intents = append(a.intents, intent)
	if intent != i.Read || a.deny && ctx.Value(artifactCallRegistrationKey{}) == nil {
		return i.AccessGrant{}, failure(f.Forbidden, nil)
	}
	return a.artifactCallFixture.AuthorizeInTx(ctx, tx, subject, intent)
}

type creationLookupFixture struct {
	call           *artifactCallFixture
	store          *creationLookupStore
	authority      *creationLookupAuthority
	invocation     ac.Invocation
	meta           f.CommandMeta
	display        ac.Display
	media, content string
}

func newCreationLookupFixture(t *testing.T) *creationLookupFixture {
	t.Helper()
	call, p := newArtifactCallFixture(t)
	rows := newProjectAuditFixture(t, "content")
	v := &creationLookupFixture{call: call, invocation: call.invocation(t, p), meta: f.CommandMeta{RequestID: newTestID[f.Request](t), IdempotencyKey: "inline-lookup"}, display: ac.Display{Name: "fixture.txt", Description: "original description"}, media: "text/plain", content: "one"}
	m := rows.meta.Details()
	m.Reference.ProjectID = p
	m.Object.Scope = identityScope(p)
	m.Name, m.Description, m.CreatedByUserID = v.display.Name, v.display.Description, v.invocation.Details().Actor.Details().UserID
	var err error
	rows.meta, err = ac.NewMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	rows.storedMeta = rows.meta
	rows.request, err = makeContentRequest(v.invocation, v.meta, v.display, v.media, v.content)
	if err != nil {
		t.Fatal(err)
	}
	rows.command, err = newCommand(rows.request, m.Reference, oc.ResolvedSource{})
	if err != nil {
		t.Fatal(err)
	}
	rows.command.created, rows.command.object = m.CreatedAt, m.Object.ID
	rows.storedCommand, rows.rowCause = rows.command, rows.command.cause
	v.store = &creationLookupStore{artifactCallTestStore: call.store, rows: rows}
	v.authority = &creationLookupAuthority{artifactCallFixture: call}
	owners, err := NewOwnerProvider(v.store, v.authority)
	if err != nil {
		t.Fatal(err)
	}
	call.state.store, call.state.owners = v.store, owners
	return v
}
func (v *creationLookupFixture) lookup(ctx context.Context) (ac.CreationLookupResult, error) {
	return v.call.service.LookupCreation(ctx, v.invocation, v.meta, v.display, v.media, v.content)
}
func (v *creationLookupFixture) assertReadOnly(t *testing.T) {
	t.Helper()
	if v.store.businessWrites != 0 {
		t.Fatal("lookup wrote command, canonical or payload state")
	}
	for _, intent := range v.authority.intents {
		if intent != i.Read {
			t.Fatal("lookup used mutation/convergence authority")
		}
	}
	for _, record := range v.call.calls {
		if record.Kind != "read" || !record.Joined {
			t.Fatal("lookup did not finish its actual owned read")
		}
	}
}

func TestArtifactLookupCreationStates(t *testing.T) {
	for _, state := range []string{"not_observed", "pending", "completed", "cancel_requested", "cancelled", "deleted"} {
		t.Run(state, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			v.store.rows.missingCommand = state == "not_observed"
			v.store.rows.storedCommand.state = state
			out, err := v.lookup(context.Background())
			if state == "deleted" {
				projectAuditCode(t, err, f.ResourceDeleted)
				if out != (ac.CreationLookupResult{}) {
					t.Fatal("deleted exposed a result")
				}
			} else {
				expected := ac.CreationState(state)
				if state == "completed" {
					expected = ac.CreationCommitted
				}
				if err != nil || out.Validate() != nil || out.State != expected {
					t.Fatal("wrong exact command observation", out.State, err)
				}
				if out.Artifact != nil && out.Artifact.Details().Reference != v.store.rows.meta.Details().Reference {
					t.Fatal("committed returned another Artifact")
				}
			}
			if (v.store.ownerReads != 0) != (state == "completed") || v.store.rows.storedCommand.state != state {
				t.Fatal("noncommitted history borrowed Object read or changed command")
			}
			v.assertReadOnly(t)
		})
	}
}

func TestArtifactLookupCreationExactIntent(t *testing.T) {
	for _, mode := range []string{"name", "description", "media", "equal-size-content", "expected-version", "another-human", "new-session-attempt-trace", "source-command", "current-owner-denied", "discovery-changed", "reference-changed", "provenance"} {
		t.Run(mode, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			want := f.IdempotencyKeyReused
			switch mode {
			case "name":
				v.display.Name = "other.txt"
			case "description":
				v.display.Description += " other"
			case "media":
				v.media = "text/markdown"
			case "equal-size-content":
				v.content = "two"
			case "expected-version":
				version := f.Version(1)
				v.meta.ExpectedVersion = &version
			case "another-human", "new-session-attempt-trace", "provenance":
				d := v.invocation.Details()
				user, err := f.ParseID[i.User](d.Actor.Details().UserID)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "another-human" {
					user = newTestID[i.User](t)
					want = f.Forbidden
				}
				d.Actor, err = i.NewHuman(user, newTestID[i.Session](t))
				if err != nil {
					t.Fatal(err)
				}
				d.AttemptID = newTestID[ac.CallAttempt](t)
				if mode == "provenance" {
					d.ExecutionID = newTestID[i.Execution](t).String()
					want = f.DependencyUnbound
				}
				v.invocation, err = ac.NewInvocation(d)
				if err != nil {
					t.Fatal(err)
				}
				v.meta.RequestID = newTestID[f.Request](t)
			case "source-command":
				v.store.rows.storedCommand.path = "source"
			case "current-owner-denied":
				v.authority.deny = true
				want = f.Forbidden
			case "discovery-changed":
				v.authority.drift = true
				want = f.ResourceBusy
			case "reference-changed":
				v.store.preflight = func() { v.store.rows.storedCommand.ref.FileID = newTestID[ac.File](t) }
				want = f.ResourceBusy
			}
			out, err := v.lookup(context.Background())
			if mode == "new-session-attempt-trace" {
				if err != nil || out.State != ac.CreationPending {
					t.Fatal("same stable Human intent did not match", err)
				}
			} else {
				projectAuditCode(t, err, want)
				if out != (ac.CreationLookupResult{}) {
					t.Fatal("rejected lookup exposed state")
				}
			}
			if v.store.ownerReads != 0 {
				t.Fatal("rejected/history lookup borrowed Owner.Read")
			}
			v.assertReadOnly(t)
		})
	}
}

func TestArtifactLookupCreationCommittedMapping(t *testing.T) {
	for _, mode := range []string{"file", "object", "cause", "missing"} {
		t.Run(mode, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			v.store.rows.storedCommand.state = "completed"
			m := v.store.rows.storedMeta.Details()
			switch mode {
			case "file":
				m.Reference.FileID = newTestID[ac.File](t)
			case "object":
				m.Object.ID = newTestID[oc.StoredObject](t)
			case "cause":
				v.store.rows.rowCause = newTestID[struct{}](t).String()
			case "missing":
				v.store.rows.missingArtifact = true
			}
			var err error
			v.store.rows.storedMeta, err = ac.NewMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			out, err := v.lookup(context.Background())
			want := f.DependencyUnavailable
			if mode == "missing" {
				want = f.ResourceDeleted
			}
			projectAuditCode(t, err, want)
			if out != (ac.CreationLookupResult{}) || v.store.ownerReads != 1 {
				t.Fatal("invalid committed mapping exposed metadata or skipped Owner.Read")
			}
			v.assertReadOnly(t)
		})
	}
}

func TestArtifactLookupCreationOriginalReturn(t *testing.T) {
	for _, stage := range []string{"registration", "business", "technical-return"} {
		t.Run(stage, func(t *testing.T) {
			v := newCreationLookupFixture(t)
			barrier := newArtifactCallBarrier()
			v.call.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				owner := cause.Details().Owner
				if stage == "registration" && owner == "artifact-call-register" || stage == "technical-return" && owner == "artifact-call-return" || stage == "business" && owner != "artifact-call-register" && owner != "artifact-call-return" {
					barrier.hold()
				}
				return result
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				barrier.unblock()
				if !joined {
					cleanupOwnerWait(t, done)
				}
			})
			go func() { _, err := v.lookup(context.Background()); done <- err }()
			cleanupOwnerWait(t, barrier.entered)
			v.call.pair.Stop()
			if v.call.pair.Joined() {
				t.Fatal("pair joined before actual original Store return")
			}
			select {
			case <-done:
				joined = true
				t.Fatal("lookup returned before original Store")
			default:
			}
			v.call.pair.state.mu.Lock()
			for _, call := range v.call.pair.state.owners {
				if call.record.Kind != "read" || call.returned != (stage == "technical-return") || call.settling != (stage == "technical-return") {
					t.Error("read ownership lost the real return phase")
				}
			}
			v.call.pair.state.mu.Unlock()
			barrier.unblock()
			err := cleanupOwnerWait(t, done)
			joined = true
			if stage == "registration" && !errors.Is(err, context.Canceled) || stage != "registration" && err != nil || !v.call.pair.Joined() {
				t.Fatal("original return did not join", err)
			}
			v.assertReadOnly(t)
		})
	}
}

func TestArtifactLookupCreationUnknown(t *testing.T) {
	v := newCreationLookupFixture(t)
	var original f.CommitResult
	v.call.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner != "artifact-call-register" && cause.Details().Owner != "artifact-call-return" {
			original = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
			return original
		}
		return result
	}
	out, err := v.lookup(context.Background())
	requireCleanupOwnerUnknown(t, oc.ProjectCleanupResult{}, err, original)
	if out != (ac.CreationLookupResult{}) {
		t.Fatal("unknown observation exposed a state")
	}
	v.call.store.after = nil
	out, err = v.lookup(context.Background())
	if err != nil || out.State != ac.CreationPending {
		t.Fatal("explicit next read did not observe original pending", err)
	}
	v.assertReadOnly(t)
}
