//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This fixture uses the original Sources resolver, Object SourceReads and owned
// Artifact service. The source and destination are both Project A; Project B is
// independent. No SQL writes manufacture source leases or business history.
type artifactSourceStopPG struct {
	*artifactStopPG
	source      art.Metadata
	copy        art.Metadata
	ref         oc.BusinessFileRef
	lease, work string
	command     f.CommandIdentity
	owned       context.Context
	settled     bool
}

func TestProjectArtifactSourceStop(t *testing.T) {
	t.Run("original-source-acquisition-and-release-return", func(t *testing.T) { testProjectArtifactSourceStop(t, false) })
	t.Run("original-source-unknown-readonly-confirmation", func(t *testing.T) { testProjectArtifactSourceStop(t, true) })
}

func newArtifactSourceStopPG(t *testing.T, unknown bool) *artifactSourceStopPG {
	t.Helper()
	s := &artifactSourceStopPG{artifactStopPG: &artifactStopPG{}}
	s.base = newArtifactCleanupPGConfigured(t, nil, func(v *artifactCleanupPG, assembly *artifactCleanupPGAssembly) {
		s.base, s.assembly = v, assembly
		t.Cleanup(func() { s.closeSource(t) })
		authority, err := project.NewArtifactSourceStopAuthority(v.p2.lifecycle, v.p2.base.tracked)
		artifactSourceRequire(t, "bind-original-project-authority", err)
		resolver, err := artifact.NewSources(assembly.owners, v.p2.objects, assembly.audit)
		artifactSourceRequire(t, "bind-original-resolver", err)
		reads, err := object.NewSourceReadsWithProjectStop(v.p2.objects, resolver, authority)
		artifactSourceRequire(t, "bind-original-source-reads", err)
		s.pair, err = artifact.NewProjectCallOwnershipWithSourceStop(v.p2.base.tracked, assembly.owners, v.p2.objects, v.p2.objects, v.p2.objects, reads, resolver, assembly.audit, v.p2.base.keys, v.guard, v.process, v.p2.lifecycle, authority)
		artifactSourceRequire(t, "bind-original-artifact-pair", err)
		v.service = s.pair.Service()
		s.participant, err = project.NewArtifactObjectLifecycleParticipant(v.p2.lifecycle, v.service, v.p2.objects)
		artifactSourceRequire(t, "bind-fixed-participant", err)
		s.driver, err = project.NewLifecycleStopDriver(v.p2.base.tracked, v.p2.lifecycle, captureProviderProcesses{guard: v.guard}, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
			return s.step(ctx, actor, cause, scope)
		})
		artifactSourceRequire(t, "bind-original-stop-driver", err)
		s.seedSource(t)
		call, held, attempt := s.holdAcquisition(t, unknown)
		prior := assembly.stop
		assembly.stop = func(actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
			s.bindCause(t, actor, cause, scope)
			s.stopSource(t, call, held, attempt, unknown)
			prior(actor, cause, scope)
		}
	})
	return s
}

func artifactSourceRequire(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		agentRetirementDiagnostic(t, "artifact-source-"+stage, err)
		t.Fatalf("formal Artifact source step failed at %s", stage)
	}
}

