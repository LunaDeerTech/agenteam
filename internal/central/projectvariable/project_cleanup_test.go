package projectvariable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls borrow the actual pair and its original live-Tx test Store.
// The authority and SQL results are controlled; they do not grant a real
// Project claim, prove PostgreSQL rollback, or retire any D04 material.
type cleanupControlAuthority struct {
	h     *ownershipControl
	actor i.Actor
	cause pc.LifecycleCause
	err   error
	calls int
}

func (a *cleanupControlAuthority) RequireProjectVariablesCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, project i.ProjectID) error {
	a.calls++
	if !a.actor.Equal(actor) || cause != a.cause || project != a.h.record.Project {
		return fault(f.Forbidden)
	}
	if err := a.h.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return err
	}
	return a.err
}

type cleanupControlRow func(...any) error

func (r cleanupControlRow) Scan(out ...any) error { return r(out...) }
func cleanupBool(value bool, err error) postgres.Row {
	return cleanupControlRow(func(out ...any) error {
		if err != nil {
			return err
		}
		if len(out) != 1 {
			return errors.New("unexpected cleanup projection")
		}
		p, ok := out[0].(*bool)
		if !ok {
			return errors.New("unexpected cleanup projection type")
		}
		*p = value
		return nil
	})
}

type cleanupControl struct {
	h                              *ownershipControl
	authority                      *cleanupControlAuthority
	cleaner                        *ProjectCleanup
	actor                          i.Actor
	cause                          pc.LifecycleCause
	scope                          pc.ScopeRef
	running, referenced, remaining bool
	reads                          int
	tables                         []string
	counts                         map[string]int
	readErr, execErr               error
}

func newCleanupControl(t *testing.T) *cleanupControl {
	t.Helper()
	h := newOwnershipControl(t)
	actor, cause, scope := h.lifecycle(t, pc.Delete)
	v := &cleanupControl{h: h, actor: actor, cause: cause, scope: scope, counts: make(map[string]int)}
	v.authority = &cleanupControlAuthority{h: h, actor: actor, cause: cause}
	var err error
	v.cleaner, err = NewProjectCleanup(h.pair, v.authority)
	if err != nil {
		t.Fatal(err)
	}
	h.store.row = func(ctx context.Context, tx f.Tx, query string, args []any) postgres.Row {
		v.reads++
		if v.authority.calls == 0 || len(args) != 1 || args[0] != scope.ProjectID.String() {
			return cleanupBool(false, errors.New("cleanup SQL before authority or wrong Project"))
		}
		switch {
		case strings.Contains(query, "FROM agenteam_projectvariable.project_calls"):
			return cleanupBool(v.running, v.readErr)
		case strings.Contains(query, "FROM agenteam_projectvariable.secret_references"):
			return cleanupBool(v.referenced, v.readErr)
		case strings.Contains(query, "FROM agenteam_projectvariable.history"):
			return cleanupBool(v.remaining, v.readErr)
		default:
			return cleanupBool(false, errors.New("unexpected cleanup read"))
		}
	}
	h.store.exec = func(ctx context.Context, tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
		_, tail, ok := strings.Cut(query, "DELETE FROM agenteam_projectvariable.")
		if !ok || len(args) != 1 || args[0] != scope.ProjectID.String() || !strings.Contains(query, "LIMIT 100") {
			return pgconn.CommandTag{}, errors.New("unexpected cleanup mutation")
		}
		table := strings.Fields(tail)[0]
		v.tables = append(v.tables, table)
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", v.counts[table])), v.execErr
	}
	// If an assertion fails after Unknown, resolve through the actual same
	// cleaner before the inherited pair's Stop/Drain. Never erase an owner map.
	t.Cleanup(func() {
		h.store.before, h.store.after = nil, nil
		v.authority.err, v.readErr, v.execErr = nil, nil, nil
		v.running, v.referenced, v.remaining = false, false, false
		v.cleaner.owners.mu.Lock()
		owner := v.cleaner.owners.calls[scope.ProjectID]
		retained := owner != nil && !owner.active && owner.returned != nil
		v.cleaner.owners.mu.Unlock()
		if retained {
			if _, err := v.run(context.Background()); err != nil {
				t.Error("original cleanup owner did not resolve", err)
			}
		}
	})
	return v
}
func (v *cleanupControl) run(ctx context.Context) (pc.CleanupReport, error) {
	return v.cleaner.Cleanup(ctx, v.actor, v.cause, v.scope, nil)
}
func cleanupCode(err error, code f.Code) bool {
	var p *f.Fault
	return errors.As(err, &p) && p.Code == code
}
func cleanupPrivate(err, original error) bool {
	var p privateFailure
	return !errors.Is(err, original) && errors.As(err, &p) && p.value != nil && p.value() == original
}
func requireCleanupReport(t *testing.T, v *cleanupControl, r pc.CleanupReport, err error, state pc.CleanupState) {
	t.Helper()
	if err != nil || !r.Matches(pc.SkillsParticipant, v.cause, v.scope) || r.Details().State != state || r.Details().Checkpoint != nil {
		t.Fatal("unexpected cleanup result", err, r.Details().State)
	}
}

