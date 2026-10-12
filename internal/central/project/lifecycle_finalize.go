package project

import (
	"context"
	"math"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewLifecycleFinalizeDriver binds only the final Project-owned transaction.
// It does not install a registry or supply missing domain cleanup. The original
// Service supplies private creation-return evidence; another same-process
// Service cannot infer that evidence from a terminal database row.
func NewLifecycleFinalizeDriver(store Store, original *Service, authority *LifecycleAuthority, processes oc.ProcessAuthority) (*LifecycleCleanupDriver, error) {
	service := original.state()
	if nilPort(store) || service == nil || !authority.bound() || nilPort(processes) ||
		!sameStore(store, service.store) || !sameStore(store, authority.store) ||
		service.deps.Authority.state() == nil || service.deps.Authority.state().lifecycle != authority ||
		nilPort(service.deps.Processes) || reflect.TypeOf(processes) != reflect.TypeOf(service.deps.Processes) || !reflect.TypeOf(processes).Comparable() || service.deps.Processes != processes {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, invalid()
	}
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleCleanupDriver{state: &lifecycleCleanupState{
		store: store, authority: authority, processes: processes, process: process,
		finalizer: original, registration: registration,
		calls: map[c.ProjectID]context.CancelFunc{}, owners: map[c.OperationID]lifecycleCleanupOwner{}, changed: make(chan struct{}),
	}}, nil
}

type finalizeClaim struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Process string `json:"process"`
	Attempt string `json:"attempt"`
	Fence   int64  `json:"fence"`
	Phase   string `json:"phase"`
}

func (v finalizeClaim) validate() error {
	if v.Kind != "creation" && v.Kind != "lifecycle" || v.Fence < 1 || v.Phase != "running" && v.Phase != "terminal" {
		return unavailable(nil)
	}
	for _, id := range []string{v.ID, v.Process, v.Attempt} {
		if _, err := parseID[struct{}](id); err != nil {
			return err
		}
	}
	return nil
}

func finalClaim(v lifecycleStopClaim) finalizeClaim {
	return finalizeClaim{Kind: "lifecycle", ID: v.operation.String(), Process: v.process.String(), Attempt: v.attempt, Fence: v.fence, Phase: v.phase}
}

type finalizeFacts struct {
	project   *projectRecord
	operation *lifecycleRecord
	command   *lifecycleCommandRecord
	claims    []finalizeClaim
	counts    [4]int64 // commands, creations, lifecycle operations, participant rows
}

type lifecycleFinalizeRound struct {
	claim               lifecycleStopClaim
	before              *finalizeFacts
	registered          bool
	unknown             *f.CommitResult
	unknownRegistration bool
	receipt             *c.DeletionReceiptData
}

func finalizeProgress(r *lifecycleRecord) error {
	if r == nil || r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "final" {
		return fault(f.InvalidState)
	}
	if err := projectLifecycleProgress(r); err != nil {
		return err
	}
	for _, row := range r.participants {
		if row.stop != "stopped" || row.cleanup != "completed" || len(row.refs) != 0 || row.reason != "" {
			return fault(f.ResourceBusy)
		}
	}
	return nil
}

