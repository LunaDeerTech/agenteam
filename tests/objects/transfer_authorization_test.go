//go:build integration

package objects_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestTransferFixedGETReplayRequiresCurrentProtectedUse(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "fixed-input", "old fixed input")
	use := id[struct{}](t)
	owner, _ := oc.NewLeaseOwner(oc.HistoryOwner, use.String())
	f.sql(t, `INSERT INTO object_fixture.uses(id,object_id,kind,owner_id) VALUES($1,$2,'history',$3)`, use.String(), put.Meta.ID.String(), f.owner.Details().ID)
	var lease oc.ObjectLease
	plan := leasePlan(t, f.service, f.actor, put.Meta.ID, owner, oc.AcquireUseAccess)
	result := plannedTx(f.store, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		var err error
		lease, err = f.service.AcquireLeaseInTx(ctx, tx, f.actor, put.Meta.ID, owner, plan, locked)
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	f.sql(t, `UPDATE object_fixture.uses SET lease_id=$1 WHERE id=$2`, lease.ID.String(), use.String())
	plan = ownerPlan(t, f.service, f.actor, f.owner, oc.ReleaseAccess, oc.AccessRequestDetails{ObjectID: put.Meta.ID})
	result = plannedTx(f.store, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		return f.service.ReleaseObjectInTx(ctx, tx, f.actor, f.owner, put.Meta.ID, plan, locked)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Owner: f.owner, Direction: oc.TransferGET, ObjectID: put.Meta.ID})
	issue := command(t, "fixed-get")
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `UPDATE object_fixture.uses SET active=false WHERE id=$1`, use.String())
	replay, err := f.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	requireCode(t, err, foundation.Forbidden)
	if _, err = replay.Material.ForRunner(f.runner, grant.Status.ID); err == nil {
		t.Fatal("transfer lease substituted for current fixed-use permission")
	}
	var active int
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, put.Meta.ID.String()).Scan(&active); err != nil || active != 2 {
		t.Fatal("denial changed independent protecting leases", active, err)
	}
}

