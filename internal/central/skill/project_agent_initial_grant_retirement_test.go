package skill

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls exercise the adapter's original caller Tx and return order.
// Scripted history/count rows do not prove the database grant exception or its
// immutable UPDATE rule; those require the real migration and PostgreSQL.
type initialGrantRetirementStore struct {
	*retirementRefStore
	args    [2][]any
	counts  [2]int64
	errs    [2]error
	hold    int
	entered chan struct{}
	release chan struct{}
}

func (s *initialGrantRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.retirementRefStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *initialGrantRetirementStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	n := s.writes
	s.writes++
	tables := [2]string{"agenteam_skill.agent_assignments", "agenteam_skill.agent_assignment_heads"}
	if n >= len(tables) || !s.authorized(ctx) ||
		!strings.HasPrefix(query, "DELETE FROM "+tables[n]+" WHERE ") || !reflect.DeepEqual(args, s.args[n]) {
		s.t.Error("retirement escaped the original Applied tuple or grant-before-head order")
		return pgconn.CommandTag{}, f.NewFault(f.InternalError, f.NotStarted)
	}
	if n == s.hold {
		close(s.entered)
		<-s.release
	}
	if s.errs[n] != nil {
		return pgconn.CommandTag{}, s.errs[n]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.counts[n])), nil
}

func initialGrantRetirementFixture(t *testing.T) (*ProjectAgentRetirementReferences, *initialGrantRetirementStore, context.Context, ac.ProjectRetirementChange) {
	t.Helper()
	_, base, ctx, change := retirementRefFixture(t)
	r := change.Request
	skill := retirementRefID[pc.Skill](t, 11).String()
	grant := retirementRefID[sc.Assignment](t, 12).String()
	command := "original-agent-creation"
	binding := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	created := time.Unix(100, 0).UTC()
	base.rows = []retirementRefRow{
		{args: []any{r.AgentID.String()}, values: []any{false}},
		{args: []any{r.AgentID.String()}, values: []any{r.ProjectID.String()}},
		{args: []any{r.ProjectID.String(), r.AgentID.String()}, values: []any{
			r.ProjectID.String(), r.AgentID.String(), command, int64(3), binding,
			true, int64(1), skill, int64(1), grant, created, int64(1), int64(1),
		}},
	}
	s := &initialGrantRetirementStore{
		retirementRefStore: base,
		args: [2][]any{
			{r.ProjectID.String(), r.AgentID.String(), grant, skill, created},
			{r.ProjectID.String(), r.AgentID.String(), command, int64(3), binding, grant},
		},
		counts: [2]int64{1, 1}, hold: -1,
	}
	a, err := NewProjectAgentRetirementReferences(s, base.owner)
	if err != nil {
		t.Fatal(err)
	}
	return a, s, ctx, change
}

func TestProjectAgentInitialGrantRetirementOriginalOrder(t *testing.T) {
	a, s, ctx, change := initialGrantRetirementFixture(t)
	if err := a.RetireProjectAgentReferencesInTx(ctx, s.tx, change); err != nil {
		t.Fatal("exact enabled initialization did not retire", err)
	}
	if s.reads != 3 || s.queries != 0 || s.writes != 2 || s.owner.calls != 1 {
		t.Fatalf("unexpected original call counts: reads=%d queries=%d writes=%d owner=%d", s.reads, s.queries, s.writes, s.owner.calls)
	}
}

func TestProjectAgentInitialGrantRetirementRejectsHistoryAndShape(t *testing.T) {
	for _, mode := range []string{"remaining-history", "extra-initial-grant", "unmatched-initial-grant", "advanced-sequence"} {
		t.Run(mode, func(t *testing.T) {
			a, s, ctx, change := initialGrantRetirementFixture(t)
			switch mode {
			case "remaining-history":
				// One real branch for the SQL history verdict, not separate
				// pretend proofs for every table contributing to that verdict.
				s.rows[0].values[0] = true
			case "extra-initial-grant":
				s.rows[2].values[11] = int64(2)
			case "unmatched-initial-grant":
				s.rows[2].values[12] = int64(0)
			case "advanced-sequence":
				s.rows[2].values[6] = int64(2)
			}
			err := a.RetireProjectAgentReferencesInTx(ctx, s.tx, change)
			if err == nil || s.writes != 0 {
				t.Fatal("history or noninitial head reached a delete", err)
			}
			if mode == "remaining-history" && (!retirementRefCode(err, f.ResourceBusy) || s.reads != 1) {
				t.Fatal("history verdict did not stop before head access", err)
			}
		})
	}
}

func TestProjectAgentInitialGrantRetirementWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		step  int
		count int64
		sql   bool
	}{
		{"grant-error", 0, 1, true},
		{"grant-missing", 0, 0, false},
		{"grant-duplicate", 0, 2, false},
		{"head-error", 1, 1, true},
		{"head-missing", 1, 0, false},
		{"head-duplicate", 1, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, ctx, change := initialGrantRetirementFixture(t)
			sentinel := errors.New("original retirement SQL failure")
			s.counts[tc.step] = tc.count
			if tc.sql {
				s.errs[tc.step] = sentinel
			}
			err := a.RetireProjectAgentReferencesInTx(ctx, s.tx, change)
			if err == nil || s.writes != tc.step+1 {
				t.Fatal("failed original delete advanced or published completion", err, s.writes)
			}
			if tc.sql {
				if !errors.Is(err, sentinel) {
					t.Fatal("original SQL failure lost", err)
				}
			} else if !retirementRefCode(err, f.ResourceBusy) {
				t.Fatal("nonexact delete count was not rejected", err)
			}
		})
	}
}

func TestProjectAgentInitialGrantRetirementWaitsForOriginalReturn(t *testing.T) {
	for _, tc := range []struct {
		name string
		step int
		sql  bool
	}{
		{"cancel-during-grant", 0, false},
		{"cancel-during-head", 1, false},
		{"cancel-with-grant-error", 0, true},
		{"cancel-with-head-error", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, ctx, change := initialGrantRetirementFixture(t)
			s.hold, s.entered, s.release = tc.step, make(chan struct{}), make(chan struct{})
			sentinel := errors.New("original held retirement SQL failure")
			if tc.sql {
				s.errs[tc.step] = sentinel
			}
			ctx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			joined, released := false, false
			t.Cleanup(func() {
				cancel()
				if !released {
					close(s.release)
				}
				if !joined {
					<-done
				}
			})
			go func() { done <- a.RetireProjectAgentReferencesInTx(ctx, s.tx, change) }()
			select {
			case <-s.entered:
			case err := <-done:
				joined = true
				t.Fatal("returned before the original delete barrier", err)
			case <-time.After(time.Second):
				t.Fatal("original delete did not enter")
			}
			cancel()
			select {
			case err := <-done:
				joined = true
				t.Fatal("cancellation replaced the original SQL return", err)
			default:
			}
			close(s.release)
			released = true
			select {
			case err := <-done:
				joined = true
				if tc.sql {
					if !errors.Is(err, sentinel) || errors.Is(err, context.Canceled) {
						t.Fatal("cancellation replaced the original SQL error", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled original return published completion", err)
				}
				if s.writes != tc.step+1 {
					t.Fatal("cancelled delete started another statement", s.writes)
				}
			case <-time.After(time.Second):
				t.Fatal("released original delete did not return")
			}
		})
	}
}