func (s *artifactSourceStopPG) seedSource(t *testing.T) {
	t.Helper()
	v := s.base
	p := v.p2.project.ID
	var err error
	s.source, err = v.service.CreateFromContent(ctxFor(t), v.invocation(t, p), meta(t, "source-original", nil), art.Display{Name: "source.txt"}, "text/plain", "real source bytes\n")
	artifactSourceRequire(t, "seed-content", err)
	s.ref, err = s.source.Details().Reference.BusinessFile()
	artifactSourceRequire(t, "seed-reference", err)
	s.copy, err = v.service.CreateFromSource(ctxFor(t), v.invocation(t, p), meta(t, "source-success", nil), art.Display{Name: "source-copy.txt"}, s.ref)
	artifactSourceRequire(t, "seed-create-from-source", err)
	a, b := s.source.Details(), s.copy.Details()
	if a.Reference == b.Reference || a.Object.ID == b.Object.ID || a.Object.SHA256 != b.Object.SHA256 || a.Object.ByteSize != b.Object.ByteSize || b.Reference.ProjectID != p || b.Kind != art.Generated {
		t.Fatal("formal source publication lost the original content or reused its business/Object identity")
	}
	preview, err := v.service.ReadArtifact(ctxFor(t), v.invocation(t, p), b.Reference, art.PreviewRequest{})
	artifactSourceRequire(t, "seed-read-copy", err)
	if preview.Details().Text != "real source bytes\n" || preview.Details().Truncated {
		t.Fatal("published source copy did not expose the original complete bytes")
	}
	expectedAudit, err := au.ArtifactMetadata(au.ArtifactCreate, au.ArtifactMetadataFields{ArtifactID: b.Reference.ArtifactID.String(), ObjectID: b.Object.ID.String(), MediaType: "text/plain", ByteSize: b.Object.ByteSize, Phase: au.PublishedPhase, SourceKind: au.SourceKind(oc.ArtifactFile), SourceID: a.Reference.ArtifactID.String(), SourceRevision: a.Version})
	artifactSourceRequire(t, "seed-audit-shape", err)
	var command, audit, released bool
	artifactSourceRequire(t, "seed-original-facts", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.commands WHERE project_id=$1 AND artifact_id=$2 AND path='source' AND state='completed' AND source->>'owner'=$3 AND source->>'object'=$4),EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE project_id=$1 AND resource_id=$2 AND action='artifact.create' AND metadata=$5::jsonb),EXISTS(SELECT 1 FROM agenteam_object.object_leases l JOIN agenteam_artifact.commands c ON c.source_lease_id=l.id WHERE c.artifact_id=$2 AND l.object_id::text=$4 AND l.owner_kind='source' AND l.state='released')`, p.String(), b.Reference.ArtifactID.String(), a.Reference.ArtifactID.String(), a.Object.ID.String(), string(expectedAudit.JSON())).Scan(&command, &audit, &released))
	if !command || !audit || !released {
		t.Fatal("source publication lacked its original completed command, exact Audit or released lease", command, audit, released)
	}
	for _, value := range []art.Metadata{s.source, s.copy} {
		var native skillsCleanupPGObject
		artifactSourceRequire(t, "seed-object-facts", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text,candidate_key,byte_size FROM agenteam_object.objects WHERE id=$1 AND state='available'`, value.Details().Object.ID.String()).Scan(&native.id, &native.key, &native.size))
		v.objects = append(v.objects, native)
	}
	v.native(t, true)
	v.otherReadable(t)
}

func (s *artifactSourceStopPG) acquisition(cause f.TransactionCause) bool {
	return cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.command.Canonical()
}

