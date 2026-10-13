package skill

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

func assignmentReceiptControl(t *testing.T, actor i.Actor) (assignmentInput, *assignmentCommand) {
	t.Helper()
	r := stateRow(t)
	enabled := true
	expected := f.Version(1)
	in, err := newAssignmentInput(actor, f.CommandMeta{RequestID: stateID[f.Request](402), IdempotencyKey: "original-assignment-intent", ExpectedVersion: &expected}, AssignmentRequest{ProjectID: r.request.ProjectID, AgentID: stateID[i.Agent](401), SkillID: &r.skill, Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	assignment := sc.AgentSkillAssignment{ID: stateID[sc.Assignment](403), ProjectID: in.Request.ProjectID, AgentID: in.Request.AgentID, SkillID: r.skill, Enabled: true, Sequence: 1, CreatedAt: r.created}
	state := assignmentSet{Project: in.Request.ProjectID, Agent: in.Request.AgentID, Sequence: 1, Items: []sc.AgentSkillAssignment{assignment}}
	digest, _, err := captureDigest(state)
	if err != nil {
		t.Fatal(err)
	}
	row := &assignmentCommand{ID: stateID[sc.AssignmentChange](404), Input: in, Digest: in.Digest, State: "planned", Candidate: assignmentCandidate{Origin: actor.Details(), BeforeDigest: digest, After: state, Skill: r.skill, Version: 1, Created: r.created}}
	if row.validate() != nil {
		t.Fatal("valid current no-op candidate rejected")
	}
	out := row.mutation(nil)
	_, raw, err := captureDigest(out)
	if err != nil {
		t.Fatal(err)
	}
	row.State = "completed"
	row.Receipt = raw
	if _, err = row.receipt(); err != nil {
		t.Fatal("completed receipt failed", err)
	}
	return in, row
}
func TestSkillAssignmentIntentAndStoredReceipt(t *testing.T) {
	actor, _ := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
	in, row := assignmentReceiptControl(t, actor)
	nextSession, _ := i.NewHuman(stateID[i.User](111), stateID[i.Session](113))
	next, err := newAssignmentInput(nextSession, in.Meta, in.Request)
	if err != nil || next.Digest != in.Digest || !bytes.Equal(next.Raw, in.Raw) {
		t.Fatal("transport Session became business intent")
	}
	for _, name := range []string{"changed-agent", "changed-skill", "changed-enabled", "changed-version"} {
		t.Run(name, func(t *testing.T) {
			r := in.Request.clone()
			m := in.Meta
			switch name {
			case "changed-agent":
				r.AgentID = stateID[i.Agent](405)
			case "changed-skill":
				skill := stateID[pc.Skill](406)
				r.SkillID = &skill
			case "changed-enabled":
				v := false
				r.Enabled = &v
			default:
				v := f.Version(2)
				m.ExpectedVersion = &v
			}
			changed, err := newAssignmentInput(actor, m, r)
			if err != nil || changed.Digest == in.Digest {
				t.Fatal("different full intent not distinguished")
			}
		})
	}
	for _, name := range []string{"version", "assignment", "invented-runtime"} {
		t.Run("receipt-"+name, func(t *testing.T) {
			copy := *row
			out := row.mutation(nil)
			switch name {
			case "version":
				out.AgentVersion++
			case "assignment":
				out.Assignment.ID = stateID[sc.Assignment](407)
			default:
				e := stateID[i.Execution](408)
				out.Runtime = &sc.AssignmentIngressReceipt{ChangeID: out.ChangeID, Queued: true, ExecutionID: &e}
			}
			_, copy.Receipt, err = captureDigest(out)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = copy.receipt(); err == nil {
				t.Fatal("stored receipt detached from original candidate")
			}
		})
	}
	_, raw, err := captureDigest(row.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	var decoded assignmentCandidate
	if assignmentDecode(raw, &decoded, 1<<20) != nil || !sameAssignmentCandidate(decoded, row.Candidate) {
		t.Fatal("candidate canonical round trip lost original set")
	}
	bad := append([]byte(`{"unexpected":true,`), raw[1:]...)
	if assignmentDecode(bad, &decoded, 1<<20) == nil {
		t.Fatal("unknown persisted field accepted")
	}
}

// This double controls only the original storage call/outcome. It is not a
// Project/Agent current-authority or PostgreSQL rollback integration proof.
type assignmentLookupStore struct {
	*initializationReadStore
	record           *assignmentCommand
	entered, release chan struct{}
	queries          int
	pending          []any
	pendingErr       error
}

func (s *assignmentLookupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *assignmentLookupStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "FROM agenteam_skill.assignment_pending_refs") {
		if s.pendingErr != nil {
			return skillRowValues{err: s.pendingErr}
		}
		if s.pending == nil {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: s.pending}
	}
	if !strings.Contains(q, "FROM agenteam_skill.assignment_commands") {
		return skillRowValues{err: errors.New("unexpected assignment lookup SQL")}
	}
	s.queries++
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	if s.record == nil {
		return skillRowValues{err: pgx.ErrNoRows}
	}
	r := s.record
	_, candidate, err := captureDigest(r.Candidate)
	if err != nil {
		return skillRowValues{err: err}
	}
	return skillRowValues{values: []any{r.ID.String(), r.Input.Actor.Details().UserID, string(r.Digest), r.Input.Raw, candidate, r.State, r.Receipt, r.RuntimeReceipt, r.Candidate.Created.Time()}}
}

func assignmentChangedControl(t *testing.T, actor i.Actor, enabled bool) *assignmentCommand {
	t.Helper()
	in, base := assignmentReceiptControl(t, actor)
	in.Request.Enabled = &enabled
	in, err := newAssignmentInput(actor, in.Meta, in.Request)
	if err != nil {
		t.Fatal(err)
	}
	c := base.Candidate
	before := c.After
	before.Items = append([]sc.AgentSkillAssignment{}, c.After.Items...)
	before.Items[0].Enabled = !enabled
	c.BeforeDigest, _, err = captureDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	removed := before.Items[0]
	added := removed
	added.ID, added.Enabled, added.Sequence = stateID[sc.Assignment](420), enabled, 2
	c.After.Sequence, c.After.Items = 2, []sc.AgentSkillAssignment{added}
	c.Version, c.Changed, c.Added, c.Removed = 2, true, &added, &removed
	if enabled {
		d := f.Digest("sha256:" + strings.Repeat("a", 64))
		revision := assignmentRevision{ID: stateID[sc.Revision](421), SkillID: c.Skill, ProjectID: c.After.Project, Revision: 1, Name: "readable", Description: "safe", EntryPath: sc.EntryPath, ObjectID: stateID[oc.StoredObject](422), ObjectVersion: 1, ObjectCreatedAt: c.Created, PublishedAt: c.Created, PackageSHA256: d, ManifestDigest: d}
		c.Revision = &revision
		c.Runtime = &sc.RuntimeAssignmentChange{ID: base.ID, ProjectID: c.After.Project, AgentID: c.After.Agent, AssignmentSequence: 2, AgentVersion: 2, Binding: bindingForAssignment(added, revision), RecordedAt: c.Created}
	}
	r := &assignmentCommand{ID: base.ID, Input: in, Digest: in.Digest, Candidate: c, State: "planned"}
	if err = r.validate(); err != nil {
		t.Fatal("valid changed candidate rejected", err)
	}
	return r
}

func TestSkillAssignmentCandidateBindsCompleteOriginalTransition(t *testing.T) {
	actor, _ := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
	remove := assignmentChangedControl(t, actor, false)
	id := remove.Candidate.Removed.ID
	remove.Input.Request = AssignmentRequest{ProjectID: remove.Input.Request.ProjectID, AgentID: remove.Input.Request.AgentID, AssignmentID: &id}
	var err error
	remove.Input, err = newAssignmentInput(actor, remove.Input.Meta, remove.Input.Request)
	if err != nil {
		t.Fatal(err)
	}
	remove.Digest = remove.Input.Digest
	remove.Candidate.Added, remove.Candidate.After.Items = nil, []sc.AgentSkillAssignment{}
	if remove.validate() != nil {
		t.Fatal("exact original grant removal rejected")
	}
	for _, name := range []string{"request-skill", "request-enabled", "added-not-in-after", "unrelated-item", "removed-still-current", "removed-id", "noop-enabled", "remove-wrong-id"} {
		t.Run(name, func(t *testing.T) {
			r := assignmentChangedControl(t, actor, false)
			switch name {
			case "request-skill":
				skill := stateID[pc.Skill](423)
				r.Input.Request.SkillID = &skill
			case "request-enabled":
				enabled := true
				r.Input.Request.Enabled = &enabled
			case "added-not-in-after":
				r.Candidate.After.Items[0].ID = stateID[sc.Assignment](424)
			case "unrelated-item":
				extra := r.Candidate.After.Items[0]
				extra.SkillID = stateID[pc.Skill](425)
				extra.ID = stateID[sc.Assignment](426)
				extra.Sequence = 1
				r.Candidate.After.Items = append(r.Candidate.After.Items, extra)
			case "removed-still-current":
				r.Candidate.After.Items[0] = *r.Candidate.Removed
			case "removed-id":
				r.Candidate.Removed.ID = stateID[sc.Assignment](427)
			case "noop-enabled":
				_, r = assignmentReceiptControl(t, actor)
				r.Candidate.After.Items[0].Enabled = false
			case "remove-wrong-id":
				id := stateID[sc.Assignment](428)
				r.Input.Request = AssignmentRequest{ProjectID: r.Input.Request.ProjectID, AgentID: r.Input.Request.AgentID, AssignmentID: &id}
				r.Candidate.Added = nil
				r.Candidate.After.Items = []sc.AgentSkillAssignment{}
			}
			// Bind every edited request to its own valid digest: rejection must
			// come from the original-intent/candidate relation, not a stale hash.
			var err error
			r.Input, err = newAssignmentInput(actor, r.Input.Meta, r.Input.Request)
			if err != nil {
				t.Fatal(err)
			}
			r.Digest = r.Input.Digest
			if r.validate() == nil {
				t.Fatal("shape-valid candidate escaped full original transition")
			}
		})
	}
}

func completeAssignmentControl(t *testing.T, r *assignmentCommand, queued bool) {
	t.Helper()
	route := sc.AssignmentIngressReceipt{ChangeID: r.ID, Queued: queued}
	if queued {
		e := stateID[i.Execution](429)
		route.ExecutionID = &e
	}
	var err error
	_, r.RuntimeReceipt, err = captureDigest(route)
	if err != nil {
		t.Fatal(err)
	}
	_, r.Receipt, err = captureDigest(r.mutation(&route))
	if err != nil {
		t.Fatal(err)
	}
	r.State = "completed"
	if _, err = r.receipt(); err != nil {
		t.Fatal("original routing receipt rejected", err)
	}
}

func TestSkillAssignmentReceiptBindsOriginalRoutingAndProtection(t *testing.T) {
	for _, name := range []string{"queued", "no-active", "changed-execution", "changed-outcome", "missing-proof", "missing-protection", "changed-protection", "no-active-with-protection"} {
		t.Run(name, func(t *testing.T) {
			s, store, _, in := assignmentLookupControl(t)
			r := assignmentChangedControl(t, in.Actor, true)
			queued := name != "no-active" && name != "no-active-with-protection"
			completeAssignmentControl(t, r, queued)
			store.record = r
			binding := r.Candidate.Runtime.Binding
			if queued || name == "no-active-with-protection" {
				store.pending = []any{stateID[i.Execution](429).String(), r.Input.Request.ProjectID.String(), r.Input.Request.AgentID.String(), binding.AssignmentID.String(), int64(binding.AssignmentSequence), binding.SkillID.String(), binding.RevisionID.String(), int64(binding.Revision), r.Candidate.Revision.ObjectID.String()}
			}
			switch name {
			case "changed-execution", "changed-outcome":
				out, err := r.receipt()
				if err != nil {
					t.Fatal(err)
				}
				if name == "changed-execution" {
					e := stateID[i.Execution](430)
					out.Runtime.ExecutionID = &e
				} else {
					out.Runtime.Queued = false
					out.Runtime.ExecutionID = nil
				}
				_, r.Receipt, err = captureDigest(out)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-proof":
				r.RuntimeReceipt = nil
			case "missing-protection":
				store.pending = nil
			case "changed-protection":
				store.pending[0] = stateID[i.Execution](431).String()
			}
			out, err := s.LookupAssignment(context.Background(), r.Input.Actor, r.Input.Meta, r.Input.Request)
			if name == "queued" || name == "no-active" {
				if err != nil || out.Receipt == nil || out.Receipt.Runtime == nil || out.Receipt.Runtime.Queued != queued {
					t.Fatal("original routing not re-readable", err)
				}
			} else if err == nil || out.Receipt != nil || out.State != "" {
				t.Fatal("tampered original routing published")
			}
		})
	}
}

type assignmentLookupAgentControl struct {
	err        error
	wrongScope bool
	calls      int
}

func (a *assignmentLookupAgentControl) ReadSkillAssignmentInTx(_ context.Context, _ f.Tx, r ag.SkillAssignmentRequest) (ag.AgentRef, error) {
	a.calls++
	ref := ag.AgentRef{ProjectID: r.ProjectID, AgentID: r.AgentID, ConfigVersion: 9}
	if a.wrongScope {
		ref.AgentID = stateID[i.Agent](989)
	}
	return ref, a.err
}
func (*assignmentLookupAgentControl) DiscoverSkillAssignment(context.Context, ag.SkillAssignmentRequest) (ag.SkillAssignmentPlan, error) {
	panic("lookup must not discover another transaction")
}
func (*assignmentLookupAgentControl) WithSkillAssignmentInTx(context.Context, f.Tx, ag.SkillAssignmentRequest, ag.SkillAssignmentPlan, func(context.Context, ag.AgentRef) (bool, error), func(context.Context, ag.AgentRef) error) error {
	panic("lookup must not enter a writer")
}

func assignmentLookupControl(t *testing.T) (*AssignmentCommands, *assignmentLookupStore, *skillReaderProjects, assignmentInput) {
	t.Helper()
	service, base, projects, actor, _ := readerFixture(t)
	in, row := assignmentReceiptControl(t, actor)
	store := &assignmentLookupStore{initializationReadStore: base, record: row}
	service.state().authority.state().store = store
	return &AssignmentCommands{deps: AssignmentDependencies{Service: service, Agents: &assignmentLookupAgentControl{}}}, store, projects, in
}
func TestSkillAssignmentLookupUsesOriginalCommitAndCurrentOwner(t *testing.T) {
	for _, name := range []string{"committed", "not-observed", "wrong-body", "denied", "unknown-read-commit", "uninitialized-agent", "wrong-agent-scope"} {
		t.Run(name, func(t *testing.T) {
			s, store, projects, in := assignmentLookupControl(t)
			switch name {
			case "not-observed":
				store.record = nil
			case "wrong-body":
				v := false
				in.Request.Enabled = &v
			case "denied":
				projects.err = fault(f.Forbidden)
			case "unknown-read-commit":
				store.unknown = true
			case "uninitialized-agent":
				s.deps.Agents.(*assignmentLookupAgentControl).err = fault(f.InvalidState)
			case "wrong-agent-scope":
				s.deps.Agents.(*assignmentLookupAgentControl).wrongScope = true
			}
			out, err := s.LookupAssignment(context.Background(), in.Actor, in.Meta, in.Request)
			switch name {
			case "committed":
				if err != nil || out.State != "committed" || out.Receipt == nil || out.Receipt.ChangeID != store.record.ID {
					t.Fatal("original receipt missing", err)
				}
			case "not-observed":
				if err != nil || out.State != "not_observed" || out.Receipt != nil {
					t.Fatal("absence fabricated receipt")
				}
			default:
				if err == nil || out.Receipt != nil || out.State != "" {
					t.Fatal("failure published a candidate")
				}
			}
			if name == "denied" && store.queries != 0 {
				t.Fatal("queried original command before current Owner")
			}
			if (name == "uninitialized-agent" || name == "wrong-agent-scope") && store.queries != 0 {
				t.Fatal("read receipt before current initialized Agent check")
			}
		})
	}
}
func TestSkillAssignmentLookupStopWaitsForActualStoreReturn(t *testing.T) {
	s, store, _, in := assignmentLookupControl(t)
	store.entered = make(chan struct{})
	store.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := s.LookupAssignment(context.Background(), in.Actor, in.Meta, in.Request); done <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-store.entered:
	case <-ctx.Done():
		t.Fatal("original query not entered")
	}
	s.deps.Service.Stop()
	if s.deps.Service.Joined() {
		t.Fatal("stop retired an actual blocked SQL call")
	}
	expired, stop := context.WithCancel(context.Background())
	stop()
	if s.deps.Service.Drain(expired) == nil {
		t.Fatal("drain ignored its caller deadline")
	}
	if _, err := s.LookupAssignment(context.Background(), in.Actor, in.Meta, in.Request); err == nil {
		t.Fatal("new lookup admitted after stop")
	}
	close(store.release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled original lookup published a receipt")
		}
	case <-ctx.Done():
		t.Fatal("original query did not return")
	}
	if err := s.deps.Service.Drain(ctx); err != nil || !s.deps.Service.Joined() {
		t.Fatal("actual return did not retire lookup", err)
	}
}
