package skill

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Original typed command/candidate decoders construct the chain. PostgreSQL,
// trigger permission and the cross-owner atomic commit remain the real PG's
// responsibility; no fabricated postgres.Rows supplies a successful grant.
func assignmentRetirementHistoryControl(t *testing.T) *assignmentRetirementHistory {
	t.Helper()
	actor, err := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
	if err != nil {
		t.Fatal(err)
	}
	r := assignmentChangedControl(t, actor, false)
	_, r.Receipt, err = captureDigest(r.mutation(nil))
	if err != nil {
		t.Fatal(err)
	}
	r.State = "completed"
	if _, err = r.receipt(); err != nil {
		t.Fatal(err)
	}
	a := *r.Candidate.Removed
	return &assignmentRetirementHistory{
		head:     &agentInitializationRecord{project: a.ProjectID, agent: a.AgentID, skill: a.SkillID, enabled: true, sequence: 1, assignment: &a.ID, created: a.CreatedAt},
		commands: []*assignmentCommand{r}, changes: []*assignmentCommand{r},
		grants: map[string]retirementGrant{}, current: r.Candidate.After,
	}
}

func TestProjectAgentAssignmentRetirementCompleteChain(t *testing.T) {
	h := assignmentRetirementHistoryControl(t)
	if err := h.verifyChain(); err != nil {
		t.Fatal(err)
	}
	first := h.grants[h.head.assignment.String()]
	next := h.grants[h.changes[0].Candidate.Added.ID.String()]
	if len(h.grants) != 2 || first.removed == nil || !first.removed.Equal(h.changes[0].Candidate.Created.Time()) || next.change != h.changes[0].ID.String() || next.removed != nil {
		t.Fatal("original removed grant and replacement history were not retained")
	}
	for _, mode := range []string{"missing-change", "wrong-previous", "sequence-gap", "wrong-current", "duplicate-grant", "wrong-removed", "noop-wrong-state"} {
		t.Run(mode, func(t *testing.T) {
			h := assignmentRetirementHistoryControl(t)
			c := &h.changes[0].Candidate
			switch mode {
			case "missing-change":
				h.changes = nil
			case "wrong-previous":
				c.BeforeDigest = f.Digest("sha256:" + strings.Repeat("b", 64))
			case "sequence-gap":
				c.After.Sequence++
			case "wrong-current":
				h.current.Sequence++
			case "duplicate-grant":
				c.Added.ID = c.Removed.ID
			case "wrong-removed":
				c.Removed.Enabled = false
			case "noop-wrong-state":
				noop := *h.commands[0]
				noop.Candidate.Changed = false
				noop.Candidate.After.Sequence = 100
				h.commands = append(h.commands, &noop)
			}
			if !retirementRefCode(h.verifyChain(), f.ResourceBusy) {
				t.Fatal("incomplete original history accepted")
			}
		})
	}
}

type assignmentRetirementIngressControl struct {
	calls, checked int
	err            error
}

func (s *assignmentRetirementIngressControl) RetireProjectAgentIngressInTx(context.Context, f.Tx, ac.ProjectRetirementChange, []sc.ProjectAgentIngressRetirementEntry) error {
	s.calls++
	return s.err
}
func (s *assignmentRetirementIngressControl) RequireProjectAgentIngressRetiredInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error {
	s.checked++
	return s.err
}

func TestProjectAgentAssignmentRetirementRejectsBeforeIngress(t *testing.T) {
	_, s, ctx, c := retirementRefFixture(t)
	if _, err := NewProjectAgentRetirementReferencesWithIngress(s, s.owner, nil); !retirementRefCode(err, f.DependencyUnbound) {
		t.Fatal("unbound ingress accepted", err)
	}
	for _, mode := range []string{"planned-or-bound-history", "expired-applied", "original-scan-error"} {
		t.Run(mode, func(t *testing.T) {
			_, s, ctx, c = retirementRefFixture(t)
			sentinel := errors.New("original history read")
			s.rows = []retirementRefRow{{args: []any{c.Request.AgentID.String(), c.Request.ProjectID.String()}, values: []any{true}}}
			if mode == "expired-applied" {
				s.owner.live = false
			}
			if mode == "original-scan-error" {
				s.rows[0].err = sentinel
			}
			ingress := &assignmentRetirementIngressControl{}
			refs, err := NewProjectAgentRetirementReferencesWithIngress(s, s.owner, ingress)
			if err != nil {
				t.Fatal(err)
			}
			err = refs.RetireProjectAgentReferencesInTx(ctx, s.tx, c)
			if err == nil || ingress.calls != 0 || s.writes != 0 {
				t.Fatal("unvalidated history reached ingress or writes", err)
			}
			if mode == "original-scan-error" && !errors.Is(err, sentinel) {
				t.Fatal("original Scan error lost", err)
			}
			if mode == "planned-or-bound-history" && !retirementRefCode(err, f.ResourceBusy) {
				t.Fatal("blocked history not Busy", err)
			}
		})
	}
	sentinel := errors.New("original ingress confirmation")
	ingress := &assignmentRetirementIngressControl{err: sentinel}
	_, s, ctx, c = retirementRefFixture(t)
	refs, err := NewProjectAgentRetirementReferencesWithIngress(s, s.owner, ingress)
	if err != nil {
		t.Fatal(err)
	}
	if err = refs.RequireProjectAgentReferencesRetiredInTx(ctx, s.tx, c); !errors.Is(err, sentinel) || s.reads != 0 || ingress.checked != 1 {
		t.Fatal("empty Skills cannot bypass original ingress recovery", err)
	}
}