func (s *artifactSourceStopPG) holdAcquisition(t *testing.T, unknown bool) (*skillsCleanupPGCall, *schedulerStopBarrier, f.ID[f.TransactionAttempt]) {
	t.Helper()
	v, store := s.base, s.base.p2.base.tracked
	var err error
	s.command, err = f.NewCommandIdentity("artifact", []string{v.p2.project.ID.String()}, "create", "source-held")
	artifactSourceRequire(t, "held-command-identity", err)
	attempt := id[f.TransactionAttempt](t)
	held := s.barrier()
	var observed atomic.Bool
	store.mu.Lock()
	store.enter = func(ctx context.Context, cause f.TransactionCause) {
		if s.acquisition(cause) {
			s.owned = ctx // Original owned business context at Store entry, not its SQL child.
		}
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if s.acquisition(cause) && observed.Load() && result.State() == f.Committed {
			held.hold(result)
		}
	}
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if unknown && s.acquisition(cause) && observed.Load() && result.State() == f.Committed {
			return f.UnknownResult(attempt, cause) // In-process acknowledgement mask only.
		}
		return result
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.acquisition(cause) {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var matches int
		var exact bool
		err = x.QueryRow(ctx, `SELECT count(*),coalesce(min(l.id::text),''),coalesce(min(w.id::text),''),coalesce(bool_and(l.id IS NOT NULL AND w.id IS NOT NULL AND c.state='pending' AND c.path='source' AND l.object_id=$3 AND l.state='active' AND l.owner_kind='source' AND l.owner_id=l.id AND l.process_id=$4 AND w.process_id=$4 AND w.instance_id IS NOT NULL AND w.source_project_id=c.project_id AND w.source_lease_id=l.id AND w.joined_at IS NULL AND w.source_joined_at IS NULL),false) FROM agenteam_artifact.commands c LEFT JOIN agenteam_object.object_leases l ON l.id=c.source_lease_id LEFT JOIN agenteam_artifact.project_work w ON w.command_hash=c.command_hash WHERE c.project_id=$1 AND c.command_key=$2`, v.p2.project.ID.String(), s.command.Key().String(), s.source.Details().Object.ID.String(), v.process.String()).Scan(&matches, &s.lease, &s.work, &exact)
		// The original lookup uses the same CommandsCause before acquisition.
		// Only an absent command may pass without the original lease/work tuple.
		if err == nil && matches == 0 {
			return nil
		}
		if err == nil && (matches != 1 || !exact) {
			return errors.New("original source acquisition lost its command/lease/work tuple")
		}
		if err == nil {
			observed.Store(true)
		}
		return err
	})
	invocation, ref := v.invocation(t, v.p2.project.ID), s.ref
	command := meta(t, "source-held", nil)
	call := s.start(func(ctx context.Context) error {
		_, err := v.service.CreateFromSource(ctx, invocation, command, art.Display{Name: "held-copy.txt"}, ref)
		return err
	})
	s.awaitHeld(t, call, held)
	if !observed.Load() || s.lease == "" || s.work == "" || s.owned == nil || s.owned.Err() != nil {
		t.Fatal("original source acquisition did not hold its live owned context and exact committed tuple")
	}
	return call, held, attempt
}

func (s *artifactSourceStopPG) facts(t *testing.T, lease string, objectJoined, callJoined, sourceJoined bool) {
	t.Helper()
	v := s.base
	var state string
	var objectDone, callDone, sourceDone, published, exact bool
	artifactSourceRequire(t, "observe-original-source-facts", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT l.state,o.joined_at IS NOT NULL,w.joined_at IS NOT NULL,w.source_joined_at IS NOT NULL,EXISTS(SELECT 1 FROM agenteam_artifact.artifacts a WHERE a.id=c.artifact_id),c.path='source' AND c.state='pending' AND c.project_id=$1 AND c.source_lease_id=l.id AND l.object_id=$4 AND l.process_id=$5 AND o.process_id=$5 AND w.process_id=$5 AND w.source_project_id=c.project_id AND w.source_lease_id=l.id FROM agenteam_object.object_leases l JOIN agenteam_object.project_work o ON o.id=l.id JOIN agenteam_artifact.project_work w ON w.id=$3 JOIN agenteam_artifact.commands c ON c.command_hash=w.command_hash WHERE l.id=$2`, v.p2.project.ID.String(), s.lease, s.work, s.source.Details().Object.ID.String(), v.process.String()).Scan(&state, &objectDone, &callDone, &sourceDone, &published, &exact))
	if state != lease || objectDone != objectJoined || callDone != callJoined || sourceDone != sourceJoined || published || !exact {
		t.Fatal("original source state/return facts differ", state, objectDone, callDone, sourceDone, published, exact)
	}
}

func (s *artifactSourceStopPG) sourceSnapshot(t *testing.T) string {
	t.Helper()
	var out string
	artifactSourceRequire(t, "source-snapshot", s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(l) FROM agenteam_object.object_leases l WHERE id=$1),(SELECT to_jsonb(w) FROM agenteam_object.project_work w WHERE id=$1),(SELECT to_jsonb(w) FROM agenteam_artifact.project_work w WHERE id=$2),(SELECT to_jsonb(c) FROM agenteam_artifact.commands c WHERE project_id=$3 AND command_key=$4))::text`, s.lease, s.work, s.base.p2.project.ID.String(), string(s.command.Key())).Scan(&out))
	return out
}

