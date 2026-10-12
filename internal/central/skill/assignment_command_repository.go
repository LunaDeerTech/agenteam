package skill

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type assignmentCandidate struct {
	Origin       i.ActorDetails              `json:"origin"`
	BeforeDigest f.Digest                    `json:"before_digest"`
	After        assignmentSet               `json:"after"`
	Skill        sc.SkillID                  `json:"skill_id"`
	Version      f.Version                   `json:"version"`
	Changed      bool                        `json:"changed"`
	Added        *sc.AgentSkillAssignment    `json:"added,omitempty"`
	Removed      *sc.AgentSkillAssignment    `json:"removed,omitempty"`
	Revision     *assignmentRevision         `json:"revision,omitempty"`
	Runtime      *sc.RuntimeAssignmentChange `json:"runtime,omitempty"`
	Created      f.Instant                   `json:"created_at"`
}
type assignmentCommand struct {
	ID             sc.AssignmentChangeID
	Input          assignmentInput
	Digest         f.Digest
	Candidate      assignmentCandidate
	State          string
	Receipt        []byte
	RuntimeReceipt []byte
}

func assignmentReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fault(f.NotFound)
	}
	return captureError(err)
}
func (r *assignmentCommand) validate() error {
	if r == nil || r.ID.Validate() != nil || r.Input.Request.validate() != nil || r.Input.Meta.ExpectedVersion == nil || r.Input.Digest != r.Digest || r.Digest.Validate() != nil || r.Candidate.BeforeDigest.Validate() != nil || r.Candidate.After.validate() != nil || r.Candidate.Skill.Validate() != nil || r.Candidate.Version.Validate() != nil || r.Candidate.Created.Validate() != nil || r.State != "planned" && r.State != "completed" {
		return unavailable(nil)
	}
	c := r.Candidate
	if c.Origin.Kind != i.Human || c.Origin.UserID != r.Input.Actor.Details().UserID {
		return unavailable(nil)
	}
	if _, e := f.ParseID[i.Session](c.Origin.SessionID); e != nil {
		return unavailable(e)
	}
	if c.After.Project != r.Input.Request.ProjectID || c.After.Agent != r.Input.Request.AgentID {
		return unavailable(nil)
	}
	if c.Changed {
		if c.Version != *r.Input.Meta.ExpectedVersion+1 || c.After.Sequence < 2 || c.Added == nil && c.Removed == nil {
			return unavailable(nil)
		}
	} else if c.Version != *r.Input.Meta.ExpectedVersion || c.Added != nil || c.Removed != nil || c.Revision != nil || c.Runtime != nil {
		return unavailable(nil)
	}
	for _, a := range []*sc.AgentSkillAssignment{c.Added, c.Removed} {
		if a != nil && (a.Validate() != nil || a.SkillID != c.Skill || a.ProjectID != c.After.Project || a.AgentID != c.After.Agent) {
			return unavailable(nil)
		}
	}
	if c.Added != nil && (c.Added.Sequence != c.After.Sequence || c.Added.CreatedAt != c.Created) {
		return unavailable(nil)
	}
	if err := r.validateTransition(); err != nil {
		return err
	}
	if c.Runtime != nil {
		if c.Runtime.Validate() != nil || c.Runtime.ID != r.ID || c.Runtime.ProjectID != c.After.Project || c.Runtime.AgentID != c.After.Agent || c.Runtime.AgentVersion != c.Version || c.Runtime.AssignmentSequence != c.After.Sequence || c.Added == nil || !c.Added.Enabled || c.Revision == nil {
			return unavailable(nil)
		}
		b := bindingForAssignment(*c.Added, *c.Revision)
		if c.Runtime.Binding != b || c.Runtime.RecordedAt != c.Created {
			return unavailable(nil)
		}
	} else if c.Added != nil && c.Added.Enabled {
		return unavailable(nil)
	}
	if c.Revision != nil && (c.Revision.validate() != nil || c.Revision.SkillID != c.Skill || c.Revision.ProjectID != c.After.Project) {
		return unavailable(nil)
	}
	if (c.Revision != nil) != (c.Runtime != nil) {
		return unavailable(nil)
	}
	if (r.State == "completed") != (len(r.Receipt) > 0) {
		return unavailable(nil)
	}
	if _, err := r.runtimeReceipt(); err != nil {
		return err
	}
	return nil
}