type assignmentRetirementDeleteControl struct {
	postgres.SQLExecutor
	t                 *testing.T
	change            ac.ProjectRetirementChange
	history           *assignmentRetirementHistory
	calls, fail, hold int
	wrongCount        bool
	sentinel          error
	entered, release  chan struct{}
}

func (s *assignmentRetirementDeleteControl) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	n := s.calls
	s.calls++
	tables := []string{"assignment_current", "agent_assignments", "assignment_changes", "assignment_commands", "agent_assignment_heads"}
	if n >= len(tables) || !strings.HasPrefix(q, "DELETE FROM agenteam_skill."+tables[n]+" WHERE ") {
		s.t.Fatal("unsafe owner delete ordering")
	}
	want := []any{s.change.Request.ProjectID.String(), s.change.Request.AgentID.String()}
	if n == 4 {
		h := s.history.head
		want = append(want, h.command, int64(h.planRevision), string(h.binding), h.enabled, h.assignment.String())
	}
	if !reflect.DeepEqual(args, want) {
		s.t.Error("delete tuple changed")
	}
	if n == s.hold {
		close(s.entered)
		<-s.release
	}
	if n == s.fail && !s.wrongCount {
		return pgconn.CommandTag{}, s.sentinel
	}
	counts := []int64{1, 2, 1, 1, 1}
	if n == s.fail {
		counts[n]++
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", counts[n])), nil
}
func assignmentRetirementDeleteFixture(t *testing.T) *assignmentRetirementDeleteControl {
	t.Helper()
	_, _, _, c := retirementRefFixture(t)
	h := assignmentRetirementHistoryControl(t)
	if err := h.verifyChain(); err != nil {
		t.Fatal(err)
	}
	return &assignmentRetirementDeleteControl{t: t, change: c, history: h, fail: -1, hold: -1, sentinel: errors.New("original delete return")}
}
func TestProjectAgentAssignmentRetirementDeletesExactHistory(t *testing.T) {
	s := assignmentRetirementDeleteFixture(t)
	if err := deleteAssignmentRetirementHistory(context.Background(), s, s.change, s.history); err != nil || s.calls != 5 {
		t.Fatal("complete delete sequence failed", err)
	}
	for n := 0; n < 5; n++ {
		for _, wrong := range []bool{false, true} {
			t.Run(fmt.Sprintf("step-%d-count-%t", n, wrong), func(t *testing.T) {
				s := assignmentRetirementDeleteFixture(t)
				s.fail = n
				s.wrongCount = wrong
				err := deleteAssignmentRetirementHistory(context.Background(), s, s.change, s.history)
				if s.calls != n+1 || wrong && !retirementRefCode(err, f.ResourceBusy) || !wrong && !errors.Is(err, s.sentinel) {
					t.Fatal("continued after failed original delete", err)
				}
			})
		}
	}
}
func TestProjectAgentAssignmentRetirementWaitsOriginalDelete(t *testing.T) {
	for _, withError := range []bool{false, true} {
		t.Run(fmt.Sprintf("original-error-%t", withError), func(t *testing.T) {
			s := assignmentRetirementDeleteFixture(t)
			s.hold = 1
			s.entered = make(chan struct{})
			s.release = make(chan struct{})
			if withError {
				s.fail = 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- deleteAssignmentRetirementHistory(ctx, s, s.change, s.history) }()
			<-s.entered
			cancel()
			select {
			case <-done:
				t.Fatal("cancellation fabricated original Exec return")
			default:
			}
			close(s.release)
			err := <-done
			if s.calls != 2 || withError && !errors.Is(err, s.sentinel) || !withError && !errors.Is(err, context.Canceled) {
				t.Fatal("original return/error or following write boundary lost", err)
			}
		})
	}
}
