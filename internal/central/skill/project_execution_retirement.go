package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// ProjectExecutionRetirementReferences retires only Skill-owned protection in
// the caller's original transaction. Its plan is tied to this concrete owner
// and the complete immutable Execution binding; it creates no transaction.
type ProjectExecutionRetirementReferences struct {
	store Store
	owner ec.ProjectExecutionRetirementOwner
}

func NewProjectExecutionRetirementReferences(store Store, owner ec.ProjectExecutionRetirementOwner) (*ProjectExecutionRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectExecutionRetirementReferences{store: store, owner: owner}, nil
}

type executionRetirementSkillPlan struct {
	owner   *ProjectExecutionRetirementReferences
	request ec.ProjectExecutionRetirementRequest
	capture *skillCaptureRecord
	queued  []*assignmentCommand
	locks   []f.LockRequest
	digest  f.Digest
}

func (p *executionRetirementSkillPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *executionRetirementSkillPlan) ProjectExecutionRetirementReferenceDigest() f.Digest {
	if p == nil {
		return ""
	}
	return p.digest
}
func (*executionRetirementSkillPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_execution_retirement_plan")
}
func (*executionRetirementSkillPlan) LogValue() slog.Value {
	return slog.StringValue("skill_execution_retirement_plan")
}
func (*ProjectExecutionRetirementReferences) ProjectExecutionRetirementReferenceKind() ec.ProjectExecutionRetirementReferenceKind {
	return ec.ProjectExecutionSkillReferences
}

func skillExecutionRetirementLocks(r ec.ProjectExecutionRetirementRequest) []f.LockRequest {
	a, _ := f.AgentLock(r.Binding.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.Binding.ExecutionID.String())
	s, _ := f.ProjectScheduleLock(r.Binding.ProjectID.String())
	return []f.LockRequest{projectLock(r.Binding.ProjectID, f.Exclusive), {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}, {Key: e, Mode: f.Exclusive}}
}

func (s *ProjectExecutionRetirementReferences) executor(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool, plan *executionRetirementSkillPlan) (postgres.SQLExecutor, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || r.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	locks := skillExecutionRetirementLocks(r)
	if plan != nil {
		if plan.owner != s || plan.request.Binding != r.Binding || plan.request.Cause != r.Cause || plan.request.Actor.Details() != r.Actor.Details() {
			return nil, fault(f.Forbidden)
		}
		locks = plan.RequiredLocks()
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if applied {
		err = s.owner.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, r)
	} else {
		err = s.owner.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, r)
	}
	if err != nil {
		return nil, portError(err)
	}
	return x, portError(ctx.Err())
}

func (s *ProjectExecutionRetirementReferences) DiscoverProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementReferencePlan, error) {
	x, err := s.executor(ctx, tx, r, false, nil)
	if err != nil {
		return nil, err
	}
	return s.readPlan(ctx, x, r)
}