func (s *artifactSourceStopPG) request() (*skillsCleanupPGCall, *oc.ObjectStopReport) {
	report := new(oc.ObjectStopReport)
	call := s.run(func(ctx context.Context, actor i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef) (err error) {
		*report, err = s.base.service.RequestProjectStop(ctx, actor, s.downstream)
		return err
	})
	return call, report
}

func (s *artifactSourceStopPG) requireReport(t *testing.T, report *oc.ObjectStopReport, state oc.ProjectStopState) {
	t.Helper()
	if !report.Matches(oc.ArtifactStopComponent, s.downstream) || report.Details().State != state {
		t.Fatal("original source Artifact stop report differs", report.Details().State)
	}
}

func (s *artifactSourceStopPG) stopSource(t *testing.T, original *skillsCleanupPGCall, acquisition *schedulerStopBarrier, attempt f.ID[f.TransactionAttempt], unknown bool) {
	t.Helper()
	call, report := s.request()
	s.await(t, call)
	s.requireReport(t, report, oc.ProjectStopPending)
	if !errors.Is(s.owned.Err(), context.Canceled) {
		t.Fatal("confirmed Project Stop did not cancel the original owned creation context")
	}
	s.facts(t, "active", false, false, false)
	select {
	case <-original.done:
		t.Fatal("held acquisition returned before its original Store return")
	default:
	}
	s.base.otherReadable(t)
	s.clearHooks()
	if unknown {
		acquisition.open()
		outboxCleanupWait(t, original.done, "original source acquisition acknowledgement")
		s.requireUnknown(t, original.err, attempt, "acquisition")
		s.facts(t, "active", false, true, false)
		s.confirmSource(t, "active")
		s.releaseUnknown(t)
	} else {
		s.normalReleaseReturn(t, original, acquisition)
	}
	call, report = s.request()
	s.await(t, call)
	s.requireReport(t, report, oc.ProjectStopped)
	s.facts(t, "released", true, true, true)
	s.completeObjects(t)
	s.settled = true
	s.base.native(t, true)
	s.base.otherReadable(t)
}

func (s *artifactSourceStopPG) normalReleaseReturn(t *testing.T, original *skillsCleanupPGCall, acquisition *schedulerStopBarrier) {
	t.Helper()
	store := s.base.p2.base.tracked
	held := s.barrier()
	var selected string
	var mu sync.Mutex
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.RecoveryCause || cause.Details().Owner != "object" {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if selected != "" {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var released bool
		if err = x.QueryRow(ctx, `SELECT state='released' FROM agenteam_object.object_leases WHERE id=$1`, s.lease).Scan(&released); err != nil {
			return err
		}
		if released {
			selected = cause.Details().RecoveryRunID
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		mu.Lock()
		match := selected != "" && cause.Kind() == f.RecoveryCause && cause.Details().Owner == "object" && cause.Details().RecoveryRunID == selected
		mu.Unlock()
		if match && result.State() == f.Committed {
			held.hold(result)
		}
	}
	store.mu.Unlock()
	acquisition.open()
	s.awaitHeld(t, original, held)
	s.facts(t, "released", false, false, false)
	call, report := s.request()
	s.await(t, call)
	s.requireReport(t, report, oc.ProjectStopPending)
	select {
	case <-original.done:
		t.Fatal("source release visibility replaced its original public/Store return")
	default:
	}
	s.base.otherReadable(t)
	held.open()
	outboxCleanupWait(t, original.done, "original cancelled source creation and release tail")
	if original.err == nil {
		t.Fatal("cancelled original source creation unexpectedly published a target")
	}
	s.clearHooks()
	s.facts(t, "released", true, true, true)
}

func (s *artifactSourceStopPG) exactSourceCause(cause f.TransactionCause, owner string) bool {
	d := cause.Details()
	return d.Kind == f.RecoveryCause && d.Owner == owner && d.RecoveryRunID == s.lease && d.CheckpointRef == s.cause.OperationID.String()
}

func (s *artifactSourceStopPG) requireUnknown(t *testing.T, err error, attempt f.ID[f.TransactionAttempt], stage string) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != attempt.String() {
		agentRetirementDiagnostic(t, "artifact-source-"+stage+"-unknown", err)
		t.Fatal("original source acknowledgement lost its first Unknown attempt")
	}
}

func (s *artifactSourceStopPG) releaseUnknown(t *testing.T) {
	t.Helper()
	store := s.base.p2.base.tracked
	attempt := id[f.TransactionAttempt](t)
	held := s.barrier()
	var masked atomic.Bool
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if s.exactSourceCause(cause, "artifact-source-release") && result.State() == f.Committed {
			held.hold(result)
		}
	}
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if s.exactSourceCause(cause, "artifact-source-release") && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	store.mu.Unlock()
	call, report := s.request()
	s.awaitHeld(t, call, held)
	s.facts(t, "released", false, true, false)
	select {
	case <-call.done:
		t.Fatal("typed source release retired its original loan before Store return")
	default:
	}
	s.base.otherReadable(t)
	held.open()
	outboxCleanupWait(t, call.done, "original typed source release acknowledgement")
	if !masked.Load() {
		t.Fatal("typed release did not reach its original committed result")
	}
	s.requireUnknown(t, call.err, attempt, "release")
	s.requireReport(t, report, oc.ProjectStopPending)
	s.clearHooks()
	s.facts(t, "released", false, true, false)
	s.confirmSource(t, "released")
}