// Reconstruct the entire prior set, including unchanged grants. Shape-valid
// candidates cannot switch the requested Skill, remove a different grant, or
// carry an unrelated change alongside the one authorized by the original key.
func (r *assignmentCommand) validateTransition() error {
	c, request := r.Candidate, r.Input.Request
	if request.SkillID != nil {
		if c.Skill != *request.SkillID || c.Changed && (c.Added == nil || c.Added.Enabled != *request.Enabled) || c.Removed != nil && c.Removed.Enabled == *request.Enabled {
			return unavailable(nil)
		}
	} else if c.Added != nil || c.Changed && (c.Removed == nil || c.Removed.ID != *request.AssignmentID) {
		return unavailable(nil)
	}
	before := c.After
	before.Items = make([]sc.AgentSkillAssignment, 0, len(c.After.Items)+1)
	found := false
	for _, a := range c.After.Items {
		if c.Removed != nil && (a.ID == c.Removed.ID || a.SkillID == c.Removed.SkillID && c.Added == nil) {
			return unavailable(nil)
		}
		if c.Added != nil && a.ID == c.Added.ID {
			if a != *c.Added {
				return unavailable(nil)
			}
			found = true
			continue
		}
		if !c.Changed {
			if request.SkillID != nil && a.SkillID == *request.SkillID && a.Enabled == *request.Enabled {
				found = true
			}
			if request.AssignmentID != nil && a.ID == *request.AssignmentID {
				return unavailable(nil)
			}
		}
		before.Items = append(before.Items, a)
	}
	if c.Added != nil && !found || !c.Changed && request.SkillID != nil && !found {
		return unavailable(nil)
	}
	if c.Changed {
		before.Sequence--
		if c.Removed != nil {
			before.Items = append(before.Items, *c.Removed)
		}
		slices.SortFunc(before.Items, func(a, b sc.AgentSkillAssignment) int {
			return bytes.Compare([]byte(a.SkillID.String()), []byte(b.SkillID.String()))
		})
	}
	if before.validate() != nil {
		return unavailable(nil)
	}
	digest, _, err := captureDigest(before)
	if err != nil || digest != c.BeforeDigest {
		return unavailable(err)
	}
	return nil
}

func (r *assignmentCommand) runtimeReceipt() (*sc.AssignmentIngressReceipt, error) {
	if r.State != "completed" || r.Candidate.Runtime == nil {
		if len(r.RuntimeReceipt) != 0 {
			return nil, unavailable(nil)
		}
		return nil, nil
	}
	var out sc.AssignmentIngressReceipt
	if assignmentDecode(r.RuntimeReceipt, &out, 1024) != nil || out.Validate() != nil || out.ChangeID != r.ID {
		return nil, unavailable(nil)
	}
	return &out, nil
}
func bindingForAssignment(a sc.AgentSkillAssignment, r assignmentRevision) sc.SkillBinding {
	return sc.SkillBinding{SkillID: a.SkillID, RevisionID: r.ID, Revision: r.Revision, AssignmentID: a.ID, AssignmentSequence: a.Sequence, Name: r.Name, Description: r.Description, PackageSHA256: r.PackageSHA256, EntryPath: r.EntryPath}
}
func (r *assignmentCommand) mutation(runtime *sc.AssignmentIngressReceipt) sc.AssignmentMutation {
	c := r.Candidate
	out := sc.AssignmentMutation{ProjectID: c.After.Project, AgentID: c.After.Agent, AgentVersion: c.Version, AssignmentSequence: c.After.Sequence, ChangeID: r.ID, Changed: c.Changed, Runtime: runtime}
	if r.Input.Request.SkillID != nil {
		for _, a := range c.After.Items {
			if a.SkillID == c.Skill {
				copy := a
				out.Assignment = &copy
				break
			}
		}
	}
	return out.Clone()
}
func (r *assignmentCommand) receipt() (sc.AssignmentMutation, error) {
	var out sc.AssignmentMutation
	if r.validate() != nil || r.State != "completed" || assignmentDecode(r.Receipt, &out, 65536) != nil || out.Validate() != nil {
		return out, unavailable(nil)
	}
	runtime, err := r.runtimeReceipt()
	if err != nil {
		return sc.AssignmentMutation{}, err
	}
	expected := r.mutation(runtime)
	a, _, e := captureDigest(out)
	b, _, e2 := captureDigest(expected)
	if e != nil || e2 != nil || a != b || (out.Runtime != nil) != (r.Candidate.Runtime != nil) {
		return sc.AssignmentMutation{}, unavailable(nil)
	}
	return out.Clone(), nil
}
func loadAssignmentCommand(ctx context.Context, x postgres.SQLExecutor, in assignmentInput) (*assignmentCommand, error) {
	r, err := loadAssignmentCommandRecord(ctx, x, in)
	if err != nil || r == nil {
		return r, err
	}
	if r.State == "completed" {
		if err = validateAssignmentRouting(ctx, x, r); err != nil {
			return nil, err
		}
	}
	return r, ctx.Err()
}

