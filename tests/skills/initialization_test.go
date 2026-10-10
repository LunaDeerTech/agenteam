//go:build integration

package skill_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestSkillInitializationPersistence(t *testing.T) {
	p := newSkillPG(t)
	v := p.newCase(t)
	ctx := testContext(t)
	pending, e := v.service.InspectProjectSkills(ctx, v.actor, v.request)
	if e != nil || pending.State != pc.InitializationResultPending || !pending.Matches(v.request) {
		t.Fatal("missing original command is not pending", e)
	}
	complete, e := v.service.InitializeProjectSkills(ctx, v.actor, v.request)
	if e != nil || complete.State != pc.InitializationCompleted || !complete.Matches(v.request) {
		t.Fatal("real Skill publication failed", e)
	}
	if strings.Join(v.objects.steps, ",") != "prepare,reserve,physical,publish,discard" {
		t.Fatal("unexpected external operation sequence")
	}
	assertPublishedFacts(t, p, v, complete)
	observed, e := v.service.InspectProjectSkills(ctx, v.actor, v.request)
	if e != nil || observed.State != pc.InitializationCompleted || *observed.AddSkillsID != *complete.AddSkillsID || *observed.Revision != 1 {
		t.Fatal("durable inspection changed original publication", e)
	}

	// A fresh service has a new private confirmation issuer. Durable replay must
	// retain original IDs and must not perform another physical operation.
	fresh := p.newService(t, v.authority, v.objects)
	before := len(v.objects.steps)
	replay, e := fresh.InitializeProjectSkills(ctx, v.actor, v.request)
	if e != nil || replay.State != pc.InitializationCompleted || *replay.AddSkillsID != *complete.AddSkillsID || len(v.objects.steps) != before {
		t.Fatal("restarted initializer replayed or replaced publication", e)
	}
	plan, e := fresh.DiscoverConfirmation(ctx, v.actor, v.request)
	if e != nil {
		t.Fatal("confirmation discovery", e)
	}
	var confirmed pc.InitializationReceipt
	result := p.store.WithinTx(ctx, testCause(t), func(ctx context.Context, tx f.Tx) error {
		if e := p.store.AcquireAll(ctx, tx, plan.RequiredLocks()); e != nil {
			return e
		}
		var e error
		confirmed, e = fresh.ConfirmInitializedInTx(ctx, tx, v.actor, v.request, plan)
		return e
	})
	requireCommitted(t, result)
	if !confirmed.Matches(v.request) || confirmed.AddSkillsID != *complete.AddSkillsID || confirmed.Revision != 1 {
		t.Fatal("caller transaction confirmed another receipt")
	}

	t.Run("missing_real_parent_locks", func(t *testing.T) {
		var denied error
		result := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
			_, denied = fresh.ConfirmInitializedInTx(ctx, tx, v.actor, v.request, plan)
			return denied
		})
		if denied == nil || result.State() != f.NotCommitted {
			t.Fatal("confirmation without held locks committed")
		}
	})
	t.Run("ended_original_transaction", func(t *testing.T) {
		var ended f.Tx
		r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
			ended = tx
			return p.store.AcquireAll(ctx, tx, plan.RequiredLocks())
		})
		requireCommitted(t, r)
		if _, e := fresh.ConfirmInitializedInTx(testContext(t), ended, v.actor, v.request, plan); e == nil {
			t.Fatal("ended transaction accepted")
		}
	})
	t.Run("private_issuer_not_durable_authority", func(t *testing.T) {
		r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := p.store.AcquireAll(ctx, tx, plan.RequiredLocks()); e != nil {
				return e
			}
			_, e := v.service.ConfirmInitializedInTx(ctx, tx, v.actor, v.request, plan)
			return e
		})
		if r.State() != f.NotCommitted {
			t.Fatal("another service borrowed a confirmation issuer")
		}
	})
	assertPublishedFacts(t, p, v, complete)
	if len(v.objects.steps) != before {
		t.Fatal("confirmation/read performed physical work")
	}
}

func assertPublishedFacts(t *testing.T, p *skillPG, v *skillCase, result pc.InitializationResult) {
	t.Helper()
	var phase, skill, object string
	var skills, revisions, attempts, liveWork, joinedWork int
	e := p.store.QueryRow(testContext(t), `SELECT i.phase,i.skill_id::text,i.object_id::text,
 (SELECT count(*) FROM agenteam_skill.skills s WHERE s.project_id=i.project_id AND s.protected AND s.serving AND s.current_revision=1),
 (SELECT count(*) FROM agenteam_skill.revisions r WHERE r.project_id=i.project_id AND r.skill_id=i.skill_id AND r.object_id=i.object_id),
 (SELECT count(*) FROM agenteam_skill.object_attempts a WHERE a.project_id=i.project_id AND a.attempt_id=i.current_attempt_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id AND w.phase<>'joined'),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id AND w.phase='joined')
 FROM agenteam_skill.initializations i WHERE i.project_id=$1`, v.request.ProjectID.String()).Scan(&phase, &skill, &object, &skills, &revisions, &attempts, &liveWork, &joinedWork)
	if e != nil || phase != "published" || skill != result.AddSkillsID.String() || object != v.objects.attempt.Details().ObjectID.String() || skills != 1 || revisions != 1 || attempts != 1 || liveWork != 0 || joinedWork != 1 {
		t.Fatal("persistent Skill publication invariant failed", e)
	}
}

func TestSkillInitializationPublicationRollback(t *testing.T) {
	p := newSkillPG(t)
	v := p.newCase(t)
	v.objects.rejectPublication = true
	out, e := v.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
	var known *f.Fault
	if !errors.As(e, &known) || known.Code != f.DependencyUnavailable || out.State != "" {
		t.Fatal("failed publication reported completed", e)
	}
	var phase string
	var skills, revisions, attempts, live int
	e = p.store.QueryRow(testContext(t), `SELECT phase,
 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined')
 FROM agenteam_skill.initializations WHERE project_id=$1`, v.request.ProjectID.String()).Scan(&phase, &skills, &revisions, &attempts, &live)
	if e != nil || phase != "reserved" || skills != 0 || revisions != 0 || attempts != 1 || live != 0 {
		t.Fatal("publication failure left partial visible Skill or lost durable attempt", e)
	}
	before := len(v.objects.steps)
	pending, e := v.service.InspectProjectSkills(testContext(t), v.actor, v.request)
	if e != nil || pending.State != pc.InitializationResultPending || len(v.objects.steps) != before {
		t.Fatal("inspection retried writer or invented completion", e)
	}
	if _, e = v.service.DiscoverConfirmation(testContext(t), v.actor, v.request); e == nil {
		t.Fatal("unpublished original command yielded confirmation")
	}
}