func (s *artifactSourceStopPG) confirmSource(t *testing.T, leaseState string) {
	t.Helper()
	store := s.base.p2.base.tracked
	before := s.sourceSnapshot(t)
	var baseline sync.Map
	var delta atomic.Int64
	delta.Store(-1)
	held := s.barrier()
	store.mu.Lock()
	store.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.exactSourceCause(cause, "artifact-source-confirm") {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var n int64
		if err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN('agenteam_object','agenteam_artifact')`).Scan(&n); err == nil {
			baseline.Store(tx, n)
		}
		return err
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if s.exactSourceCause(cause, "artifact-source-confirm") && result.State() == f.Committed {
			held.hold(result)
		}
	}
	store.mu.Unlock()
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !s.exactSourceCause(cause, "artifact-source-confirm") {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var n int64
		if err = x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN('agenteam_object','agenteam_artifact')`).Scan(&n); err != nil {
			return err
		}
		start, ok := baseline.LoadAndDelete(tx)
		if !ok || n < start.(int64) {
			return errors.New("missing original source confirmation transaction baseline")
		}
		delta.Store(n - start.(int64))
		return nil
	})
	call, report := s.request()
	s.awaitHeld(t, call, held)
	if delta.Load() != 0 || s.sourceSnapshot(t) != before {
		t.Fatal("source confirmation wrote or changed the original command/lease/work", delta.Load())
	}
	s.facts(t, leaseState, false, true, false)
	select {
	case <-call.done:
		t.Fatal("source confirmation substituted visibility for original return")
	default:
	}
	s.base.otherReadable(t)
	held.open()
	s.await(t, call)
	s.requireReport(t, report, oc.ProjectStopPending)
	if s.sourceSnapshot(t) != before {
		t.Fatal("same invocation continued into release/join after confirmation")
	}
	s.clearHooks()
	t.Logf("PROJECT_ARTIFACT_SOURCE_CONFIRMATION original_lease_state=%s original_tx_writes=%d original_facts_unchanged=true", leaseState, delta.Load())
}

