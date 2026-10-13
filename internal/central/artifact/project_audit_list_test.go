package artifact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// postgres.Rows is deliberately not forged here. The valid private witness
// reaches the original bounded page SQL and preserves its controlled error;
// the App PG test owns successful Rows/Close/page and actual Audit persistence.
type listAuditStore struct {
	*projectAuditStore
	query   func(context.Context, string, ...any) (*postgres.Rows, error)
	queries int
}

func (s *listAuditStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *listAuditStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.queries++
	return s.query(ctx, query, args...)
}

func newListAuditFixture(t *testing.T) (*projectAuditFixture, *listAuditStore, projectAuditList, error) {
	t.Helper()
	v := newProjectAuditFixture(t, "content")
	s := &listAuditStore{projectAuditStore: v.store}
	original := errors.New("original page query failure")
	filter := ac.ListFilter{NameQuery: "a%_\\"}
	query, err := artifactListQuery(v.meta.Details().Reference.ProjectID, filter)
	if err != nil {
		t.Fatal(err)
	}
	list := projectAuditList{invocation: v.request.invocation, filter: filter, page: f.PageRequest{Limit: 2}, query: query, rows: []ac.Metadata{v.meta}}
	s.query = func(_ context.Context, q string, args ...any) (*postgres.Rows, error) {
		if !strings.Contains(q, "FROM agenteam_artifact.artifacts WHERE project_id=$1") || !strings.Contains(q, "ORDER BY created_at DESC,id DESC LIMIT $8") ||
			len(args) != 8 || args[0] != v.meta.Details().Reference.ProjectID.String() || args[4] != escapeLike(filter.NameQuery) || args[6] != "" || args[7] != 3 {
			return nil, errors.New("list lost its exact bounded query/filter/position")
		}
		return nil, original
	}
	v.authority, err = NewProjectAuditAuthority(s)
	if err != nil {
		t.Fatal(err)
	}
	v.service.state().store = s
	v.service.state().audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
		v.saved, v.entry, v.key = ctx, e, k
		return au.AppendReceipt{}, v.authority.CheckProjectAuditInTx(ctx, tx, e, k)
	})
	return v, s, list, original
}

func TestArtifactProjectListAuditExactWitness(t *testing.T) {
	for _, mode := range []string{"exact-query", "query-filter", "page-limit", "page-cursor", "count", "attempt", "wrong-store", "wrong-tx", "bare-context"} {
		t.Run(mode, func(t *testing.T) {
			v, s, list, original := newListAuditFixture(t)
			v.service.state().audit = projectAuditAppender(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) (au.AppendReceipt, error) {
				v.saved, v.entry, v.key = ctx, e, k
				w := ctx.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
				switch mode {
				case "query-filter":
					w.list.filter.NameQuery = "different"
				case "page-limit":
					w.list.page.Limit++
				case "page-cursor":
					w.list.page.Cursor = "different"
				case "count":
					w.list.rows = nil
				case "attempt":
					d := w.list.invocation.Details()
					d.AttemptID = newTestID[ac.CallAttempt](t)
					var err error
					w.list.invocation, err = ac.NewInvocation(d)
					if err != nil {
						t.Fatal(err)
					}
				case "wrong-store":
					w.store = v.store
				case "wrong-tx":
					tx = f.NewTx()
				case "bare-context":
					ctx = context.Background()
				}
				return au.AppendReceipt{}, v.authority.CheckProjectAuditInTx(ctx, tx, e, k)
			})
			err := v.service.appendProjectListAudit(context.Background(), s.tx, list)
			if mode == "exact-query" {
				if !errors.Is(err, original) || s.queries != 1 {
					t.Fatal("valid private query lost the original SQL error", err, s.queries)
				}
			} else {
				projectAuditCode(t, err, f.Forbidden)
				if s.queries != 0 {
					t.Fatal("changed private binding reached page SQL", s.queries)
				}
			}
			before := s.queries
			projectAuditCode(t, v.authority.CheckProjectAuditInTx(v.saved, s.tx, v.entry, v.key), f.Forbidden)
			if s.queries != before {
				t.Fatal("list proof outlived its Append callback")
			}
		})
	}
}

func TestArtifactProjectListAuditOriginalReturn(t *testing.T) {
	v, s, list, original := newListAuditFixture(t)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	query := s.query
	s.query = func(ctx context.Context, q string, args ...any) (*postgres.Rows, error) {
		close(entered)
		<-release
		return query(ctx, q, args...)
	}
	go func() { returned <- v.service.appendProjectListAudit(context.Background(), s.tx, list) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		<-returned
		t.Fatal("original list query never entered")
	}
	w := v.saved.Value(projectAuditWitnessKey{}).(*projectAuditWitness)
	if !w.active.Load() {
		t.Error("proof expired before the original query returned")
	}
	var early error
	var returnedEarly bool
	select {
	case early = <-returned:
		returnedEarly = true
		t.Error("Append returned while its actual page query was held")
	default:
	}
	close(release)
	if !returnedEarly {
		early = <-returned
	}
	if err := early; !errors.Is(err, original) || w.active.Load() {
		t.Fatal("actual return lost the first SQL error or retained the proof", err)
	}
}
