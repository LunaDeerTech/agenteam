package meeting

import (
	"context"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type decisionInboxTestStore struct {
	Store
	tx                                            f.Tx
	active, projectEX, held, live, retained, open bool
	project                                       i.ProjectID
	meeting                                       c.MeetingID
	decision                                      c.DecisionID
	reads                                         int
}

func (s *decisionInboxTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.active || s.tx != tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *decisionInboxTestStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if !s.held {
		return fault(f.Forbidden)
	}
	for _, lock := range locks {
		if lock.Key.Canonical() == projectLock(s.project, f.Exclusive).Key.Canonical() && lock.Mode == f.Exclusive && !s.projectEX {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *decisionInboxTestStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.reads++
	return controlEventRow(func(dest ...any) error {
		if len(args) != 3 || args[0] != s.project.String() || args[1] != s.meeting.String() || args[2] != s.decision.String() {
			return pgx.ErrNoRows
		}
		if strings.Contains(query, "FROM agenteam_meeting.decision_requests") {
			if !s.live {
				return pgx.ErrNoRows
			}
			*dest[0].(*f.Version) = 3
			*dest[1].(*time.Time) = time.Unix(50, 0).UTC()
			*dest[2].(*string) = strings.Repeat("问", 600)
			*dest[3].(*bool) = s.open
		} else {
			if !s.retained {
				return pgx.ErrNoRows
			}
			*dest[0].(*f.Version) = 4
			*dest[1].(*time.Time) = time.Unix(51, 0).UTC()
		}
		return nil
	})
}

type decisionInboxTestKey struct{}
type decisionInboxTestAuthority struct{ store *decisionInboxTestStore }

func (a *decisionInboxTestAuthority) RequireDecisionSourceReadInTx(ctx context.Context, tx f.Tx, store any, p i.ProjectID, m, d string, scan bool) error {
	if ctx.Value(decisionInboxTestKey{}) != a || store != a.store || p != a.store.project || tx != a.store.tx || !a.store.active {
		return fault(f.Forbidden)
	}
	if scan {
		if m != "" || d != "" {
			return fault(f.Forbidden)
		}
	} else if m != a.store.meeting.String() || d != a.store.decision.String() {
		return fault(f.Forbidden)
	}
	return nil
}
func TestDecisionInboxRequiresPrivateSourceAndOriginalTransaction(t *testing.T) {
	store := &decisionInboxTestStore{tx: f.NewTx(), active: true, held: true, live: true, open: true, project: summaryTestID[i.Project](t), meeting: summaryTestID[c.Meeting](t), decision: summaryTestID[c.Decision](t)}
	authority := &decisionInboxTestAuthority{store}
	source, err := NewDecisionInboxSource(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), decisionInboxTestKey{}, authority)
	for _, which := range []string{"bare", "foreign-tx", "foreign-meeting", "foreign-decision", "ended", "missing-lock"} {
		t.Run(which, func(t *testing.T) {
			store.active, store.held = true, true
			used, tx, m, d := ctx, store.tx, store.meeting.String(), store.decision.String()
			switch which {
			case "bare":
				used = context.Background()
			case "foreign-tx":
				tx = f.NewTx()
			case "foreign-meeting":
				m = summaryTestID[c.Meeting](t).String()
			case "foreign-decision":
				d = summaryTestID[c.Decision](t).String()
			case "ended":
				store.active = false
			case "missing-lock":
				store.held = false
			}
			before := store.reads
			if _, err := source.ReadDecisionSourceInTx(used, tx, store.project, m, d); err == nil || store.reads != before {
				t.Fatal("source DTO escaped private original read", err)
			}
		})
	}
	store.active, store.held = true, true
	live, err := source.ReadDecisionSourceInTx(ctx, store.tx, store.project, store.meeting.String(), store.decision.String())
	if err != nil || !live.Relevant || !live.Open || live.Version != 3 || live.Context.MeetingID != store.meeting.String() || live.ID != store.decision.String() || len([]rune(live.Summary)) != 512 {
		t.Fatal("pending source lost canonical identities", err)
	}
	store.open = false
	resolved, err := source.ReadDecisionSourceInTx(ctx, store.tx, store.project, store.meeting.String(), store.decision.String())
	if err != nil || resolved.Open || resolved.Version != 3 {
		t.Fatal("resolved source reopened", err)
	}
	store.live, store.retained = false, true
	deleted, err := source.ReadDecisionSourceInTx(ctx, store.tx, store.project, store.meeting.String(), store.decision.String())
	if err != nil || !deleted.Relevant || deleted.Open || deleted.Version != 4 || deleted.Summary != "" || deleted.Context.MeetingID != store.meeting.String() {
		t.Fatal("late source reconstructed deleted question or action", err)
	}
	// A rebuild cannot enumerate a project using a per-item shared read.
	before := store.reads
	if _, err := source.ScanDecisionSourcesInTx(ctx, store.tx, store.project); err == nil || before != store.reads {
		t.Fatal("rebuild did not require its exclusive project gate")
	}
}