func TestProjectVariableCleanupRequiresOriginalAuthority(t *testing.T) {
	for _, mode := range []string{"nil-pair", "unbound-pair", "nil-authority", "typed-nil-authority", "nil-context", "cancelled", "human", "archive", "checkpoint", "authority-rejected", "skipped-callback"} {
		t.Run(mode, func(t *testing.T) {
			v := newCleanupControl(t)
			ctx, actor, cause, cleaner := context.Background(), v.actor, v.cause, v.cleaner
			var cp *pc.CleanupCheckpoint
			var err error
			switch mode {
			case "nil-pair":
				_, err = NewProjectCleanup(nil, v.authority)
			case "unbound-pair":
				_, err = NewProjectCleanup(&ProjectCallOwnership{}, v.authority)
			case "nil-authority":
				_, err = NewProjectCleanup(v.h.pair, nil)
			case "typed-nil-authority":
				var a *cleanupControlAuthority
				_, err = NewProjectCleanup(v.h.pair, a)
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "human":
				actor = v.h.actor
			case "archive":
				cause.Action = pc.Archive
			case "checkpoint":
				value, e := pc.NewCleanupCheckpoint(pc.SkillsParticipant, cause, v.scope, 1, []byte(`{}`))
				if e != nil {
					t.Fatal(e)
				}
				cp = &value
			case "authority-rejected":
				v.authority.err = fault(f.Forbidden)
			case "skipped-callback":
				v.h.store.before = func(context.Context, f.TransactionCause) *f.CommitResult { r := f.CommittedResult(); return &r }
			}
			if strings.HasSuffix(mode, "pair") || strings.HasSuffix(mode, "authority") {
				if err == nil {
					t.Fatal("unbound constructor accepted")
				}
				return
			}
			r, err := cleaner.Cleanup(ctx, actor, cause, v.scope, cp)
			if err == nil || r.Matches(pc.SkillsParticipant, cause, v.scope) || v.reads != 0 || len(v.tables) != 0 || !v.h.pair.Joined() {
				t.Fatal("invalid request reached metadata or retained a completed call", err, v.reads, v.tables)
			}
		})
	}
}

