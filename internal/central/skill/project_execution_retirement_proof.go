package skill

import (
	"bytes"
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// A proof preserves the original queued decision; it does not rewrite it into
// no-active or grant current authority. Only the Execution Applied issuer may
// insert one, and only the later Agent Applied path may consume it.
type executionRetirementPending struct {
	ChangeID           sc.AssignmentChangeID `json:"change_id"`
	ProjectID          i.ProjectID           `json:"project_id"`
	AgentID            i.AgentID             `json:"agent_id"`
	ExecutionID        i.ExecutionID         `json:"execution_id"`
	AssignmentID       sc.AssignmentID       `json:"assignment_id"`
	AssignmentSequence f.Version             `json:"assignment_sequence"`
	SkillID            sc.SkillID            `json:"skill_id"`
	RevisionID         sc.RevisionID         `json:"revision_id"`
	Revision           f.Revision            `json:"revision"`
	ObjectID           oc.ObjectID           `json:"object_id"`
}

type executionRetirementProofData struct {
	Format          int                        `json:"format"`
	Change          json.RawMessage            `json:"change_data"`
	RuntimeReceipt  json.RawMessage            `json:"runtime_receipt"`
	Pending         executionRetirementPending `json:"pending"`
	CommandDigest   f.Digest                   `json:"command_digest"`
	CandidateDigest f.Digest                   `json:"candidate_digest"`
	ReceiptDigest   f.Digest                   `json:"receipt_digest"`
}

type executionRetirementProof struct {
	binding ec.ProjectExecutionRetirementBinding
	commit  ec.ProjectExecutionRetirementCommit
	data    executionRetirementProofData
	raw     []byte
	digest  f.Digest
}

func retirementProofData(r *assignmentCommand) (executionRetirementProofData, []byte, f.Digest, error) {
	var empty executionRetirementProofData
	if r == nil || r.State != "completed" || r.validate() != nil || r.Candidate.Runtime == nil || r.Candidate.Revision == nil {
		return empty, nil, "", fault(f.ResourceBusy)
	}
	receipt, err := r.runtimeReceipt()
	if err != nil || receipt == nil || !receipt.Queued || receipt.ExecutionID == nil {
		return empty, nil, "", fault(f.ResourceBusy)
	}
	_, change, err := captureDigest(*r.Candidate.Runtime)
	if err != nil {
		return empty, nil, "", err
	}
	if len(change) > 65536 || len(r.RuntimeReceipt) > 1024 {
		return empty, nil, "", unavailable(nil)
	}
	candidate, _, err := captureDigest(r.Candidate)
	if err != nil {
		return empty, nil, "", err
	}
	c := *r.Candidate.Runtime
	out := executionRetirementProofData{
		Format: 1, Change: change, RuntimeReceipt: bytes.Clone(r.RuntimeReceipt),
		Pending:       executionRetirementPending{r.ID, c.ProjectID, c.AgentID, *receipt.ExecutionID, c.Binding.AssignmentID, c.AssignmentSequence, c.Binding.SkillID, c.Binding.RevisionID, c.Binding.Revision, r.Candidate.Revision.ObjectID},
		CommandDigest: r.Digest, CandidateDigest: candidate, ReceiptDigest: sum(r.Receipt),
	}
	digest, raw, err := captureDigest(out)
	if err != nil || len(raw) > 131072 {
		return empty, nil, "", unavailable(err)
	}
	return out, raw, digest, nil
}

func readExecutionRetirementProof(ctx context.Context, x postgres.SQLExecutor, command *assignmentCommand) (*executionRetirementProof, error) {
	expected, expectedRaw, expectedDigest, err := retirementProofData(command)
	if err != nil {
		return nil, err
	}
	var project, agent, execution, canonical, operation, process, attempt, transaction, digest string
	var executionVersion, projectVersion, fence int64
	var raw []byte
	err = x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,execution_id::text,execution_version,canonical_digest,operation_id::text,project_version,claim_process::text,claim_attempt::text,fence,transaction_id,proof_data,proof_digest FROM agenteam_skill.assignment_execution_retirement_proofs WHERE change_id=$1`, command.ID.String()).Scan(&project, &agent, &execution, &executionVersion, &canonical, &operation, &projectVersion, &process, &attempt, &fence, &transaction, &raw, &digest)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	var data executionRetirementProofData
	if assignmentDecode(raw, &data, 131072) != nil || data.Format != 1 || !bytes.Equal(raw, expectedRaw) || digest != string(expectedDigest) || sum(raw) != expectedDigest {
		return nil, fault(f.ResourceBusy)
	}
	if project != expected.Pending.ProjectID.String() || agent != expected.Pending.AgentID.String() || execution != expected.Pending.ExecutionID.String() {
		return nil, fault(f.ResourceBusy)
	}
	p := &executionRetirementProof{data: data, raw: bytes.Clone(raw), digest: expectedDigest}
	p.binding = ec.ProjectExecutionRetirementBinding{ProjectID: expected.Pending.ProjectID, AgentID: expected.Pending.AgentID, ExecutionID: expected.Pending.ExecutionID, Version: f.Version(executionVersion), CanonicalDigest: f.Digest(canonical)}
	p.commit.ProjectID = p.binding.ProjectID
	p.commit.OperationID, err = f.ParseID[pc.Operation](operation)
	if err != nil {
		return nil, unavailable(err)
	}
	p.commit.ProjectVersion, p.commit.ProcessID, p.commit.AttemptID, p.commit.Fence, p.commit.TransactionID = f.Version(projectVersion), process, attempt, fence, transaction
	if p.binding.Validate() != nil || p.commit.Validate() != nil {
		return nil, fault(f.ResourceBusy)
	}
	return p, nil
}

// This reader is called only after the original Agent Applied gate, never by
// ordinary Lookup/routing or the Execution Prepared discovery path.
func loadAssignmentRetirementCommand(ctx context.Context, x postgres.SQLExecutor, change ac.ProjectRetirementChange, id sc.AssignmentChangeID) (*assignmentCommand, *executionRetirementProof, error) {
	r, err := loadAssignmentRecordByID(ctx, x, change.Request.ProjectID, change.Request.AgentID, id)
	if err != nil {
		return nil, nil, err
	}
	receipt, err := r.runtimeReceipt()
	if err != nil {
		return nil, nil, err
	}
	if receipt == nil || !receipt.Queued {
		if err = validateAssignmentRouting(ctx, x, r); err != nil {
			return nil, nil, err
		}
		return r, nil, nil
	}
	proof, err := readExecutionRetirementProof(ctx, x, r)
	if err != nil {
		return nil, nil, err
	}
	if proof.commit.ProjectID != change.Request.ProjectID || proof.commit.OperationID != change.Request.Cause.OperationID || proof.commit.ProjectVersion != change.Request.Cause.ProjectVersion {
		return nil, nil, fault(f.ResourceBusy)
	}
	return r, proof, nil
}