// All reads and writes here concern Project-owned tables. Domain facts are
// represented only by their frozen, fully validated participant checkpoints.
func (st *lifecycleCleanupState) finalFacts(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, operation c.OperationID) (*finalizeFacts, error) {
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fault(f.ResourceDeleted)
	}
	r, err := loadLifecycleOperation(ctx, x, project, operation)
	if err != nil {
		return nil, err
	}
	if err = finalizeProgress(r); err != nil {
		return nil, err
	}
	cause := c.LifecycleCause{OperationID: operation, Action: c.Delete, ProjectVersion: r.operation.ProjectVersion}
	if _, err = st.authority.lifecycleFact(ctx, x, project, cause, ""); err != nil {
		return nil, err
	}
	if !p.initialized || p.ref.Lifecycle != c.Deleting || p.operation == nil || *p.operation != operation || p.ref.Version != cause.ProjectVersion || p.ref.OwnerUserID.String() != r.owner {
		return nil, fault(f.Forbidden)
	}
	creation, err := loadCreation(ctx, x, p.creation)
	if err != nil {
		return nil, err
	}
	if creation == nil || creation.operation.State != c.CreationCompleted || creation.operation.ProjectID != project || creation.owner != p.ref.OwnerUserID {
		return nil, fault(f.ResourceBusy)
	}
	var key string
	var matching int64
	err = x.QueryRow(ctx, `SELECT count(*),COALESCE(min(key),'') FROM agenteam_project.commands WHERE project_id=$1 AND command_name='delete' AND plan->>'operation_id'=$2`, project.String(), operation.String()).Scan(&matching, &key)
	if err != nil {
		return nil, unavailable(err)
	}
	if matching != 1 {
		return nil, unavailable(nil)
	}
	command, err := loadLifecycleCommand(ctx, x, project, c.DeleteCommand, f.IdempotencyKey(key))
	if err != nil {
		return nil, err
	}
	if command == nil || command.state != "completed" || command.user != r.owner || command.plan.OperationID != operation || command.plan.ManifestDigest != r.digest || command.header.AggregateVersion == nil || *command.header.AggregateVersion != p.ref.Version {
		return nil, unavailable(nil)
	}
	var unfinished bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$1 AND (state<>'completed' OR actor_user_id<>$3)) OR EXISTS(SELECT 1 FROM agenteam_project.lifecycle_operations WHERE project_id=$1 AND (owner_user_id<>$3 OR (id<>$2 AND (action<>'archive' OR state<>'completed')))) OR EXISTS(SELECT 1 FROM agenteam_project.work_claims w WHERE w.project_id=$1 AND w.work_kind='lifecycle' AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_operations o WHERE o.id=w.work_id AND o.project_id=w.project_id))`, project.String(), operation.String(), p.ref.OwnerUserID.String()).Scan(&unfinished)
	if err != nil {
		return nil, unavailable(err)
	}
	if unfinished {
		return nil, fault(f.ResourceBusy)
	}
	v := &finalizeFacts{project: p, operation: r, command: command}
	var raw []byte
	err = x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',work_kind,'id',work_id::text,'process',process_id::text,'attempt',attempt_id::text,'fence',fence,'phase',phase) ORDER BY work_kind,work_id),'[]'::jsonb) FROM agenteam_project.work_claims WHERE project_id=$1`, project.String()).Scan(&raw)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = decodeLifecycleJSON(raw, &v.claims); err != nil {
		return nil, err
	}
	creationClaim, currentClaim := 0, 0
	for i, claim := range v.claims {
		if err = claim.validate(); err != nil {
			return nil, err
		}
		if i > 0 && (v.claims[i-1].Kind > claim.Kind || v.claims[i-1].Kind == claim.Kind && v.claims[i-1].ID >= claim.ID) {
			return nil, unavailable(nil)
		}
		if claim.Kind == "creation" {
			if claim.ID != p.creation.String() || claim.Phase != "terminal" {
				return nil, fault(f.ResourceBusy)
			}
			creationClaim++
		} else if claim.ID == operation.String() {
			currentClaim++
		}
	}
	if creationClaim != 1 || currentClaim != 1 {
		return nil, fault(f.ResourceBusy)
	}
	err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_project.creations WHERE project_id=$1),(SELECT count(*) FROM agenteam_project.lifecycle_operations WHERE project_id=$1),(SELECT count(*) FROM agenteam_project.lifecycle_participants p JOIN agenteam_project.lifecycle_operations o ON o.id=p.operation_id WHERE o.project_id=$1)`, project.String()).Scan(&v.counts[0], &v.counts[1], &v.counts[2], &v.counts[3])
	if err != nil {
		return nil, unavailable(err)
	}
	if v.counts[0] < 1 || v.counts[1] != 1 || v.counts[2] < 1 || v.counts[3] < int64(len(r.participants)) {
		return nil, unavailable(nil)
	}
	return v, ctx.Err()
}

func sameFinalizeFacts(before, actual *finalizeFacts, replacement *lifecycleStopClaim) bool {
	if before == nil || actual == nil || before.project == nil || actual.project == nil || before.operation == nil || actual.operation == nil || before.command == nil || actual.command == nil || before.project.ref.ID != actual.project.ref.ID || before.project.creation != actual.project.creation || before.project.ref.Version != actual.project.ref.Version || before.project.ref.OwnerUserID != actual.project.ref.OwnerUserID || before.operation.operation.Version != actual.operation.operation.Version || before.operation.digest != actual.operation.digest || before.counts != actual.counts || before.command.project != actual.command.project || before.command.user != actual.command.user || before.command.key != actual.command.key || before.command.name != actual.command.name || before.command.state != actual.command.state || !sameLifecyclePlan(before.command, actual.command) {
		return false
	}
	expected := slices.Clone(before.claims)
	if replacement != nil {
		found := false
		for i := range expected {
			if expected[i].Kind == "lifecycle" && expected[i].ID == replacement.operation.String() {
				expected[i], found = finalClaim(*replacement), true
			}
		}
		if !found {
			return false
		}
	}
	return slices.Equal(expected, actual.claims)
}

// Process death is checked outside the writer transaction. The later P EX
// transaction compares the entire original claim set before using that proof.
func (st *lifecycleCleanupState) finalReturns(ctx context.Context, facts *finalizeFacts) error {
	for _, claim := range facts.claims {
		process, err := parseID[oc.Process](claim.Process)
		if err != nil {
			return err
		}
		if process != st.process {
			if err = st.processes.ConfirmStopped(ctx, process); err != nil {
				return portError(err)
			}
			continue
		}
		if claim.Kind == "creation" {
			original := st.finalizer.state()
			original.mu.Lock()
			returned := original.joined[claim.Attempt]
			original.mu.Unlock()
			if !returned {
				return fault(f.ResourceBusy)
			}
		} else if claim.ID != facts.operation.operation.ID.String() || claim.Phase != "terminal" {
			// A different old local driver is not this owner's returned call.
			return fault(f.ResourceBusy)
		}
	}
	return portError(ctx.Err())
}

func (st *lifecycleCleanupState) finalTx(ctx context.Context, round *lifecycleFinalizeRound, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return st.store.WithinTx(ctx, round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if st.processes.CurrentProcess() != st.process || round.claim.process != st.process {
			return fault(f.Forbidden)
		}
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, round.claim.project)
		if err != nil {
			return err
		}
		if nilPort(x) {
			return unavailable(nil)
		}
		if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		return fn(ctx, x)
	})
}

func (st *lifecycleCleanupState) runFinalize(ctx context.Context, project c.ProjectID, operation c.OperationID, round *lifecycleFinalizeRound) (*lifecycleFinalizeRound, bool, error) {
	if round != nil {
		if round.claim.project != project || round.claim.operation != operation || round.claim.process != st.process {
			return round, false, fault(f.Forbidden)
		}
		if round.unknown != nil {
			done, err := st.confirmFinalize(ctx, round)
			return round, done, err
		}
	} else {
		attempt, err := f.NewID[struct{}]()
		if err != nil {
			return nil, false, unavailable(err)
		}
		round = &lifecycleFinalizeRound{claim: lifecycleStopClaim{project: project, operation: operation, process: st.process, attempt: attempt.String(), phase: "running"}}
		validated := false
		result := st.finalTx(ctx, round, func(ctx context.Context, x postgres.SQLExecutor) error {
			var err error
			round.before, err = st.finalFacts(ctx, x, project, operation)
			validated = err == nil
			return err
		})
		if err = commitError(result); err != nil {
			return nil, false, err
		}
		if !validated {
			return nil, false, unavailable(nil)
		}
		if err = st.finalReturns(ctx, round.before); err != nil {
			return nil, false, err
		}
		for _, claim := range round.before.claims {
			if claim.Kind == "lifecycle" && claim.ID == operation.String() {
				// finalReturns has already proved a foreign process stopped.
				// Its running final claim may be reclaimed only after the next
				// transaction rechecks this exact tuple, never for a live local
				// instance whose original round is unavailable here.
				if claim.Phase != "terminal" && claim.Process == st.process.String() || claim.Fence == math.MaxInt64 {
					return nil, false, fault(f.ResourceBusy)
				}
				round.claim.fence = claim.Fence + 1
			}
		}
	}
	if !round.registered {
		if err := st.authority.retainFinalize(round); err != nil {
			return nil, false, err
		}
		validated := false
		result := st.finalTx(ctx, round, func(ctx context.Context, x postgres.SQLExecutor) error {
			actual, err := st.finalFacts(ctx, x, project, operation)
			if err != nil {
				return err
			}
			if !sameFinalizeFacts(round.before, actual, nil) {
				return fault(f.ResourceBusy)
			}
			var previous finalizeClaim
			for _, claim := range actual.claims {
				if claim.Kind == "lifecycle" && claim.ID == operation.String() {
					previous = claim
				}
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$3,attempt_id=$4,fence=$5,phase='running' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$2 AND process_id=$6 AND attempt_id=$7 AND fence=$8 AND phase=$9`, operation.String(), project.String(), st.process.String(), round.claim.attempt, round.claim.fence, previous.Process, previous.Attempt, previous.Fence, previous.Phase)
			if err = affected(tag, err); err != nil {
				return err
			}
			validated = true
			return ctx.Err()
		})
		if result.State() == f.Unknown {
			round.unknown, round.unknownRegistration = &result, true
			return round, false, commitError(result)
		}
		if err := commitError(result); err != nil {
			st.authority.releaseFinalize(round)
			return nil, false, err
		}
		if !validated {
			st.authority.releaseFinalize(round)
			return nil, false, unavailable(nil)
		}
		round.registered = true
	}
	if err := ctx.Err(); err != nil {
		return round, false, portError(err)
	}
	validated := false
	result := st.finalTx(ctx, round, func(ctx context.Context, x postgres.SQLExecutor) error {
		actual, err := st.finalFacts(ctx, x, project, operation)
		if err != nil {
			return err
		}
		if !sameFinalizeFacts(round.before, actual, &round.claim) {
			return fault(f.ResourceBusy)
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		hash, err := c.DeletionCommandKeyHash(project, actual.project.ref.OwnerUserID, actual.command.key)
		if err != nil {
			return err
		}
		receipt := c.DeletionReceiptData{OperationID: operation, DeletedProjectID: project, OriginalOwnerUserID: actual.project.ref.OwnerUserID, CommandKeyHash: hash, RequestDigest: actual.command.semantic, CompletedAt: now}
		if receipt.Validate() != nil {
			return unavailable(nil)
		}
		round.receipt = &receipt
		if err = writeFinalize(ctx, x, round, actual); err != nil {
			return err
		}
		validated = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		round.unknown, round.unknownRegistration = &result, false
		return round, false, commitError(result)
	}
	if err := commitError(result); err != nil {
		return round, false, err
	}
	if !validated {
		return round, false, unavailable(nil)
	}
	return round, true, nil
}

func writeFinalize(ctx context.Context, x postgres.SQLExecutor, round *lifecycleFinalizeRound, facts *finalizeFacts) error {
	d := round.receipt
	if d == nil || d.Validate() != nil {
		return unavailable(nil)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_project.deletion_receipts(operation_id,deleted_project_id,original_owner_user_id,command_key_hash,request_digest,completed_at,status) VALUES($1,$2,$3,$4,$5,$6,'completed')`, d.OperationID.String(), d.DeletedProjectID.String(), d.OriginalOwnerUserID.String(), d.CommandKeyHash.String(), d.RequestDigest.String(), d.CompletedAt.Time())
	if err = affected(tag, err); err != nil {
		return err
	}
	steps := []struct {
		sql   string
		count int64
	}{
		{`DELETE FROM agenteam_project.commands WHERE project_id=$1`, facts.counts[0]},
		{`DELETE FROM agenteam_project.lifecycle_participants WHERE operation_id IN (SELECT id FROM agenteam_project.lifecycle_operations WHERE project_id=$1)`, facts.counts[3]},
		{`DELETE FROM agenteam_project.lifecycle_operations WHERE project_id=$1`, facts.counts[2]},
		{`DELETE FROM agenteam_project.creations WHERE project_id=$1`, facts.counts[1]},
		{`DELETE FROM agenteam_project.work_claims WHERE project_id=$1`, int64(len(facts.claims))},
		{`DELETE FROM agenteam_project.projects WHERE id=$1`, 1},
	}
	for _, step := range steps {
		if err = ctx.Err(); err != nil {
			return portError(err)
		}
		tag, err = x.Exec(ctx, step.sql, d.DeletedProjectID.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != step.count {
			return unavailable(nil)
		}
	}
	return portError(ctx.Err())
}

func (st *lifecycleCleanupState) confirmFinalize(ctx context.Context, round *lifecycleFinalizeRound) (bool, error) {
	original := *round.unknown
	validated, applied, registered := false, false, false
	result := st.finalTx(ctx, round, func(ctx context.Context, x postgres.SQLExecutor) error {
		receipt, err := loadDeletionReceipt(ctx, x, round.claim.project)
		if err != nil {
			return err
		}
		if receipt != nil {
			if round.unknownRegistration || round.receipt == nil || !sameFinalizeReceipt(receipt.Details(), *round.receipt) {
				return fault(f.Forbidden)
			}
			var remaining bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1) OR EXISTS(SELECT 1 FROM agenteam_project.creations WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_project.lifecycle_operations WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants WHERE operation_id=$2) OR EXISTS(SELECT 1 FROM agenteam_project.work_claims WHERE project_id=$1)`, round.claim.project.String(), round.claim.operation.String()).Scan(&remaining)
			if err != nil {
				return unavailable(err)
			}
			if remaining {
				return unavailable(nil)
			}
			applied = true
		} else {
			facts, err := st.finalFacts(ctx, x, round.claim.project, round.claim.operation)
			if err != nil {
				return err
			}
			registered = sameFinalizeFacts(round.before, facts, &round.claim)
			if !registered && (!round.unknownRegistration || !sameFinalizeFacts(round.before, facts, nil)) {
				return fault(f.ResourceBusy)
			}
		}
		validated = true
		return ctx.Err()
	})
	// A confirmation never replaces the first physical Unknown provenance and
	// never performs another DELETE. Only its original committed return releases
	// that custody; a later explicit Run may resume an unapplied transaction.
	if result.State() != f.Committed || !validated || ctx.Err() != nil {
		return false, commitError(original)
	}
	round.unknown = nil
	round.registered = registered
	return applied, nil
}

func sameFinalizeReceipt(a, b c.DeletionReceiptData) bool {
	return a.OperationID == b.OperationID && a.DeletedProjectID == b.DeletedProjectID && a.OriginalOwnerUserID == b.OriginalOwnerUserID && a.CommandKeyHash == b.CommandKeyHash && a.RequestDigest == b.RequestDigest && a.CompletedAt.Time().Equal(b.CompletedAt.Time())
}
