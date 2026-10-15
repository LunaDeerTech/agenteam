package project

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type meetingDeletionProof struct {
	fixture *auditGateFixture
	actor   i.Actor
	denied  bool
	binds   int
	checks  int
}

func (p *meetingDeletionProof) RequireMeetingDeletionBindingInTx(_ context.Context, tx f.Tx, store any) error {
	p.binds++
	if p.denied || tx != p.fixture.store.tx || store != p.fixture.store {
		return fault(f.Forbidden)
	}
	return nil
}
func (p *meetingDeletionProof) RequireMeetingCleanupInTx(_ context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, owner i.UserID) error {
	p.checks++
	if p.denied || tx != p.fixture.store.tx || !actor.Equal(p.actor) || project != p.fixture.project || owner.String() != p.fixture.actor.Details().UserID {
		return fault(f.Forbidden)
	}
	return nil
}

func meetingDeletionGate(t *testing.T) (*auditGateFixture, *MeetingDeletionAccess, *meetingDeletionProof) {
	t.Helper()
	v := newAuditGateFixture(t, nil)
	scope, _ := i.InProject(v.project)
	registration, _ := i.RegisterService(i.MeetingRuntime)
	actor, err := registration.Actor(testID[struct{}](t).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	proof := &meetingDeletionProof{fixture: v, actor: actor}
	gate, err := NewMeetingDeletionAccess(v.a, proof)
	if err != nil {
		t.Fatal(err)
	}
	return v, gate, proof
}

func TestMeetingDeletionProjectConvergesAfterArchiveWithoutLaunchGrant(t *testing.T) {
	v, gate, proof := meetingDeletionGate(t)
	for _, state := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
		v.lifecycle, v.order = state, nil
		if err := gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project); err != nil {
			t.Fatal("accepted deletion could not converge before audit fence", state, err)
		}
		for _, stage := range v.order {
			if stage == "session" {
				t.Fatal("cleanup impersonated the accepting Human Session")
			}
		}
	}
	if proof.binds != 4 || proof.checks != 4 {
		t.Fatal("current Project facts replaced private original deletion proof")
	}
	v.lifecycle = c.Archived
	if _, err := v.a.RequirePreparingProjectInTx(context.Background(), v.store.tx, v.project); err == nil {
		t.Fatal("cleanup grant changed generic new-execution admission")
	}
	proof.denied = true
	if err := gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project); err == nil {
		t.Fatal("technical Actor became a deletion grant")
	}
}

func TestMeetingDeletionProjectKeepsCurrentAuditPhaseFence(t *testing.T) {
	v, gate, proof := meetingDeletionGate(t)
	v.lifecycle = c.Deleting
	base := v.store.row
	stage := "domains"
	v.store.row = func(query string, args ...any) postgres.Row {
		if strings.Contains(query, "FROM agenteam_project.lifecycle_operations") {
			return valuesRow(v.project.String(), v.actor.Details().UserID, string(c.Delete), int64(1), string(c.OperationCleaning), "", stage)
		}
		return base(query, args...)
	}
	for _, next := range []string{"domains", "outbox", "audit", "final"} {
		stage = next
		err := gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project)
		if next == "audit" || next == "final" {
			hasCode(t, err, f.InvalidState)
		} else if err != nil {
			t.Fatal("pre-audit original deletion refused", next, err)
		}
	}
	if proof.checks != 4 {
		t.Fatal("phase gate skipped original Meeting facts")
	}
}

func TestMeetingDeletionProjectRejectsForeignTransactionActorAndMissingFacts(t *testing.T) {
	v, gate, proof := meetingDeletionGate(t)
	for _, wrong := range []struct {
		actor i.Actor
		tx    f.Tx
		p     i.ProjectID
	}{{v.actor, v.store.tx, v.project}, {proof.actor, f.NewTx(), v.project}, {proof.actor, v.store.tx, testID[i.Project](t)}} {
		before := proof.checks
		if err := gate.RequireMeetingDeletionProjectInTx(context.Background(), wrong.tx, wrong.actor, wrong.p); err == nil || proof.checks != before {
			t.Fatal("foreign deletion reached original facts")
		}
	}
	v.initialized = false
	if err := gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project); err == nil {
		t.Fatal("uninitialized Project used cleanup gate")
	}
	v.initialized = true
	v.store.row = func(string, ...any) postgres.Row { return rowFunc(func(...any) error { return pgx.ErrNoRows }) }
	hasCode(t, gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project), f.NotFound)
	v.store.lockErr = errors.New("missing original Project EX lock")
	before := proof.binds
	if err := gate.RequireMeetingDeletionProjectInTx(context.Background(), v.store.tx, proof.actor, v.project); err == nil || proof.binds != before {
		t.Fatal("missing lock reached domain authority")
	}
}
