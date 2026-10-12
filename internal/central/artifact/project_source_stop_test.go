package artifact

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The controlled authority exercises only Artifact's original command/work
// checkpoint. The concrete Object handle and Project typed loan are PG inputs.
type artifactSourceStopStore struct {
	*artifactCallTestStore
	fixture *artifactSourceStopFixture
}

func (s *artifactSourceStopStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.artifactCallTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return artifactSourceStopExecutor{s, tx}, nil
}

type artifactSourceStopExecutor struct {
	store *artifactSourceStopStore
	tx    f.Tx
}

func (e artifactSourceStopExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("no unbounded source query")
}
func (e artifactSourceStopExecutor) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if _, err := e.store.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	v := e.store.fixture
	if strings.Contains(q, "FROM agenteam_artifact.commands ") {
		return v.audit.store.QueryRow(ctx, q, args...)
	}
	if strings.Contains(q, "source_joined_at IS NOT NULL FROM agenteam_artifact.project_work") {
		return projectAuditScan(func(out ...any) error {
			if len(args) != 7 || args[0] != v.item.call.String() || args[2] != v.item.target.String() || args[3] != v.request.Details().SourceProjectID.String() || args[4] != v.item.lease.ID().String() || args[5] != v.base.process.String() || args[6] != v.base.pair.state.instance.String() {
				return errors.New("source work lookup changed original tuple")
			}
			v.workReads++
			if v.rowErr != nil {
				return v.rowErr
			}
			if v.missing {
				return pgx.ErrNoRows
			}
			return (cleanupOwnerRow{values: []any{v.matches, v.joined}}).Scan(out...)
		})
	}
	return e.store.artifactCallTestStore.fixture.row(ctx, e.tx, q, args)
}
func (e artifactSourceStopExecutor) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if _, err := e.store.InTx(e.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	v := e.store.fixture
	if strings.HasPrefix(q, "UPDATE agenteam_artifact.project_work SET source_joined_at=") {
		if len(args) != 2 || args[0] != v.item.call.String() || args[1] != v.item.lease.ID().String() {
			return pgconn.CommandTag{}, errors.New("source checkpoint changed original tuple")
		}
		v.writes++
		if v.zeroRows {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		v.joined = true
		return pgconn.NewCommandTag("UPDATE 1"), ctx.Err()
	}
	return e.store.artifactCallTestStore.fixture.exec(ctx, e.tx, q, args)
}

type artifactSourceLoanKey struct{}
type artifactSourceStopFixture struct {
	base                                     *artifactCallFixture
	audit                                    *projectAuditFixture
	store                                    *artifactSourceStopStore
	item                                     *artifactProjectSource
	request                                  oc.ProjectSourceStopRequest
	matches, joined, missing, zeroRows, deny bool
	rowErr                                   error
	workReads, writes                        int
	selected                                 []oc.ProjectSourceStopRequest
	skipCallback                             bool
}

func (v *artifactSourceStopFixture) WithProjectSourceStop(ctx context.Context, r oc.ProjectSourceStopRequest, fn func(context.Context) error) error {
	v.selected = append(v.selected, r)
	if v.skipCallback {
		return nil
	} // Selection alone must never report completion.
	return fn(context.WithValue(ctx, artifactSourceLoanKey{}, v))
}
func (v *artifactSourceStopFixture) RequireProjectSourceStopInTx(ctx context.Context, tx f.Tx, r oc.ProjectSourceStopRequest) error {
	if v.deny || ctx.Value(artifactSourceLoanKey{}) != v || !r.Equal(v.request) {
		return failure(f.Forbidden, nil)
	}
	if _, err := v.store.InTx(tx); err != nil {
		return err
	}
	for _, p := range []i.ProjectID{r.Details().SourceProjectID, r.Details().TargetProjectID} {
		if err := v.base.store.held(ctx, tx, p); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func newArtifactSourceStopFixture(t *testing.T) *artifactSourceStopFixture {
	t.Helper()
	base, _ := newArtifactCallFixture(t)
	audit := newProjectAuditSourceFixture(t)
	v := &artifactSourceStopFixture{base: base, audit: audit, matches: true}
	v.store = &artifactSourceStopStore{artifactCallTestStore: base.store, fixture: v}
	base.state.store = v.store
	base.pair.state.sourceAuthority = v
	lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), audit.command.sourceLease)
	if err != nil {
		t.Fatal(err)
	}
	v.item = &artifactProjectSource{command: audit.command.identity, source: audit.command.source, lease: lease, target: audit.command.ref.ProjectID, call: newTestID[artifactCallID](t)}
	stop := base.stop(t, v.item.target)
	v.request, err = base.service.sourceRequest(stop.actor, stop.cause, v.item)
	if err != nil {
		t.Fatal(err)
	}
	base.state.projectSources = map[f.Digest]*artifactProjectSource{digestBytes([]byte(v.item.command.Canonical())): v.item}
	t.Cleanup(func() {
		base.store.after = nil
		v.deny = false
		v.rowErr = nil
		v.matches = true
		v.missing = false
		v.zeroRows = false
		if v.item.retained {
			if _, err := v.check(context.Background(), false); err != nil {
				t.Error("original Artifact source confirmation", err)
			}
		}
		// These entries are controlled fixture projections, not real Object
		// handles. Remove them only after all original callbacks returned; this
		// teardown does not claim production Forget or cross-package joining.
		base.state.mu.Lock()
		defer base.state.mu.Unlock()
		for key, item := range base.state.projectSources {
			if item.active || item.retained {
				t.Error("controlled source owner has not returned")
				continue
			}
			delete(base.state.projectSources, key)
		}
	})
	return v
}
func (v *artifactSourceStopFixture) check(ctx context.Context, mark bool) (bool, error) {
	var complete bool
	err := v.WithProjectSourceStop(ctx, v.request, func(ctx context.Context) (err error) {
		complete, err = v.base.service.checkProjectSource(ctx, v.request, v.item, mark)
		return err
	})
	return complete, err
}

func TestArtifactProjectSourceStopSelection(t *testing.T) {
	v := newArtifactSourceStopFixture(t)
	a := v.item.target
	b := newTestID[i.Project](t)
	makeItem := func(sourceP, target i.ProjectID) *artifactProjectSource {
		d := v.item.source.Details()
		ref := d.Reference.Details()
		ref.ProjectID = sourceP
		var err error
		d.Reference, err = oc.NewBusinessFileRef(ref)
		if err != nil {
			t.Fatal(err)
		}
		d.Owner, err = oc.NewObjectOwner(oc.Artifact, ref.ArtifactID, sourceP.String())
		if err != nil {
			t.Fatal(err)
		}
		d.Meta.Scope, err = i.InProject(sourceP)
		if err != nil {
			t.Fatal(err)
		}
		source, err := oc.NewResolvedSource(d)
		if err != nil {
			t.Fatal(err)
		}
		command, err := commandIdentity(target, newTestID[struct{}](t).String(), "create")
		if err != nil {
			t.Fatal(err)
		}
		lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), newTestID[oc.Lease](t))
		if err != nil {
			t.Fatal(err)
		}
		return &artifactProjectSource{command: command, source: source, lease: lease, target: target, call: newTestID[artifactCallID](t)}
	}
	aFromB, bFromA, onlyB := makeItem(b, a), makeItem(a, b), makeItem(b, b)
	v.base.state.projectSources = map[f.Digest]*artifactProjectSource{}
	for _, item := range []*artifactProjectSource{aFromB, bFromA, onlyB} {
		v.base.state.projectSources[digestBytes([]byte(item.command.Canonical()))] = item
	}
	v.skipCallback = true
	pending, err := v.base.service.convergeProjectSources(context.Background(), v.request.Details().Actor, v.request.Details().Cause)
	if err != nil || !pending || len(v.selected) != 2 || v.writes != 0 {
		t.Fatal("Project source selection pretended convergence", pending, err, len(v.selected))
	}
	seen := map[string]bool{}
	for _, r := range v.selected {
		seen[r.Details().Command.Canonical()] = true
	}
	if !seen[aFromB.command.Canonical()] || !seen[bFromA.command.Canonical()] || seen[onlyB.command.Canonical()] || aFromB.active || bFromA.active || onlyB.active {
		t.Fatal("source/destination filtering crossed unrelated Project B")
	}
	// An active selected source remains Pending without borrowing another loan;
	// the disjoint B item remains untouched and can be selected for B later.
	aFromB.active, bFromA.active = true, true
	v.selected = nil
	pending, err = v.base.service.convergeProjectSources(context.Background(), v.request.Details().Actor, v.request.Details().Cause)
	if err != nil || !pending || len(v.selected) != 0 || onlyB.active {
		t.Fatal("active A loan affected B", err)
	}
	aFromB.active, bFromA.active = false, false
	stopB := v.base.stop(t, b)
	pending, err = v.base.service.convergeProjectSources(context.Background(), stopB.actor, stopB.cause)
	seen = map[string]bool{}
	for _, r := range v.selected {
		seen[r.Details().Command.Canonical()] = true
	}
	if err != nil || !pending || !seen[onlyB.command.Canonical()] {
		t.Fatal("B admission was globally stopped", err)
	}
	// Archive cannot borrow a source-only relationship into another target.
	// Keep the unrelated B entry present to exercise that same selection.
	v.base.state.projectSources = map[f.Digest]*artifactProjectSource{
		digestBytes([]byte(bFromA.command.Canonical())): bFromA,
		digestBytes([]byte(onlyB.command.Canonical())):  onlyB,
	}
	archive := v.request.Details().Cause.Details()
	archive.Action = oc.ProjectStopArchive
	archiveCause, err := oc.NewProjectStopCause(archive)
	if err != nil {
		t.Fatal(err)
	}
	v.selected = nil
	pending, err = v.base.service.convergeProjectSources(context.Background(), v.request.Details().Actor, archiveCause)
	if err != nil || !pending || len(v.selected) != 0 || v.writes != 0 || bFromA.active || onlyB.active {
		t.Fatal("source-only Archive borrowed a stop loan", pending, err, len(v.selected))
	}
}