func (s *ProjectExecutionRetirementReferences) readPlan(ctx context.Context, x postgres.SQLExecutor, r ec.ProjectExecutionRetirementRequest) (*executionRetirementSkillPlan, error) {
	p := &executionRetirementSkillPlan{owner: s, request: r, locks: skillExecutionRetirementLocks(r), queued: []*assignmentCommand{}}
	// The globally unique Execution is never filtered by Project before its
	// complete head and set have been checked.
	var raw []byte
	err := x.QueryRow(ctx, `SELECT record FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1`, r.Binding.ExecutionID.String()).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, unavailable(err)
	}
	if err == nil {
		var record skillCaptureRecord
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if len(raw) > 256<<10 || decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || record.Format != record.Source.format() || record.Source.validate() != nil || record.Attempt.Validate() != nil {
			return nil, unavailable(nil)
		}
		req := record.Source.Result.Request
		if req.ProjectID != r.Binding.ProjectID || req.AgentID != r.Binding.AgentID || req.ExecutionID != r.Binding.ExecutionID {
			return nil, fault(f.ResourceBusy)
		}
		original := sc.InitialRoundBindingsRequest{Initial: record.Source.Result, CaptureAttemptBinding: record.Attempt}
		loaded, readErr := loadInitialRoundCapture(ctx, x, original)
		if readErr != nil {
			return nil, readErr
		}
		if err = validateInitialRoundCapture(ctx, x, original, loaded); err != nil {
			return nil, err
		}
		p.capture = &loaded
		// The immutable initialization is read even for an empty/disabled
		// captured binding set, so retain its Skill gate in the final union.
		p.locks = append(p.locks, skillLock(loaded.Source.InitialSkillID, f.Shared))
		for _, binding := range loaded.Source.Result.Bindings {
			p.locks = append(p.locks, skillLock(binding.SkillID, f.Shared))
		}
	} else {
		var count int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1`, r.Binding.ExecutionID.String()).Scan(&count); err != nil {
			return nil, unavailable(err)
		}
		if count != 0 {
			return nil, fault(f.ResourceBusy)
		}
	}
	var ids []string
	err = x.QueryRow(ctx, `SELECT COALESCE(array_agg(change_id::text ORDER BY change_id),'{}'::text[]) FROM agenteam_skill.assignment_pending_refs WHERE execution_id=$1`, r.Binding.ExecutionID.String()).Scan(&ids)
	if err != nil {
		return nil, unavailable(err)
	}
	for _, raw := range ids {
		id, parseErr := f.ParseID[sc.AssignmentChange](raw)
		if parseErr != nil {
			return nil, unavailable(parseErr)
		}
		command, readErr := loadAssignmentByID(ctx, x, r.Binding.ProjectID, r.Binding.AgentID, id)
		if readErr != nil {
			return nil, readErr
		}
		data, _, _, readErr := retirementProofData(command)
		if readErr != nil {
			return nil, readErr
		}
		if data.Pending.ExecutionID != r.Binding.ExecutionID {
			return nil, fault(f.ResourceBusy)
		}
		if err = verifyRetirementChange(ctx, x, command); err != nil {
			return nil, err
		}
		p.queued = append(p.queued, command)
		p.locks = append(p.locks, skillLock(data.Pending.SkillID, f.Shared))
	}
	// A missing pending row must not erase an original queued command from
	// the expected set. Other already-retired Executions can have proofs, so
	// this complete command read does not apply ordinary routing to them.
	commandIDs, err := retirementHistoryIDs(ctx, x, "assignment_commands", r.Binding.AgentID.String())
	if err != nil {
		return nil, err
	}
	queuedIDs := []string{}
	for _, raw := range commandIDs {
		id, parseErr := f.ParseID[sc.AssignmentChange](raw)
		if parseErr != nil {
			return nil, unavailable(parseErr)
		}
		command, readErr := loadAssignmentRecordByID(ctx, x, r.Binding.ProjectID, r.Binding.AgentID, id)
		if readErr != nil {
			return nil, readErr
		}
		receipt, readErr := command.runtimeReceipt()
		if readErr != nil {
			return nil, readErr
		}
		if receipt != nil && receipt.Queued && receipt.ExecutionID != nil && *receipt.ExecutionID == r.Binding.ExecutionID {
			queuedIDs = append(queuedIDs, raw)
		}
	}
	if !slices.Equal(queuedIDs, ids) {
		return nil, fault(f.ResourceBusy)
	}
	var proof bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_execution_retirement_proofs WHERE execution_id=$1)`, r.Binding.ExecutionID.String()).Scan(&proof)
	if err != nil {
		return nil, unavailable(err)
	}
	if proof {
		return nil, fault(f.ResourceBusy)
	}
	p.locks, err = ob.NormalizeLocks(p.locks)
	if err != nil {
		return nil, portError(err)
	}
	data := struct {
		Capture *skillCaptureRecord            `json:"capture"`
		Queued  []executionRetirementProofData `json:"queued"`
	}{Capture: p.capture, Queued: []executionRetirementProofData{}}
	for _, command := range p.queued {
		proof, _, _, e := retirementProofData(command)
		if e != nil {
			return nil, e
		}
		data.Queued = append(data.Queued, proof)
	}
	p.digest, _, err = captureDigest(data)
	if err != nil {
		return nil, err
	}
	return p, portError(ctx.Err())
}

func retirementSkillPlan(s *ProjectExecutionRetirementReferences, raw ec.ProjectExecutionRetirementReferencePlan) (*executionRetirementSkillPlan, error) {
	p, ok := raw.(*executionRetirementSkillPlan)
	if !ok || p == nil || p.owner != s || p.digest.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}

