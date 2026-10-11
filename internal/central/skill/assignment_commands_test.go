package skill

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
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
}

func (s *assignmentLookupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *assignmentLookupStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
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
	return skillRowValues{values: []any{r.ID.String(), r.Input.Actor.Details().UserID, string(r.Digest), r.Input.Raw, candidate, r.State, r.Receipt, r.Candidate.Created.Time()}}
}
func assignmentLookupControl(t *testing.T) (*AssignmentCommands, *assignmentLookupStore, *skillReaderProjects, assignmentInput) {
	t.Helper()
	service, base, projects, actor, _ := readerFixture(t)
	in, row := assignmentReceiptControl(t, actor)
	store := &assignmentLookupStore{initializationReadStore: base, record: row}
	service.state().authority.state().store = store
	return &AssignmentCommands{deps: AssignmentDependencies{Service: service}}, store, projects, in
}
func TestSkillAssignmentLookupUsesOriginalCommitAndCurrentOwner(t *testing.T) {
	for _, name := range []string{"committed", "not-observed", "wrong-body", "denied", "unknown-read-commit"} {
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