func TestProjectVariableCleanupWaitsForBothProjectLanes(t *testing.T) {
	for _, mode := range []string{"ordinary-read", "ordinary-mutation", "secret-read", "secret-mutation", "registered-running", "retained-original", "other-project"} {
		t.Run(mode, func(t *testing.T) {
			v := newCleanupControl(t)
			if mode == "registered-running" {
				v.running = true
			} else if mode == "retained-original" {
				id := testID[projectCallIdentity](220)
				v.h.pair.state.owners[id] = &projectCallOwner{record: projectCallRecord{ID: id, Project: v.scope.ProjectID}, returned: true}
				t.Cleanup(func() { delete(v.h.pair.state.owners, id) })
			} else {
				begin := v.h.pair.Service().beginProject
				if strings.HasPrefix(mode, "secret") {
					begin = v.h.pair.SecretService().beginProject
				}
				kind := readCall
				if strings.HasSuffix(mode, "mutation") {
					kind = mutationCall
				}
				project := v.scope.ProjectID
				if mode == "other-project" {
					project = testID[i.Project](221)
				}
				run, _, done, err := begin(context.Background(), project, kind)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(done)
				defer func() {
					if run.Err() != nil {
						t.Error("cleanup stopped an original business call")
					}
				}()
			}
			r, err := v.run(context.Background())
			want := pc.CleanupPending
			if mode == "other-project" {
				want = pc.CleanupCompleted
			}
			requireCleanupReport(t, v, r, err, want)
			if mode != "other-project" && len(v.tables) != 0 {
				t.Fatal("cleanup crossed unreturned original", v.tables)
			}
			if mode != "registered-running" && mode != "other-project" && v.reads != 0 {
				t.Fatal("local original did not fence SQL")
			}
		})
	}
}

func TestProjectVariableCleanupBoundedMetadataOrder(t *testing.T) {
	want := []string{"history", "secret_history", "commands", "secret_commands", "variables", "project_generations", "secret_project_generations"}
	for stop, table := range want {
		t.Run(table, func(t *testing.T) {
			v := newCleanupControl(t)
			v.counts[table] = 100
			r, err := v.run(context.Background())
			requireCleanupReport(t, v, r, err, pc.CleanupPending)
			if !slices.Equal(v.tables, want[:stop+1]) {
				t.Fatal("crossed first nonempty FK batch", v.tables)
			}
			v.counts[table] = 0
			v.tables = nil
			r, err = v.run(context.Background())
			requireCleanupReport(t, v, r, err, pc.CleanupCompleted)
			if !slices.Equal(v.tables, want) {
				t.Fatal("empty confirmation did not inspect complete metadata", v.tables)
			}
		})
	}
	for _, mode := range []string{"references", "scan-error", "exec-error", "oversized-result"} {
		t.Run(mode, func(t *testing.T) {
			v := newCleanupControl(t)
			original := errors.New("private storage sentinel")
			switch mode {
			case "references":
				v.referenced = true
			case "scan-error":
				v.readErr = original
			case "exec-error":
				v.execErr = original
			case "oversized-result":
				v.counts["history"] = 101
			}
			r, err := v.run(context.Background())
			if err == nil || r.Matches(pc.SkillsParticipant, v.cause, v.scope) {
				t.Fatal("failed metadata read published result", err)
			}
			if mode == "references" && (!cleanupCode(err, f.ResourceBusy) || len(v.tables) != 0) {
				t.Fatal("references did not prevent deletion", err)
			}
			if strings.HasSuffix(mode, "error") && !cleanupPrivate(err, original) {
				t.Fatal("SQL cause was lost or made public", err)
			}
			if len(v.tables) > 1 {
				t.Fatal("continued after failed first deletion")
			}
		})
	}
}