func TestArtifactProjectSourceCheckpointFacts(t *testing.T) {
	for _, mode := range []string{"read", "mark", "command-lease", "command-source", "foreign-target", "work-identity", "missing-work", "gate-denied", "without-loan", "zero-rows", "scan-error"} {
		t.Run(mode, func(t *testing.T) {
			v := newArtifactSourceStopFixture(t)
			switch mode {
			case "command-lease":
				v.audit.storedCommand.sourceLease = newTestID[oc.Lease](t)
			case "command-source":
				d := v.audit.storedCommand.source.Details()
				d.Revision++
				var err error
				v.audit.storedCommand.source, err = oc.NewResolvedSource(d)
				if err != nil {
					t.Fatal(err)
				}
			case "foreign-target":
				v.audit.storedCommand.ref.ProjectID = newTestID[i.Project](t)
			case "work-identity":
				v.matches = false
			case "missing-work":
				v.missing = true
			case "gate-denied":
				v.deny = true
			case "zero-rows":
				v.zeroRows = true
			case "scan-error":
				v.rowErr = errors.New("original source work scan failed")
			}
			var complete bool
			var err error
			if mode == "without-loan" {
				complete, err = v.base.service.checkProjectSource(context.Background(), v.request, v.item, true)
			} else {
				complete, err = v.check(context.Background(), mode != "read")
			}
			if mode == "read" || mode == "mark" {
				want := 0
				if mode == "mark" {
					want = 1
				}
				if err != nil || !complete || v.writes != want || v.workReads != 1 {
					t.Fatal("original source checkpoint not consumed", complete, err, v.writes)
				}
				return
			}
			if err == nil || complete || mode != "zero-rows" && v.writes != 0 {
				t.Fatal("invalid source facts reached checkpoint", complete, err, v.writes)
			}
			if mode == "scan-error" && !errors.Is(err, v.rowErr) {
				t.Fatal("original source work error lost", err)
			}
		})
	}
}

