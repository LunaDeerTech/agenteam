package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func installRepositoryRow(t *testing.T) installationRow {
	t.Helper()
	request, err := NewInstallRequest(context.Background(), stateID[pc.Skill](503), installTestPackage(t, "Example", "repository-private-canary"))
	if err != nil {
		t.Fatal(err)
	}
	input := request.data()
	project, user := stateID[id.Project](501), stateID[id.User](502)
	semantic, err := installSemantic(project, user, input)
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.ParseInstant("2026-10-10T01:02:03Z")
	if err != nil {
		t.Fatal(err)
	}
	return installationRow{id: stateID[Installation](504), project: project, user: user, key: "original-install-key", skill: input.skill,
		revision: stateID[sc.Revision](505), semantic: semantic, pkg: freezeInstallation(input), phase: installationPlanned, version: 1, created: at, updated: at}
}

func TestInstallationDurableIdentityAndPublicationState(t *testing.T) {
	row := installRepositoryRow(t)
	if err := row.validate(); err != nil {
		t.Fatal(err)
	}
	if receipt, err := row.receipt(); err == nil || receipt.Validate() == nil {
		t.Fatal("a durable planned command is not a published Skill")
	}
	for name, alter := range map[string]func(*installationRow){
		"different-user":    func(r *installationRow) { r.user = stateID[id.User](506) },
		"different-project": func(r *installationRow) { r.project = stateID[id.Project](507) },
		"different-target":  func(r *installationRow) { r.skill = stateID[pc.Skill](508) },
		"different-name":    func(r *installationRow) { r.pkg.name = "Changed" },
		"changed-metadata":  func(r *installationRow) { r.pkg.description += " changed" },
		"half-mapping":      func(r *installationRow) { r.object = stateID[oc.StoredObject](509) },
		"false-published":   func(r *installationRow) { r.phase = installationPublished },
		"unknown-phase":     func(r *installationRow) { r.phase = "completed" },
		"unknown-reason":    func(r *installationRow) { r.phase, r.reason = installationFailed, "private failure" },
	} {
		t.Run(name, func(t *testing.T) {
			corrupt := row
			alter(&corrupt)
			if corrupt.validate() == nil {
				t.Fatal("altered original facts accepted")
			}
		})
	}
	row.phase = installationPublished
	row.object, row.upload, row.attempt = stateID[oc.StoredObject](509), stateID[oc.Upload](510), stateID[oc.Attempt](511)
	// A shape test for persisted input only; this does not claim that a physical
	// Object publication or a PostgreSQL transaction has occurred.
	receipt, err := row.receipt()
	if err != nil || receipt.SkillID != row.skill || receipt.RevisionID != row.revision || receipt.ObjectID != row.object || receipt.PackageSHA256 != row.pkg.packageDigest {
		t.Fatal("receipt must retain exact original object and package", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", row), "original-install-key") {
		t.Fatal("default formatting exposed command material")
	}
}

func TestInstallationOriginalCommandAndParentLocks(t *testing.T) {
	row := installRepositoryRow(t)
	owner, err := row.owner()
	if err != nil || owner.Details().Kind != oc.SkillRevision || owner.Details().ID != row.revision.String() || owner.Details().ProjectID != row.project.String() {
		t.Fatal("revision owner must retain its real Project", err)
	}
	command, err := row.identity()
	if err != nil {
		t.Fatal(err)
	}
	locks, err := row.locks(f.Exclusive)
	if err != nil || len(locks) != 4 {
		t.Fatal("incomplete command/user/project/skill union", err)
	}
	expected := []f.LockRequest{commandLock(command), userLock(row.user.String(), f.Exclusive), projectLock(row.project, f.Exclusive), skillLock(row.skill, f.Exclusive)}
	for _, want := range expected {
		found := false
		for _, got := range locks {
			found = found || got.Key.Canonical() == want.Key.Canonical() && got.Mode == want.Mode
		}
		if !found {
			t.Fatal("missing original lock")
		}
	}
	for n := 1; n < len(locks); n++ {
		if f.CompareLockKeys(locks[n-1].Key, locks[n].Key) >= 0 {
			t.Fatal("union is not strictly ordered")
		}
	}
}

type installationUnknownStore struct {
	Store
	calls   int
	attempt f.ID[f.TransactionAttempt]
	cause   f.TransactionCause
}

func (s *installationUnknownStore) WithinTx(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.cause = cause
	return f.UnknownResult(s.attempt, cause)
}

func TestInstallationUnknownCommitExposesNoPhysicalPlan(t *testing.T) {
	row := installRepositoryRow(t)
	request, err := NewInstallRequest(context.Background(), row.skill, installTestPackage(t, "Example", "repository-private-canary"))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := id.NewHuman(row.user, stateID[id.Session](512))
	if err != nil {
		t.Fatal(err)
	}
	// This double intentionally returns Unknown before invoking any callback;
	// it proves the no-plan/no-resend boundary, not a database commit outcome.
	store := &installationUnknownStore{attempt: stateID[f.TransactionAttempt](513)}
	authority := &Authority{data: func() *authorityState { return &authorityState{store: store} }}
	plan, err := authority.planInstallation(context.Background(), actor, row.project, row.key, request)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || plan.id.Validate() == nil || store.calls != 1 {
		t.Fatal("unknown must retain no usable publication plan", err)
	}
	actual, ok := UnknownAttempt(err)
	if !ok || actual.AttemptID() != store.attempt || actual.Cause().Details().Kind != f.CommandsCause || actual.Cause().Details().Primary.Canonical() != store.cause.Details().Primary.Canonical() || len(actual.Cause().Details().Related) != 0 {
		t.Fatal("original physical transaction identity lost")
	}
}
