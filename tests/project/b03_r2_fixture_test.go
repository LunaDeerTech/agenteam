//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type r2Participant struct {
	name  c.ParticipantName
	calls atomic.Int64
}

func (p *r2Participant) Name() c.ParticipantName { return p.name }
func (p *r2Participant) RequestStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("R2 must not call RequestStop")
}
func (p *r2Participant) InspectStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("R2 must not call InspectStop")
}
func (p *r2Participant) Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	p.calls.Add(1)
	return c.CleanupReport{}, errors.New("R2 must not call Cleanup")
}
func r2Entries(version foundation.Version, skills bool) []c.ParticipantRegistration {
	entries := []c.ParticipantRegistration{
		{Name: c.ArtifactObjectParticipant, ContractVersion: version, OwnerModule: "artifact-object", ReferenceKinds: []c.ReferenceKind{"object"}},
		{Name: c.SecretParticipant, ContractVersion: version, OwnerModule: "secret", ReferenceKinds: []c.ReferenceKind{"secret"}},
		{Name: c.OutboxParticipant, ContractVersion: version, OwnerModule: "outbox", CleanupAfter: []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant}},
		{Name: c.AuditParticipant, ContractVersion: version, OwnerModule: "audit", CleanupAfter: []c.ParticipantName{c.OutboxParticipant}},
	}
	if skills {
		entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: version, OwnerModule: "skills"})
		entries[2].CleanupAfter = append(entries[2].CleanupAfter, c.SkillsParticipant)
	}
	return entries
}
func r2Registry(t *testing.T, current []c.ParticipantRegistration, retained ...c.ParticipantRegistration) *project.LifecycleRegistry {
	t.Helper()
	manifest, err := c.NewRequiredManifest(current)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []project.LifecycleParticipantBinding{}
	for _, entry := range append(append([]c.ParticipantRegistration{}, current...), retained...) {
		adapter := &r2Participant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: adapter})
		t.Cleanup(func() {
			if adapter.calls.Load() != 0 {
				t.Errorf("acceptance invoked participant %s", adapter.name)
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

type r2Fixture struct {
	*b02Fixture
	registry *project.LifecycleRegistry
}

func newR2Fixture(t *testing.T) *r2Fixture {
	t.Helper()
	f := &r2Fixture{b02Fixture: newFixture(t), registry: r2Registry(t, r2Entries(1, false))}
	f.service = f.lifecycleService(t, nil)
	return f
}
func (f *r2Fixture) lifecycleService(t *testing.T, change func(*project.Dependencies)) *project.Service {
	t.Helper()
	d := project.Dependencies{Authority: f.authority, Activity: f.accounts, Audit: f.aud, Events: f.events, ProjectEvents: f.typed, Initializer: f.skills, Processes: f.process, Cursors: f.keys, LifecycleRegistry: f.registry}
	if change != nil {
		change(&d)
	}
	s, err := project.New(f.store, d, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}

type r2Intent struct {
	project      c.ProjectRef
	command      c.CommandName
	meta         foundation.CommandMeta
	confirmation c.DeleteProjectRequest
}

func newR2Intent(t *testing.T, p c.ProjectRef, command c.CommandName, key string) r2Intent {
	t.Helper()
	v := p.Version
	return r2Intent{project: p, command: command, meta: meta(t, key, &v), confirmation: c.DeleteProjectRequest{NormalizedCurrentPath: "owner-alpha/" + p.NormalizedName, Permanent: true}}
}
func (i r2Intent) invoke(ctx context.Context, s *project.Service, actor identity.Actor) (c.LifecycleResult, error) {
	if i.command == c.ArchiveCommand {
		op, err := s.BeginArchive(ctx, actor, i.meta, i.project.ID)
		if err != nil {
			return c.LifecycleResult{}, err
		}
		return c.LifecycleResult{Operation: &op}, nil
	}
	return s.BeginDeleteProject(ctx, actor, i.meta, i.project.ID, i.confirmation)
}
func (i r2Intent) lookup() c.CommandLookupRequest {
	return c.CommandLookupRequest{ProjectID: i.project.ID, Command: i.command, Key: i.meta.IdempotencyKey}
}

type r2Snapshot struct {
	Gate                                                      string
	Version                                                   int64
	Operations, Participants, Audits, Events, Receipts, Plans int
	Activity                                                  time.Time
}

func (f *r2Fixture) snapshot(t *testing.T, p c.ProjectID, actor identity.Actor) r2Snapshot {
	t.Helper()
	var s r2Snapshot
	err := f.raw.QueryRow(ctxFor(t), `SELECT lifecycle,version,(SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$1),(SELECT count(*) FROM agenteam_project.lifecycle_participants p JOIN agenteam_project.lifecycle_operations o ON o.id=p.operation_id WHERE o.project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action IN ('project.archive.accepted','project.delete.accepted')),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.lifecycle_changed'),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name IN ('archive','delete') AND state='completed'),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name IN ('archive','delete') AND state='planned'),(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2) FROM agenteam_project.projects WHERE id=$1`, p.String(), actor.Details().SessionID).Scan(&s.Gate, &s.Version, &s.Operations, &s.Participants, &s.Audits, &s.Events, &s.Receipts, &s.Plans, &s.Activity)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (f *r2Fixture) resetActivity(t *testing.T, actor identity.Actor) time.Time {
	t.Helper()
	var at time.Time
	err := f.raw.QueryRow(ctxFor(t), `UPDATE agenteam_account.sessions SET last_activity_at=clock_timestamp()-interval '90 seconds' WHERE id=$1 RETURNING last_activity_at`, actor.Details().SessionID).Scan(&at)
	if err != nil {
		t.Fatal(err)
	}
	return at
}
func (f *r2Fixture) assertNoAcceptance(t *testing.T, i r2Intent, actor identity.Actor, before time.Time) {
	t.Helper()
	s := f.snapshot(t, i.project.ID, actor)
	if s.Gate != string(i.project.Lifecycle) || s.Version != int64(i.project.Version) || s.Operations != 0 || s.Participants != 0 || s.Audits != 0 || s.Events != 0 || s.Receipts != 0 || !s.Activity.Equal(before) {
		t.Fatalf("partial lifecycle acceptance: %+v", s)
	}
}
func (f *r2Fixture) assertAccepted(t *testing.T, i r2Intent, actor identity.Actor, result c.LifecycleResult, manifest c.RequiredManifest) {
	t.Helper()
	if result.Validate() != nil || result.Operation == nil || result.Operation.State != c.OperationAccepted || result.Operation.Version != 1 || result.Operation.ProjectVersion != i.project.Version+1 {
		t.Fatalf("not an accepted operation: %+v", result.Operation)
	}
	wantGate := "archiving"
	if i.command == c.DeleteCommand {
		wantGate = "deleting"
	}
	s := f.snapshot(t, i.project.ID, actor)
	if s.Gate != wantGate || s.Version != int64(i.project.Version+1) || s.Operations != 1 || s.Participants != len(manifest.Entries()) || s.Audits != 1 || s.Events != 1 || s.Receipts != 1 || s.Plans != 0 {
		t.Fatalf("acceptance facts: %+v", s)
	}
	var op, eventID, plannedEventID, manifestDigest string
	var rawManifest []byte
	err := f.raw.QueryRow(ctxFor(t), `SELECT p.current_lifecycle_operation_id::text,o.required_manifest,o.manifest_digest,c.event_id::text,e.id::text FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.commands c ON c.project_id=p.id AND c.command_name=$2 AND c.key=$3 JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE p.id=$1`, i.project.ID.String(), string(i.command), string(i.meta.IdempotencyKey)).Scan(&op, &rawManifest, &manifestDigest, &plannedEventID, &eventID)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, _ := manifest.Digest()
	var entries []c.ParticipantRegistration
	if json.Unmarshal(rawManifest, &entries) != nil {
		t.Fatal("stored manifest decode")
	}
	stored, err := c.NewRequiredManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	actualDigest, _ := stored.Digest()
	if op != result.Operation.ID.String() || plannedEventID != eventID || manifestDigest != wantDigest.String() || actualDigest != wantDigest {
		t.Fatal("original operation/event/manifest identity changed")
	}
}

type r2FailPrepare struct{ oc.Appender }

func (r2FailPrepare) PrepareAppend(context.Context, identity.Actor, event.Event) (oc.AppendPlan, error) {
	return oc.AppendPlan{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
}

type r2FailActivity struct{ project.ActivityAuthority }

func (a r2FailActivity) TouchActivityInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if err := a.ActivityAuthority.TouchActivityInTx(ctx, tx, actor); err != nil {
		return err
	}
	return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
}

type r2PrepareBarrier struct {
	oc.Appender
	reached, release chan struct{}
	once             sync.Once
}

func (b *r2PrepareBarrier) PrepareAppend(ctx context.Context, actor identity.Actor, ev event.Event) (oc.AppendPlan, error) {
	plan, err := b.Appender.PrepareAppend(ctx, actor, ev)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	b.once.Do(func() { close(b.reached) })
	select {
	case <-b.release:
		return plan, nil
	case <-ctx.Done():
		return oc.AppendPlan{}, ctx.Err()
	}
}
func r2RequireSQLState(t *testing.T, err error, state string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != state {
		t.Fatalf("SQLSTATE = %v, want %s", err, state)
	}
}
func r2ObserveWaiter(t *testing.T, f *r2Fixture, holder int32, key foundation.LockKey, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	hash := uint64(key.AdvisoryKey())
	for {
		var blocked bool
		err := f.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid WHERE w.locktype='advisory' AND w.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND NOT w.granted AND w.mode=$1 AND h.granted AND h.pid=$2 AND h.pid=ANY(pg_blocking_pids(w.pid)) AND w.classid::bigint=$3 AND w.objid::bigint=$4)`, mode, holder, int64(uint32(hash>>32)), int64(uint32(hash))).Scan(&blocked)
		if err != nil {
			t.Fatal("exact holder/waiter", err)
		}
		if blocked {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("exact lifecycle waiter not observed")
		}
	}
}
func r2SafeError(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return fmt.Sprintf("%v SQLSTATE=%s", err, pg.Code)
	}
	return fmt.Sprint(err)
}