func TestProjectVariableCleanupPreservesOriginalReturn(t *testing.T) {
	for _, mode := range []string{"scan", "exec", "store-return"} {
		t.Run(mode, func(t *testing.T) {
			v := newCleanupControl(t)
			entered, release := make(chan struct{}), make(chan struct{})
			open := ownershipRelease(release)
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report pc.CleanupReport
				err    error
			}
			done := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				open()
				if !joined {
					ownershipWait(t, done)
				}
			})
			switch mode {
			case "scan":
				v.h.store.row = func(context.Context, f.Tx, string, []any) postgres.Row {
					return cleanupControlRow(func(out ...any) error { close(entered); <-release; *out[0].(*bool) = false; return nil })
				}
			case "exec":
				v.h.store.exec = func(context.Context, f.Tx, string, []any) (pgconn.CommandTag, error) {
					close(entered)
					<-release
					return pgconn.NewCommandTag("DELETE 1"), nil
				}
			case "store-return":
				v.h.store.after = func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult {
					close(entered)
					<-release
					return f.CommittedResult()
				}
			}
			go func() { r, e := v.run(ctx); done <- outcome{r, e} }()
			ownershipWait(t, entered)
			if v.h.pair.Joined() || v.h.pair.Service().Joined() || v.h.pair.SecretService().Joined() {
				t.Fatal("one lane dropped original cleanup")
			}
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancellation replaced original return")
			default:
			}
			open()
			out := ownershipWait(t, done)
			joined = true
			if !errors.Is(out.err, context.Canceled) || out.report.Matches(pc.SkillsParticipant, v.cause, v.scope) || !v.h.pair.Joined() {
				t.Fatal("cancelled original published progress or failed to join", out.err)
			}
		})
	}
}

func TestProjectVariableCleanupRetainsUnknownUntilConfirmation(t *testing.T) {
	v := newCleanupControl(t)
	v.counts["history"] = 1
	attempt := testID[f.TransactionAttempt](222)
	var original f.CommitResult
	v.h.store.after = func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
		original = f.UnknownResult(attempt, cause)
		return original
	}
	r, err := v.run(context.Background())
	var retained commitFailure
	if !errors.As(err, &retained) || retained.result.AttemptID() != attempt || !reflect.DeepEqual(retained.result.Cause().Details(), original.Cause().Details()) || r.Matches(pc.SkillsParticipant, v.cause, v.scope) || v.h.pair.Joined() {
		t.Fatal("Unknown lost original ownership", err)
	}
	if len(v.h.store.causes) != 1 || len(v.tables) != 1 {
		t.Fatal("Unknown retried its callback")
	}
	other, e := NewProjectCleanup(v.h.pair, v.authority)
	if e != nil {
		t.Fatal(e)
	}
	wrong := v.cause
	wrong.ProjectVersion++
	if _, e = other.Cleanup(context.Background(), v.actor, wrong, v.scope, nil); !cleanupCode(e, f.InvalidState) || len(v.h.store.causes) != 1 {
		t.Fatal("second constructor bypassed retained original", e)
	}
	v.h.store.after = nil
	v.authority.err = fault(f.Forbidden)
	_, err = v.run(context.Background())
	if !errors.As(err, &retained) || retained.result.AttemptID() != attempt || v.h.pair.Joined() || len(v.tables) != 1 {
		t.Fatal("failed confirmation discarded original", err)
	}
	v.authority.err = nil
	v.remaining = true
	entered, release := make(chan struct{}), make(chan struct{})
	open := ownershipRelease(release)
	v.h.store.after = func(_ context.Context, _ f.TransactionCause, r f.CommitResult) f.CommitResult {
		close(entered)
		<-release
		return r
	}
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		open()
		if !joined {
			ownershipWait(t, done)
		}
	})
	go func() {
		report, e := other.Cleanup(context.Background(), v.actor, v.cause, v.scope, nil)
		if e == nil && report.Details().State != pc.CleanupPending {
			e = errors.New("confirmation skipped remaining metadata")
		}
		done <- e
	}()
	ownershipWait(t, entered)
	if v.h.pair.Joined() || v.h.pair.Service().Joined() || v.h.pair.SecretService().Joined() {
		t.Fatal("confirmation Store return released original too soon")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("confirmation did not await original return")
	default:
	}
	open()
	err = ownershipWait(t, done)
	joined = true
	if err != nil || !v.h.pair.Joined() || len(v.tables) != 1 {
		t.Fatal("confirmation deleted rows or retained resolved original", err, v.tables)
	}
}