func TestArtifactProjectSourceUnknownConfirmation(t *testing.T) {
	for _, mark := range []bool{false, true} {
		name := "check"
		if mark {
			name = "checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			v := newArtifactSourceStopFixture(t)
			attempt := newTestID[f.TransactionAttempt](t)
			masked := false
			v.base.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if c.Details().Owner == "artifact-source-owner" && !masked && r.State() == f.Committed {
					masked = true
					return f.UnknownResult(attempt, c)
				}
				return r
			}
			complete, err := v.check(context.Background(), mark)
			if complete || !masked || !v.item.retained || v.item.original.AttemptID() != attempt {
				t.Fatal("original source Unknown not retained", err)
			}
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CauseID != attempt.String() {
				t.Fatal("Unknown attempt lost", err)
			}
			v.base.pair.Stop()
			if v.base.pair.Joined() {
				t.Fatal("retained source checkpoint escaped pair ownership")
			}
			writes := v.writes
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				open()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("original source confirmation did not return")
					}
				}
			})
			v.base.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if c.Details().Owner == "artifact-source-owner" {
					close(entered)
					<-release
				}
				return r
			}
			go func() {
				complete, err := v.check(context.Background(), true)
				if complete && err == nil {
					err = errors.New("confirmation performed a new checkpoint")
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("source confirmation original return not held")
			}
			v.base.state.mu.Lock()
			retained := v.item.retained
			v.base.state.mu.Unlock()
			if !retained || v.writes != writes || v.base.pair.Joined() {
				t.Fatal("original source confirmation wrote or released early")
			}
			open()
			select {
			case err := <-done:
				joined = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("confirmation did not return")
			}
			if v.item.retained || v.writes != writes || v.base.pair.Joined() {
				t.Fatal("readonly confirmation changed source state")
			}
			v.base.store.after = nil
			complete, err = v.check(context.Background(), true)
			if err != nil || !complete {
				t.Fatal("next explicit source checkpoint did not converge", err)
			}
			if v.base.pair.Joined() {
				t.Fatal("checkpoint replaced original Object Forget")
			}
		})
	}
}