func TestTransferProjectArchiveDuringSourceReadPreventsNewCandidate(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := bytes.Repeat([]byte("a"), 256<<10)
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "archive-during-complete"), f.spec(t, body))
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, material, body); status != 200 {
		t.Fatal(status)
	}
	proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	proxy.mode.Store(transferHoldStageRead)
	done := make(chan error, 1)
	go func() {
		_, err := through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
		done <- err
	}()
	proxy.wait(t)
	key, _ := foundation.ProjectLock(f.project.String())
	result := f.store.WithinTx(contextFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
		e, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE object_fixture.projects SET state='archived' WHERE id=$1`, f.project.String())
		return err
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	proxy.unblock()
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("source did not join after archive")
	}
	requireCode(t, err, foundation.ProjectNotActive)
	var available, externalActive bool
	if err = f.store.QueryRow(contextFor(t), `SELECT o.state='available',l.state='active' FROM agenteam_object.object_transfers t JOIN agenteam_object.objects o ON o.id=t.object_id JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.id=$1`, grant.Status.ID.String()).Scan(&available, &externalActive); err != nil || available || !externalActive || proxy.candidatePUT.Load() != 0 {
		t.Fatal("archive allowed candidate/publish or retired unproven external lease", err)
	}
}

func TestTransferCurrentOwnerPrecedesHistoricalReplay(t *testing.T) {
	f := newTransferFixture(t)
	proxy := newTransferProxy(t, f.fixture)
	through := transferOn(t, f, f.store, proxy.config(t, f.fixture))
	body := []byte("current owner before any historical response")
	spec := f.spec(t, body)
	issue := command(t, "owner-replay")
	grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, issue, spec)
	if err != nil {
		t.Fatal(err)
	}
	material, _ := grant.Material.ForRunner(f.runner, grant.Status.ID)
	if status, _ := transferRequest(t, through, material, body); status != 200 {
		t.Fatal(status)
	}
	completed := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	if _, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed); err != nil {
		t.Fatal(err)
	}
	cancel := command(t, "owner-cancel")
	if _, err = through.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, cancel); err != nil {
		t.Fatal(err)
	}
	terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	f.sql(t, `UPDATE object_fixture.owners SET user_id=$1 WHERE id=$2`, id[identity.User](t).String(), f.owner.Details().ID)
	beforeGET, beforePUT := proxy.stageGET.Load(), proxy.candidatePUT.Load()
	_, err = through.transfers.InspectTransfer(contextFor(t), f.actor, grant.Status.ID)
	requireCode(t, err, foundation.Forbidden)
	_, err = through.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, completed)
	requireCode(t, err, foundation.Forbidden)
	_, err = through.transfers.CancelTransfer(contextFor(t), f.actor, grant.Status.ID, cancel)
	requireCode(t, err, foundation.Forbidden)
	_, err = through.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal)
	requireCode(t, err, foundation.Forbidden)
	changed := spec.Details()
	changed.ExpiresInSeconds++
	other, _ := oc.NewTransferSpec(changed)
	_, err = through.transfers.IssueTransfer(contextFor(t), f.actor, issue, other)
	requireCode(t, err, foundation.Forbidden)
	if proxy.stageGET.Load() != beforeGET || proxy.candidatePUT.Load() != beforePUT {
		t.Fatal("revoked owner replay touched storage")
	}
}

func TestTransferArchivedTerminalConvergesWithoutNewMaterial(t *testing.T) {
	f := newTransferFixture(t)
	put := f.put(t, "archive-input", "fixed input")
	f.sql(t, `INSERT INTO object_fixture.transfer_inputs VALUES($1,$2)`, f.operation.String(), put.Meta.ID.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Owner: f.owner, Direction: oc.TransferGET, ObjectID: put.Meta.ID})
	grant, err := f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "archive-get"), spec)
	if err != nil {
		t.Fatal(err)
	}
	proof := f.evidence(t, grant, oc.TransferCompletedEvidence, false)
	f.sql(t, `UPDATE object_fixture.projects SET state='archived'`)
	state, err := f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof)
	if err != nil || state.State != oc.TransferComplete || !state.LeaseActive {
		t.Fatal("archived terminal facts", err)
	}
	if _, err = f.transfers.CompleteTransfer(contextFor(t), f.actor, grant.Status.ID, proof); err != nil {
		t.Fatal("archived complete replay", err)
	}
	_, err = f.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "archive-new"), spec)
	requireCode(t, err, foundation.ProjectNotActive)
	terminal := f.evidence(t, grant, oc.TransferStoppedEvidence, true)
	state, err = f.transfers.ConfirmStopped(contextFor(t), f.actor, grant.Status.ID, terminal)
	if err != nil || state.LeaseActive || state.State != oc.TransferComplete {
		t.Fatal("archived terminal retirement", err)
	}
}

func TestTransferMissingRequiredDomainPortsRollbackIssue(t *testing.T) {
	for _, missing := range []string{"resource", "gate", "lease", "audit"} {
		t.Run(missing, func(t *testing.T) {
			f := newTransferFixture(t)
			proxy := newTransferProxy(t, f.fixture)
			through := transferOn(t, f, f.store, proxy.config(t, f.fixture), func(a *object.Authorizations, audit *audit.Authorizations) {
				switch missing {
				case "resource":
					a.Resources = nil
				case "gate":
					a.Gate = nil
				case "lease":
					a.Leases = nil
				case "audit":
					audit.Projects = nil
				}
			})
			grant, err := through.transfers.IssueTransfer(contextFor(t), f.actor, command(t, "missing"), f.spec(t, []byte("not admitted")))
			requireCode(t, err, foundation.DependencyUnbound)
			if _, err = grant.Material.ForRunner(f.runner, grant.Status.ID); err == nil {
				t.Fatal("missing domain produced material")
			}
			var count int
			if err = f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.objects)+(SELECT count(*) FROM agenteam_object.object_transfers)+(SELECT count(*) FROM agenteam_object.uploads)+(SELECT count(*) FROM agenteam_object.upload_attempts)+(SELECT count(*) FROM agenteam_object.object_leases)+(SELECT count(*) FROM agenteam_audit.audit_records)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed Issue retained partial facts", count, err)
			}
			if proxy.stagePUT.Load() != 0 || proxy.stageGET.Load() != 0 || proxy.candidatePUT.Load() != 0 {
				t.Fatal("failed Issue touched storage")
			}
		})
	}
}

func TestTransferIssueWaitsForRealAuthorityAndProjectGates(t *testing.T) {
	for _, target := range []string{"runner", "operation", "project"} {
		t.Run(target, func(t *testing.T) {
			f := newTransferFixture(t)
			spec := f.spec(t, []byte("locked declaration"))
			var key foundation.LockKey
			var sql, id string
			switch target {
			case "runner":
				key, _ = foundation.SystemConfigLock("runner-transfer-authority")
				sql = "UPDATE object_fixture.runners SET active=false WHERE id=$1"
				id = f.runner.String()
			case "operation":
				key, _ = foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-operation:"+f.operation.String())
				sql = "UPDATE object_fixture.transfer_operations SET active=false WHERE id=$1"
				id = f.operation.String()
			case "project":
				key, _ = foundation.ProjectLock(f.project.String())
				sql = "UPDATE object_fixture.projects SET state='archived' WHERE id=$1"
				id = f.project.String()
			}
			ctx := contextFor(t)
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			result := make(chan foundation.CommitResult, 1)
			cause := cause(t)
			go func() {
				result <- f.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
						return err
					}
					close(held)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					e, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					_, err = e.Exec(ctx, sql, id)
					return err
				})
			}()
			select {
			case <-held:
			case <-ctx.Done():
				t.Fatal("gate holder failed")
			}
			done := make(chan error, 1)
			cmd := command(t, "blocked-issue")
			go func() { _, err := f.transfers.IssueTransfer(ctx, f.actor, cmd, spec); done <- err }()
			waitAdvisory(t, f.fixture, key)
			once.Do(func() { close(release) })
			if r := <-result; r.State() != foundation.Committed {
				t.Fatal(r.Fault())
			}
			err := <-done
			if target == "project" {
				requireCode(t, err, foundation.ProjectNotActive)
			} else {
				requireCode(t, err, foundation.Forbidden)
			}
			var count int
			if err = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_transfers`).Scan(&count); err != nil || count != 0 {
				t.Fatal("gate race admitted grant", err)
			}
		})
	}
}