// Decode and validate every immutable command field. Ordinary reads still
// require live routing through loadAssignmentCommand. The retirement reader
// additionally checks the original Applied proof before accepting retired
// queued routing; this decoder alone is never an authorization grant.
func loadAssignmentCommandRecord(ctx context.Context, x postgres.SQLExecutor, in assignmentInput) (*assignmentCommand, error) {
	var id, actor, digest, state string
	var input, candidate, receipt, runtimeReceipt []byte
	var created time.Time
	err := x.QueryRow(ctx, `SELECT id::text,actor_user_id::text,semantic_digest,input,candidate,state,receipt,runtime_receipt,created_at FROM agenteam_skill.assignment_commands WHERE project_id=$1 AND agent_id=$2 AND command_name=$3 AND idempotency_key=$4`, in.Request.ProjectID.String(), in.Request.AgentID.String(), in.Request.name(), string(in.Meta.IdempotencyKey)).Scan(&id, &actor, &digest, &input, &candidate, &state, &receipt, &runtimeReceipt, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if actor != in.Actor.Details().UserID {
		return nil, fault(f.NotFound)
	}
	if digest != string(in.Digest) {
		return nil, fault(f.IdempotencyKeyReused)
	}
	if !bytes.Equal(input, in.Raw) {
		return nil, unavailable(nil)
	}
	r := &assignmentCommand{Input: in, Digest: in.Digest, State: state, Receipt: slices.Clone(receipt), RuntimeReceipt: slices.Clone(runtimeReceipt)}
	r.ID, err = f.ParseID[sc.AssignmentChange](id)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = assignmentDecode(candidate, &r.Candidate, 1<<20); err != nil {
		return nil, err
	}
	if !created.Equal(r.Candidate.Created.Time()) || r.validate() != nil {
		return nil, unavailable(nil)
	}
	if r.State == "completed" {
		if _, err = r.receipt(); err != nil {
			return nil, err
		}
	}
	return r, ctx.Err()
}
func planAssignmentCommand(ctx context.Context, x postgres.SQLExecutor, in assignmentInput, current ag.AgentRef, target sc.SkillID) (*assignmentCommand, error) {
	before, err := loadAssignmentSet(ctx, x, in.Request.ProjectID, in.Request.AgentID)
	if err != nil {
		return nil, err
	}
	digest, _, err := captureDigest(before)
	if err != nil {
		return nil, err
	}
	id, err := f.NewID[sc.AssignmentChange]()
	if err != nil {
		return nil, unavailable(err)
	}
	at, err := assignmentNow(ctx, x)
	if err != nil {
		return nil, err
	}
	c := assignmentCandidate{Origin: in.Actor.Details(), BeforeDigest: digest, After: before, Skill: target, Version: current.ConfigVersion, Created: at}
	c.After.Items = slices.Clone(before.Items)
	if in.Request.SkillID != nil {
		if _, _, err = loadAssignmentRevision(ctx, x, in.Request.ProjectID, target); err != nil {
			return nil, err
		}
	}
	found := -1
	for n, a := range before.Items {
		if a.SkillID == target {
			found = n
			break
		}
	}
	if in.Request.SkillID == nil {
		if found < 0 || before.Items[found].ID != *in.Request.AssignmentID {
			// An already retired grant is an honest current no-op; another current
			// grant for the same Skill is never removed using its predecessor's ID.
			return &assignmentCommand{ID: id, Input: in, Digest: in.Digest, Candidate: c, State: "planned"}, nil
		}
	} else if found >= 0 && before.Items[found].Enabled == *in.Request.Enabled {
		return &assignmentCommand{ID: id, Input: in, Digest: in.Digest, Candidate: c, State: "planned"}, nil
	}
	c.Changed = true
	c.Version, err = nextAssignmentVersion(current.ConfigVersion)
	if err != nil {
		return nil, err
	}
	c.After.Sequence, err = nextAssignmentVersion(before.Sequence)
	if err != nil {
		return nil, err
	}
	if found >= 0 {
		old := before.Items[found]
		c.Removed = &old
		c.After.Items = append(c.After.Items[:found], c.After.Items[found+1:]...)
	}
	if in.Request.SkillID != nil {
		_, revision, err := loadAssignmentRevision(ctx, x, in.Request.ProjectID, target)
		if err != nil {
			return nil, err
		}
		grant, err := f.NewID[sc.Assignment]()
		if err != nil {
			return nil, unavailable(err)
		}
		added := sc.AgentSkillAssignment{ID: grant, ProjectID: in.Request.ProjectID, AgentID: in.Request.AgentID, SkillID: target, Enabled: *in.Request.Enabled, Sequence: c.After.Sequence, CreatedAt: at}
		c.Added = &added
		c.After.Items = append(c.After.Items, added)
		slices.SortFunc(c.After.Items, func(a, b sc.AgentSkillAssignment) int {
			return bytes.Compare([]byte(a.SkillID.String()), []byte(b.SkillID.String()))
		})
		if added.Enabled {
			frozen, err := assignmentRevisionFrom(revision)
			if err != nil {
				return nil, err
			}
			c.Revision = &frozen
			runtime := sc.RuntimeAssignmentChange{ID: id, ProjectID: added.ProjectID, AgentID: added.AgentID, AssignmentSequence: c.After.Sequence, AgentVersion: c.Version, Binding: bindingForAssignment(added, frozen), RecordedAt: at}
			c.Runtime = &runtime
		}
	}
	r := &assignmentCommand{ID: id, Input: in, Digest: in.Digest, Candidate: c, State: "planned"}
	if err = r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}
func insertAssignmentCommand(ctx context.Context, x postgres.SQLExecutor, r *assignmentCommand) error {
	if err := r.validate(); err != nil {
		return err
	}
	_, raw, err := captureDigest(r.Candidate)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fault(f.PayloadTooLarge)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.assignment_commands(id,project_id,agent_id,actor_user_id,command_name,idempotency_key,semantic_digest,input,candidate,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'planned',$10)`, r.ID.String(), r.Input.Request.ProjectID.String(), r.Input.Request.AgentID.String(), r.Input.Actor.Details().UserID, r.Input.Request.name(), string(r.Input.Meta.IdempotencyKey), string(r.Digest), r.Input.Raw, raw, r.Candidate.Created.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}
func completeAssignmentCommand(ctx context.Context, x postgres.SQLExecutor, r *assignmentCommand, out sc.AssignmentMutation, runtime *sc.AssignmentIngressReceipt) error {
	if out.Validate() != nil {
		return unavailable(nil)
	}
	_, raw, err := captureDigest(out)
	if err != nil {
		return err
	}
	var routing any
	completed := *r
	completed.State, completed.Receipt = "completed", raw
	if runtime != nil {
		_, completed.RuntimeReceipt, err = captureDigest(runtime.Clone())
		if err != nil {
			return err
		}
		routing = completed.RuntimeReceipt
	}
	if _, err = completed.receipt(); err != nil {
		return err
	}
	if err = validateAssignmentRouting(ctx, x, &completed); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_skill.assignment_commands SET state='completed',receipt=$2,runtime_receipt=$3,completed_at=GREATEST(created_at,clock_timestamp()) WHERE id=$1 AND state='planned'`, r.ID.String(), raw, routing)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

// The independent routing proof was frozen only after the real ingress call
// returned. Compare it to Skill's immutable package protection, never to the
// current Execution slot (which may have changed since this command).
func validateAssignmentRouting(ctx context.Context, x postgres.SQLExecutor, r *assignmentCommand) error {
	routing, err := r.runtimeReceipt()
	if err != nil || routing == nil {
		return err
	}
	var e, p, agent, assignment, skill, revision, object string
	var sequence, number int64
	err = x.QueryRow(ctx, `SELECT execution_id::text,project_id::text,agent_id::text,assignment_id::text,assignment_sequence,skill_id::text,revision_id::text,revision,object_id::text FROM agenteam_skill.assignment_pending_refs WHERE change_id=$1`, r.ID.String()).Scan(&e, &p, &agent, &assignment, &sequence, &skill, &revision, &number, &object)
	if errors.Is(err, pgx.ErrNoRows) {
		if routing.Queued {
			return unavailable(nil)
		}
		return ctx.Err()
	}
	if err != nil {
		return unavailable(err)
	}
	if !routing.Queued || r.Candidate.Runtime == nil || r.Candidate.Revision == nil {
		return unavailable(nil)
	}
	c := *r.Candidate.Runtime
	if e != routing.ExecutionID.String() || p != c.ProjectID.String() || agent != c.AgentID.String() || assignment != c.Binding.AssignmentID.String() || sequence != int64(c.AssignmentSequence) || skill != c.Binding.SkillID.String() || revision != c.Binding.RevisionID.String() || number != int64(c.Binding.Revision) || object != r.Candidate.Revision.ObjectID.String() {
		return unavailable(nil)
	}
	return ctx.Err()
}
func applyAssignmentState(ctx context.Context, x postgres.SQLExecutor, r *assignmentCommand) error {
	c := r.Candidate
	if !c.Changed || r.validate() != nil {
		return unavailable(nil)
	}
	// Recheck the immutable selected revision at publication. Permission is the
	// complete Agent/Project witness outside this repository; this is identity.
	if c.Revision != nil {
		_, revision, err := loadAssignmentRevision(ctx, x, c.After.Project, c.Skill)
		if err != nil {
			return err
		}
		frozen, e := assignmentRevisionFrom(revision)
		if e != nil {
			return e
		}
		a, _, e := captureDigest(frozen)
		b, _, e2 := captureDigest(*c.Revision)
		if e != nil || e2 != nil || a != b {
			return fault(f.ConfirmationStale)
		}
	}
	if c.Removed != nil {
		tag, err := x.Exec(ctx, `UPDATE agenteam_skill.agent_assignments SET removed_at=$2 WHERE id=$1 AND removed_at IS NULL`, c.Removed.ID.String(), c.Created.Time())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
	}
	if c.Added != nil {
		a := *c.Added
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.agent_assignments(id,project_id,agent_id,skill_id,enabled,assignment_sequence,created_at,change_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, a.ID.String(), a.ProjectID.String(), a.AgentID.String(), a.SkillID.String(), a.Enabled, int64(a.Sequence), a.CreatedAt.Time(), r.ID.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	digest, raw, err := captureDigest(c.After)
	if err != nil {
		return err
	}
	var runtime any
	if c.Runtime != nil {
		_, data, e := captureDigest(c.Runtime)
		if e != nil {
			return e
		}
		runtime = data
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.assignment_changes(id,project_id,agent_id,assignment_sequence,agent_version,previous_digest,state_data,state_digest,runtime_data,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID.String(), c.After.Project.String(), c.After.Agent.String(), int64(c.After.Sequence), int64(c.Version), string(c.BeforeDigest), raw, string(digest), runtime, c.Created.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	tag, err = x.Exec(ctx, `INSERT INTO agenteam_skill.assignment_current(project_id,agent_id,assignment_sequence,change_id,state_digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT(project_id,agent_id) DO UPDATE SET assignment_sequence=EXCLUDED.assignment_sequence,change_id=EXCLUDED.change_id,state_digest=EXCLUDED.state_digest WHERE agenteam_skill.assignment_current.assignment_sequence=EXCLUDED.assignment_sequence-1`, c.After.Project.String(), c.After.Agent.String(), int64(c.After.Sequence), r.ID.String(), string(digest))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	current, err := loadAssignmentSet(ctx, x, c.After.Project, c.After.Agent)
	if err != nil {
		return err
	}
	actual, _, err := captureDigest(current)
	if err != nil {
		return err
	}
	if actual != digest {
		return unavailable(nil)
	}
	return ctx.Err()
}

// Event/ingress lookup uses the original stored caller only as immutable source
// data. Authorization is checked separately against the current caller/witness.
func loadAssignmentByID(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, id sc.AssignmentChangeID) (*assignmentCommand, error) {
	r, err := loadAssignmentRecordByID(ctx, x, project, agent, id)
	if err != nil {
		return nil, err
	}
	if r.State == "completed" {
		if err = validateAssignmentRouting(ctx, x, r); err != nil {
			return nil, err
		}
	}
	return r, ctx.Err()
}

func loadAssignmentRecordByID(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID, id sc.AssignmentChangeID) (*assignmentCommand, error) {
	var key string
	var raw, candidate []byte
	err := x.QueryRow(ctx, `SELECT idempotency_key,input,candidate FROM agenteam_skill.assignment_commands WHERE project_id=$1 AND agent_id=$2 AND id=$3`, project.String(), agent.String(), id.String()).Scan(&key, &raw, &candidate)
	if err != nil {
		return nil, assignmentReadError(err)
	}
	var intent assignmentIntent
	var plan assignmentCandidate
	if assignmentDecode(raw, &intent, 16384) != nil || assignmentDecode(candidate, &plan, 1<<20) != nil {
		return nil, unavailable(nil)
	}
	user, e := f.ParseID[i.User](plan.Origin.UserID)
	session, e2 := f.ParseID[i.Session](plan.Origin.SessionID)
	if e != nil || e2 != nil || intent.User != user.String() || intent.Request.ProjectID != project || intent.Request.AgentID != agent {
		return nil, unavailable(nil)
	}
	actor, e := i.NewHuman(user, session)
	if e != nil {
		return nil, unavailable(e)
	}
	requestID, e := f.ParseID[f.Request](id.String())
	if e != nil {
		return nil, unavailable(e)
	}
	in, e := newAssignmentInput(actor, f.CommandMeta{RequestID: requestID, IdempotencyKey: f.IdempotencyKey(key), ExpectedVersion: &intent.Expected}, intent.Request)
	if e != nil {
		return nil, unavailable(e)
	}
	r, e := loadAssignmentCommandRecord(ctx, x, in)
	if e != nil {
		return nil, e
	}
	if r == nil || r.ID != id {
		return nil, unavailable(nil)
	}
	return r, nil
}

// Explicit protected metadata; RevisionMetadata's default decoder intentionally
// refuses persisted capabilities. The manifest itself is never rehydrated.
type assignmentRevision struct {
	ID              sc.RevisionID `json:"id"`
	SkillID         sc.SkillID    `json:"skill_id"`
	ProjectID       i.ProjectID   `json:"project_id"`
	Revision        f.Revision    `json:"revision"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	EntryPath       string        `json:"entry_path"`
	ObjectID        oc.ObjectID   `json:"object_id"`
	ObjectVersion   f.Version     `json:"object_version"`
	ObjectCreatedAt f.Instant     `json:"object_created_at"`
	PublishedAt     f.Instant     `json:"published_at"`
	PackageSHA256   f.Digest      `json:"package_sha256"`
	ManifestDigest  f.Digest      `json:"manifest_digest"`
}

func assignmentRevisionFrom(r sc.RevisionMetadata) (assignmentRevision, error) {
	d, e := r.Manifest.Digest()
	if e != nil || r.Validate() != nil {
		return assignmentRevision{}, unavailable(e)
	}
	return assignmentRevision{r.ID, r.SkillID, r.ProjectID, r.Revision, r.Name, r.Description, r.EntryPath, r.Object.ID, r.Object.Version, r.Object.CreatedAt, r.PublishedAt, r.PackageSHA256, d}, nil
}
func (r assignmentRevision) validate() error {
	if r.ID.Validate() != nil || r.SkillID.Validate() != nil || r.ProjectID.Validate() != nil || r.Revision.Validate() != nil || r.ObjectID.Validate() != nil || r.ObjectVersion.Validate() != nil || r.ObjectCreatedAt.Validate() != nil || r.PublishedAt.Validate() != nil || r.PackageSHA256.Validate() != nil || r.ManifestDigest.Validate() != nil || r.EntryPath != sc.EntryPath {
		return unavailable(nil)
	}
	return nil
}
