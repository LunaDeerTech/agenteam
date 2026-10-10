//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Only acceptance is a controlled upstream prerequisite. No test writes the
// stopping transition or the lifecycle work claim produced by the real driver.
func phaseAccepted(t *testing.T, v *variableLifecycleFixture, p pc.ProjectRef) pc.LifecycleCause {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Archive, ProjectVersion: p.Version + 1}
	raw, err := json.Marshal(v.manifest.Entries())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := v.manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	txCause, _ := f.NewRecoveryCause("project.accepted-fixture", cause.OperationID.String(), "")
	result := v.raw.WithinTx(ctxFor(t), txCause, func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(p.ID.String())
		if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.raw.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'archive',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), p.ID.String(), p.OwnerUserID.String(), int64(cause.ProjectVersion), raw, digest.String())
		if err != nil {
			return err
		}
		for _, entry := range v.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','not_applicable',1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion)); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archiving',version=$2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND version=$4 AND lifecycle='active' AND current_lifecycle_operation_id IS NULL`, p.ID.String(), int64(cause.ProjectVersion), cause.OperationID.String(), int64(p.Version))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("accepted fixture gate mismatch")
		}
		return nil
	})
	if result.State() != f.Committed {
		t.Fatal("accepted prerequisite did not commit", result.Fault())
	}
	return cause
}

type phaseRelease struct {
	done chan struct{}
	once sync.Once
}

func newPhaseRelease() *phaseRelease { return &phaseRelease{done: make(chan struct{})} }
func (r *phaseRelease) release()     { r.once.Do(func() { close(r.done) }) }
func phaseAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("phase original barrier missing")
	}
}
func phaseDriver(t *testing.T, v *variableLifecycleFixture, process oc.ProcessID, authority *project.LifecycleAuthority, step project.LifecycleLocalStopStep) *project.LifecycleStopDriver {
	t.Helper()
	driver, err := project.NewLifecycleStopDriver(v.tracked, authority, fixtureProcess{process}, step)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil {
			t.Error("phase driver original calls did not drain", err)
		}
	})
	return driver
}

type phaseRunResult struct {
	done chan struct{}
	err  error
}

func phaseRun(t *testing.T, driver *project.LifecycleStopDriver, project pc.ProjectID, operation pc.OperationID, base context.Context, releases ...*phaseRelease) *phaseRunResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	result := &phaseRunResult{done: make(chan struct{})}
	go func() { defer close(result.done); result.err = driver.Run(ctx, project, operation) }()
	t.Cleanup(func() {
		cancel()
		for _, r := range releases {
			r.release()
		}
		await(t, result.done)
	})
	return result
}
func phaseMustPending(t *testing.T, driver *project.LifecycleStopDriver, result *phaseRunResult) {
	t.Helper()
	select {
	case <-result.done:
		t.Fatal("original phase call returned before its barrier")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if driver.Drain(ctx) == nil {
		t.Fatal("Drain passed a held original phase call")
	}
}

func phaseState(t *testing.T, v *variableLifecycleFixture, cause pc.LifecycleCause, state string, claim string, fence int64) {
	t.Helper()
	var actual, digest string
	var version, accepted int64
	var participants, changed int
	err := v.raw.QueryRow(ctxFor(t), `SELECT state,version,project_version,manifest_digest,(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1),(SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND (stop_state<>'required' OR cleanup_state<>'not_applicable' OR version<>1 OR safe_pending_refs<>'[]'::jsonb)) FROM agenteam_project.lifecycle_operations WHERE id=$1`, cause.OperationID.String()).Scan(&actual, &version, &accepted, &digest, &participants, &changed)
	expectedVersion := int64(2)
	if state == "accepted" {
		expectedVersion = 1
	}
	expectedDigest, _ := v.manifest.Digest()
	if err != nil || actual != state || version != expectedVersion || accepted != int64(cause.ProjectVersion) || digest != expectedDigest.String() || participants != len(v.manifest.Entries()) || changed != 0 {
		t.Fatal("phase changed frozen manifest/participant completion", err)
	}
	var count int
	if err = v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, cause.OperationID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if claim == "" {
		if count != 0 {
			t.Fatal("unconfirmed phase persisted claim")
		}
		return
	}
	var actualClaim string
	var actualFence int64
	if count != 1 {
		t.Fatal("missing/duplicate lifecycle claim")
	}
	if err = v.raw.QueryRow(ctxFor(t), `SELECT phase,fence FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, cause.OperationID.String()).Scan(&actualClaim, &actualFence); err != nil || actualClaim != claim || actualFence != fence {
		t.Fatal("claim attempt state/fence mismatch", err)
	}
}