func (s *ProjectExecutionRetirementReferences) RetireProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, raw ec.ProjectExecutionRetirementReferencePlan) error {
	p, err := retirementSkillPlan(s, raw)
	if err != nil {
		return err
	}
	x, err := s.executor(ctx, tx, r, false, p)
	if err != nil {
		return err
	}
	current, err := s.readPlan(ctx, x, r)
	if err != nil {
		return err
	}
	if current.digest != p.digest {
		return fault(f.ResourceBusy)
	}
	for _, command := range p.queued {
		data, _, _, e := retirementProofData(command)
		if e != nil {
			return e
		}
		v := data.Pending
		tag, e := x.Exec(ctx, `DELETE FROM agenteam_skill.assignment_pending_refs WHERE change_id=$1 AND project_id=$2 AND agent_id=$3 AND execution_id=$4 AND assignment_id=$5 AND assignment_sequence=$6 AND skill_id=$7 AND revision_id=$8 AND revision=$9 AND object_id=$10`, v.ChangeID.String(), v.ProjectID.String(), v.AgentID.String(), v.ExecutionID.String(), v.AssignmentID.String(), int64(v.AssignmentSequence), v.SkillID.String(), v.RevisionID.String(), int64(v.Revision), v.ObjectID.String())
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		if e = ctx.Err(); e != nil {
			return portError(e)
		}
	}
	if p.capture != nil {
		for _, step := range []struct {
			table string
			count int64
		}{{"execution_bindings", int64(len(p.capture.Source.Result.Bindings))}, {"execution_binding_heads", 1}} {
			tag, e := x.Exec(ctx, `DELETE FROM agenteam_skill.`+step.table+` WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3`, r.Binding.ExecutionID.String(), r.Binding.ProjectID.String(), r.Binding.AgentID.String())
			if e != nil {
				return unavailable(e)
			}
			if tag.RowsAffected() != step.count {
				return fault(f.ResourceBusy)
			}
			if e = ctx.Err(); e != nil {
				return portError(e)
			}
		}
	}
	return requireExecutionSkillReferencesAbsent(ctx, x, r.Binding.ExecutionID)
}

func requireExecutionSkillReferencesAbsent(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) error {
	var remaining bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.execution_bindings WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE execution_id=$1)`, execution.String()).Scan(&remaining)
	if err != nil {
		return unavailable(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func (s *ProjectExecutionRetirementReferences) FinalizeProjectExecutionRetirementInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, raw ec.ProjectExecutionRetirementReferencePlan) error {
	p, err := retirementSkillPlan(s, raw)
	if err != nil {
		return err
	}
	x, err := s.executor(ctx, tx, r, true, p)
	if err != nil {
		return err
	}
	commit, err := s.owner.ProjectExecutionRetirementProofInTx(ctx, tx, r)
	if err != nil {
		return portError(err)
	}
	if !commit.Matches(r) {
		return fault(f.Forbidden)
	}
	if err = requireExecutionSkillReferencesAbsent(ctx, x, r.Binding.ExecutionID); err != nil {
		return err
	}
	for _, command := range p.queued {
		_, data, digest, e := retirementProofData(command)
		if e != nil {
			return e
		}
		tag, e := x.Exec(ctx, `INSERT INTO agenteam_skill.assignment_execution_retirement_proofs(change_id,project_id,agent_id,execution_id,execution_version,canonical_digest,operation_id,project_version,claim_process,claim_attempt,fence,transaction_id,proof_data,proof_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, command.ID.String(), r.Binding.ProjectID.String(), r.Binding.AgentID.String(), r.Binding.ExecutionID.String(), int64(r.Binding.Version), string(r.Binding.CanonicalDigest), commit.OperationID.String(), int64(commit.ProjectVersion), commit.ProcessID, commit.AttemptID, commit.Fence, commit.TransactionID, data, string(digest))
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		if e = ctx.Err(); e != nil {
			return portError(e)
		}
	}
	return s.RequireProjectExecutionReferencesRetiredInTx(ctx, tx, r, p)
}

func (s *ProjectExecutionRetirementReferences) RequireProjectExecutionReferencesRetiredInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, raw ec.ProjectExecutionRetirementReferencePlan) error {
	p, err := retirementSkillPlan(s, raw)
	if err != nil {
		return err
	}
	x, err := s.executor(ctx, tx, r, true, p)
	if err != nil {
		return err
	}
	commit, err := s.owner.ProjectExecutionRetirementProofInTx(ctx, tx, r)
	if err != nil {
		return portError(err)
	}
	if !commit.Matches(r) {
		return fault(f.Forbidden)
	}
	if err = requireExecutionSkillReferencesAbsent(ctx, x, r.Binding.ExecutionID); err != nil {
		return err
	}
	var count int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.assignment_execution_retirement_proofs WHERE execution_id=$1`, r.Binding.ExecutionID.String()).Scan(&count); err != nil {
		return unavailable(err)
	}
	if count != int64(len(p.queued)) {
		return fault(f.ResourceBusy)
	}
	for _, before := range p.queued {
		command, e := loadAssignmentRecordByID(ctx, x, r.Binding.ProjectID, r.Binding.AgentID, before.ID)
		if e != nil {
			return e
		}
		_, expected, _, e := retirementProofData(before)
		if e != nil {
			return e
		}
		proof, e := readExecutionRetirementProof(ctx, x, command)
		if e != nil {
			return e
		}
		if proof.binding != r.Binding || proof.commit != commit || !bytes.Equal(proof.raw, expected) {
			return fault(f.ResourceBusy)
		}
	}
	return portError(ctx.Err())
}

var _ ec.ProjectExecutionSkillRetirementReferences = (*ProjectExecutionRetirementReferences)(nil)