// Object's fixed scheduler consumes the one real issued download on lane 3.
// Each next call is justified by the preceding returned Pending and cursor.
func (s *artifactSourceStopPG) completeObjects(t *testing.T) {
	t.Helper()
	v := s.base
	var existing, grants, pending int
	artifactSourceRequire(t, "object-prefix", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_object.project_stops WHERE project_id=$1),(SELECT count(*) FROM agenteam_download.grants WHERE project_id=$1 AND NOT revoked),(SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1 AND joined_at IS NULL)`, v.p2.project.ID.String()).Scan(&existing, &grants, &pending))
	if existing != 0 || grants != 1 || pending != 0 {
		t.Fatal("source return did not leave the expected original Object stop inputs", existing, grants, pending)
	}
	for lane := 0; lane <= 3; lane++ {
		want := pc.StopPending
		if lane == 3 {
			want = pc.Stopped
		}
		s.observe(t, false, want)
		var state, after string
		var next int
		artifactSourceRequire(t, "object-returned-scan", v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.state,p.scan_kind,COALESCE(p.scan_after::text,''),(SELECT count(*) FROM agenteam_download.grants g WHERE g.project_id=p.project_id AND NOT g.revoked) FROM agenteam_object.project_stops p WHERE p.project_id=$1 AND p.operation_id=$2`, v.p2.project.ID.String(), s.cause.OperationID.String()).Scan(&state, &next, &after, &grants))
		if lane < 3 && (state != "stopping" || next != lane+1 || after != "" || grants != 1) || lane == 3 && (state != "stopped" || next != 0 || after != "" || grants != 0) {
			t.Fatal("real Object scan departed from its original fixed lane", lane, state, next, grants)
		}
	}
}

func testProjectArtifactSourceStop(t *testing.T, unknown bool) {
	s := newArtifactSourceStopPG(t, unknown)
	if !s.settled {
		t.Fatal("source stop did not return through both actual components")
	}
	s.facts(t, "released", true, true, true)
	// This slice proves source return and Stop, not removal of source history.
	var commands, sourceWork int
	artifactSourceRequire(t, "retained-source-history", s.base.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_artifact.commands WHERE project_id=$1 AND path='source'),(SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1 AND source_lease_id IS NOT NULL)`, s.scope.ProjectID.String()).Scan(&commands, &sourceWork))
	if commands != 2 || sourceWork != 2 {
		t.Fatal("source Stop erased durable command/work history", commands, sourceWork)
	}
	s.closeSource(t)
	if unknown {
		t.Log("PROJECT_ARTIFACT_SOURCE_UNKNOWN real_source_publication=true acquisition_local_ack_unknown=true typed_release_original_return_held=true typed_release_local_ack_unknown=true two_original_readonly_confirmations=true next_explicit_join=true other_project_live=true no_network_or_foreign_death_proof=true")
	} else {
		t.Log("PROJECT_ARTIFACT_SOURCE_RETURN real_source_publication=true original_acquisition_store_held=true original_owned_context_canceled=true original_release_store_held=true actual_public_and_source_return=true actual_artifact_object_stop=true other_project_live=true source_history_retained=true")
	}
}

func (s *artifactSourceStopPG) closeSource(t *testing.T) {
	t.Helper()
	if s.closed {
		return
	}
	for _, b := range s.barriers {
		b.open()
	}
	for _, call := range s.calls {
		outboxCleanupWait(t, call.done, "original source call before shutdown")
	}
	s.clearHooks()
	if !s.settled && s.driver != nil && s.downstream.Validate() == nil {
		// A failed assertion must still let original Unknown/Close owners return
		// before global Stop. These are explicit bounded Pending continuations.
		for round := 0; round < 3; round++ {
			call, report := s.request()
			outboxCleanupWait(t, call.done, "original source recovery before global Stop")
			if call.err != nil {
				t.Error("original source owner remains retained", call.err)
				break
			}
			if report.Matches(oc.ArtifactStopComponent, s.downstream) && report.Details().State == oc.ProjectStopped {
				break
			}
			if !report.Matches(oc.ArtifactStopComponent, s.downstream) || report.Details().State != oc.ProjectStopPending {
				t.Error("invalid original source recovery report")
				break
			}
		}
	}
	s.artifactStopPG.close(t)
}
