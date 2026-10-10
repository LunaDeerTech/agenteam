package skill

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type skillRowValues struct {
	values []any
	err    error
}

func (r skillRowValues) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dst) != len(r.values) {
		return errors.New("column count")
	}
	for n, v := range r.values {
		reflect.ValueOf(dst[n]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}
func stateValues(t *testing.T) []any {
	t.Helper()
	r := stateRow(t)
	manifest, e := json.Marshal(r.bundle.manifest)
	if e != nil {
		t.Fatal(e)
	}
	return []any{r.request.ProjectID.String(), r.request.CreationID.String(), string(r.request.InitializationKey), r.skill.String(), r.revision.String(), string(r.semantic), r.bundle.id, int64(r.bundle.revision), string(r.bundle.packageDigest), string(r.bundle.manifestDigest), int64(r.bundle.size), manifest, r.bundle.name, r.bundle.description, string(r.phase), int64(r.version), "", "", "", "", r.created.Time(), r.updated.Time()}
}
func TestInitializationScannerRejectsDamageWithoutInventingAbsence(t *testing.T) {
	row, e := scanInitialization(skillRowValues{values: stateValues(t)})
	if e != nil || row == nil {
		t.Fatal(e)
	}
	for name, edit := range map[string]func([]any){
		"description":           func(v []any) { v[13] = "different frozen text" },
		"project":               func(v []any) { v[0] = "not-an-id" },
		"semantic":              func(v []any) { v[5] = "sha256:bad" },
		"partial_binding":       func(v []any) { v[16] = stateID[struct{}](20).String() },
		"premature_publication": func(v []any) { v[14] = "published" },
		"manifest":              func(v []any) { v[11] = []byte(`{"format":"skill-zip-v1"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			v := stateValues(t)
			edit(v)
			got, e := scanInitialization(skillRowValues{values: v})
			if e == nil || got != nil {
				t.Fatal("damage became a row or not-found")
			}
		})
	}
	if v, e := scanInitialization(skillRowValues{err: pgx.ErrNoRows}); v != nil || e != nil {
		t.Fatal("missing row")
	}
	if v, e := scanInitialization(skillRowValues{err: errors.New("query interrupted")}); v != nil || e == nil {
		t.Fatal("query failure lost")
	}
}

type initializationGateStore struct {
	Store
	calls            []string
	tx               f.Tx
	heldErr, liveErr error
	values           skillRowValues
}

func (s *initializationGateStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.calls = append(s.calls, "held")
	if tx != s.tx || len(locks) != 2 || locks[0].Mode != f.Exclusive || locks[1].Mode != f.Exclusive {
		return errors.New("wrong lock proof")
	}
	return s.heldErr
}
func (s *initializationGateStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.calls = append(s.calls, "live")
	if tx != s.tx {
		return nil, errors.New("foreign tx")
	}
	if s.liveErr != nil {
		return nil, s.liveErr
	}
	return s, nil
}
func (s *initializationGateStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.calls = append(s.calls, "query")
	return s.values
}

type initializationGateProjects struct {
	ProjectPorts
	store       *initializationGateStore
	err         error
	convergence bool
	tx          f.Tx
	actor       id.Actor
	request     pc.InitializationRequest
}

func (p *initializationGateProjects) ValidateInitializationInTx(_ context.Context, tx f.Tx, a id.Actor, c pc.CreationID, project pc.ProjectID, key f.IdempotencyKey) error {
	p.store.calls = append(p.store.calls, "project")
	p.tx = tx
	p.actor = a
	p.request = pc.InitializationRequest{CreationID: c, ProjectID: project, InitializationKey: key}
	return p.err
}
func (p *initializationGateProjects) ValidateInitializationConvergenceInTx(ctx context.Context, tx f.Tx, a id.Actor, r pc.InitializationRequest) error {
	p.convergence = true
	return p.ValidateInitializationInTx(ctx, tx, a, r.CreationID, r.ProjectID, r.InitializationKey)
}
func TestInitializationAuthorityUsesOriginalLiveTxAndGateBeforeFacts(t *testing.T) {
	r := stateRow(t)
	role, _ := id.RegisterService(id.ProjectInitialization)
	scope, _ := id.InProject(r.request.ProjectID)
	actor, _ := role.Actor(r.request.CreationID.String(), scope)
	for _, mode := range []string{"publish_gate", "convergence_gate", "missing_lock", "ended_tx", "denied_project"} {
		t.Run(mode, func(t *testing.T) {
			store := &initializationGateStore{tx: f.NewTx(), values: skillRowValues{values: stateValues(t)}}
			projects := &initializationGateProjects{store: store}
			a, e := NewAuthority(store, projects)
			if e != nil {
				t.Fatal(e)
			}
			sentinel := f.NewFault(f.Forbidden, f.NotStarted)
			want := []string{"held", "live", "project", "query"}
			switch mode {
			case "missing_lock":
				store.heldErr = sentinel
				want = want[:1]
			case "ended_tx":
				store.liveErr = sentinel
				want = want[:2]
			case "denied_project":
				projects.err = sentinel
				want = want[:3]
			}
			_, row, e := a.initializationInTx(context.Background(), store.tx, actor, r.request, mode == "convergence_gate")
			if !reflect.DeepEqual(store.calls, want) {
				t.Fatal("authority order", store.calls)
			}
			if len(want) < 4 {
				if e != sentinel || row != nil {
					t.Fatal("error replaced or facts leaked")
				}
			} else if e != nil || row == nil || projects.tx != store.tx || !projects.actor.Equal(actor) || projects.request != r.request || projects.convergence != (mode == "convergence_gate") {
				t.Fatal("original port input changed", e)
			}
		})
	}
	var missing *initializationGateStore
	if a, e := NewAuthority(missing, &initializationGateProjects{}); e == nil || a != nil {
		t.Fatal("typed nil Store")
	}
	if a, e := NewAuthority(&initializationGateStore{}, (*initializationGateProjects)(nil)); e == nil || a != nil {
		t.Fatal("typed nil project port")
	}
}
