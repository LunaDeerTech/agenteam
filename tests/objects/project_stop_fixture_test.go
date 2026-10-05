//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// This authority owns only fixture lifecycle facts. It verifies current cause,
// phase, component manifest and actual held locks in the consumer's exact Tx.
// Production Project binding belongs to the separately integrated R3 adapter.
type objectStopAuthority struct {
	store     *postgres.Store
	mu        sync.Mutex
	validated map[foundation.Tx]oc.ProjectStopRequest
}

func newObjectStopAuthority(t *testing.T, f *fixture, store *postgres.Store) *objectStopAuthority {
	t.Helper()
	f.sql(t, `CREATE TABLE object_fixture.stop_causes(project_id uuid PRIMARY KEY,operation_id uuid NOT NULL,action text NOT NULL,accepted_version bigint NOT NULL,phase text NOT NULL,manifest boolean NOT NULL DEFAULT true)`)
	return &objectStopAuthority{store: store, validated: map[foundation.Tx]oc.ProjectStopRequest{}}
}
func (a *objectStopAuthority) dependencies(r oc.ProjectStopRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, fault(foundation.InvalidArgument)
	}
	b, err := oc.ProjectStopBinding(r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	key, _ := foundation.ProjectLock(r.Details().Cause.Details().ProjectID.String())
	record, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-project-stop:"+r.Details().Cause.Details().ProjectID.String())
	return oc.NewAccessDependencies(b, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}, {Key: record, Mode: foundation.Exclusive}})
}
func (a *objectStopAuthority) DiscoverProjectStop(_ context.Context, r oc.ProjectStopRequest) (oc.AccessDependencies, error) {
	return a.dependencies(r)
}
func (a *objectStopAuthority) ValidateProjectStopInTx(ctx context.Context, tx foundation.Tx, r oc.ProjectStopRequest, expected oc.AccessDependencies) (oc.ProjectStopAuthorization, error) {
	actual, err := a.dependencies(r)
	if err != nil {
		return oc.ProjectStopAuthorization{}, err
	}
	if !actual.Equal(expected) || r.Details().Component != oc.ObjectStopComponent {
		return oc.ProjectStopAuthorization{}, fault(foundation.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, actual.Locks()); err != nil {
		return oc.ProjectStopAuthorization{}, err
	}
	e, err := a.store.InTx(tx)
	if err != nil {
		return oc.ProjectStopAuthorization{}, err
	}
	d := r.Details().Cause.Details()
	var op, action, phase, state string
	var version, current int64
	var manifest bool
	err = e.QueryRow(ctx, `SELECT c.operation_id::text,c.action,c.accepted_version,c.phase,c.manifest,p.state,p.version FROM object_fixture.stop_causes c JOIN object_fixture.projects p ON p.id=c.project_id WHERE c.project_id=$1`, d.ProjectID.String()).Scan(&op, &action, &version, &phase, &manifest, &state, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.ProjectStopAuthorization{}, fault(foundation.Forbidden)
	}
	if err != nil {
		return oc.ProjectStopAuthorization{}, err
	}
	if op != d.OperationID.String() || action != string(d.Action) || version != int64(d.ProjectVersion) || current != version || !manifest || state != "archived" && state != "deleting" || action == "archive" && state != "archived" || action == "delete" && state != "deleting" {
		return oc.ProjectStopAuthorization{}, fault(foundation.Forbidden)
	}
	mode := oc.ContinueProjectStop
	if phase == "complete" && r.Details().Step == oc.InspectProjectStopStep {
		mode = oc.ReadProjectStop
	} else if phase != "stopping" {
		return oc.ProjectStopAuthorization{}, fault(foundation.Forbidden)
	}
	a.mu.Lock()
	a.validated[tx] = r
	a.mu.Unlock()
	return oc.NewProjectStopAuthorization(tx, r, expected, mode)
}

func activateObjectStop(t *testing.T, f *fixture, action oc.ProjectStopAction) (identity.Actor, oc.ProjectStopCause) {
	t.Helper()
	operation := id[oc.ProjectStopOperation](t)
	state := "archived"
	if action == oc.ProjectStopDelete {
		state = "deleting"
	}
	var version int64
	if err := f.store.QueryRow(contextFor(t), `UPDATE object_fixture.projects SET state=$2,operation_id=$3,version=version+1 WHERE id=$1 RETURNING version`, f.project.String(), state, operation.String()).Scan(&version); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO object_fixture.stop_causes VALUES($1,$2,$3,$4,'stopping',true) ON CONFLICT(project_id) DO UPDATE SET operation_id=EXCLUDED.operation_id,action=EXCLUDED.action,accepted_version=EXCLUDED.accepted_version,phase='stopping',manifest=true`, f.project.String(), operation.String(), string(action), version)
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: f.project, OperationID: operation, Action: action, ProjectVersion: foundation.Version(version)})
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(f.project)
	actor, _ := registration.Actor(operation.String(), scope)
	return actor, cause
}

type stopServiceOptions struct {
	store     *postgres.Store
	wrapper   object.Store
	endpoint  string
	planner   oc.AccessPlanner
	processes oc.ProcessAuthority
	process   oc.ProcessID
	spoolPath string
}

func newObjectStopService(t *testing.T, f *fixture, a *objectStopAuthority, o stopServiceOptions) *object.Service {
	t.Helper()
	store := o.store
	if store == nil {
		store = f.store
	}
	port := o.wrapper
	if port == nil {
		port = store
	}
	endpoint := o.endpoint
	if endpoint == "" {
		endpoint = f.remote.Endpoint()
	}
	values := map[string]string{"ENDPOINT": endpoint, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	if strings.HasPrefix(endpoint, "https:") {
		values["TLS_MODE"] = "verify-full"
		values["CA_FILE"] = f.remote.CAFile
	}
	config, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := object.NewBackend(config)
	if err != nil {
		t.Fatal(err)
	}
	process := o.process
	if process.Validate() != nil {
		process = id[oc.Process](t)
	}
	path := o.spoolPath
	if path == "" {
		path = filepath.Join(t.TempDir(), "spool")
	}
	spool, err := object.OpenSpool(path, process)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	base := &authority{store: store}
	auditing, err := audit.New(store, keys, audit.Authorizations{Projects: auditAuthority{base}})
	if err != nil {
		t.Fatal(err)
	}
	planner := o.planner
	if planner == nil {
		planner = base
	}
	service, err := object.New(port, backend, spool, auditing, object.Authorizations{Planner: planner, Resources: base, Read: base, Gate: base, Cleanup: base, Leases: base, Processes: o.processes, ProjectStop: a})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return service
}

func stopUntilSettled(t *testing.T, s *object.Service, actor identity.Actor, cause oc.ProjectStopCause) oc.ObjectStopReport {
	t.Helper()
	var report oc.ObjectStopReport
	ctx, cancel := context.WithTimeout(contextFor(t), 3*time.Second)
	defer cancel()
	for i := 0; i < 40; i++ {
		var err error
		report, err = s.RequestProjectStop(ctx, actor, cause)
		if err != nil {
			if codeOf(err) != foundation.ResourceBusy {
				t.Fatal(err)
			}
		} else if report.Details().State == oc.ProjectStopped {
			return report
		}
		select {
		case <-ctx.Done():
			t.Fatal("stop did not join", report.Details())
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("stop did not visit all native lanes", report.Details())
	return report
}
func codeOf(err error) foundation.Code {
	var f *foundation.Fault
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}
func projectWorkCount(t *testing.T, f *fixture, kind string, joined bool) int {
	t.Helper()
	var n int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1 AND kind=$2 AND (joined_at IS NOT NULL)=$3`, f.project.String(), kind, joined).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func assertStopReaderLifetime(t *testing.T, f *fixture, object oc.ObjectID, process oc.ProcessID, active bool) {
	t.Helper()
	var count int
	var matches bool
	err := f.store.QueryRow(contextFor(t), `SELECT count(*),coalesce(bool_and(l.process_id=w.process_id AND w.process_id=$2 AND l.object_id=w.object_id AND l.state=CASE WHEN $3 THEN 'active' ELSE 'released' END AND (w.joined_at IS NULL)=$3),false) FROM agenteam_object.project_work w JOIN agenteam_object.object_leases l ON l.id=w.resource_id WHERE w.object_id=$1 AND w.kind='reader'`, object.String(), process.String(), active).Scan(&count, &matches)
	if err != nil || count != 1 || !matches {
		t.Fatal("reader native/work/process precondition", active, count, matches, err)
	}
	t.Logf("actual reader lifetime: active=%t, native lease and work share exact process %s", active, process)
}

func assertStopBodyHeld(t *testing.T, p *storageProxy) {
	t.Helper()
	select {
	case <-p.began:
	case <-time.After(3 * time.Second):
		t.Fatal("actual GET body did not enter the controlled hold")
	}
	select {
	case <-p.finished:
		t.Fatal("GET body already returned/closed before stop")
	default:
	}
}

func projectStopSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var raw []byte
	if err := f.store.QueryRow(contextFor(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.project_id,p.operation_id) FROM agenteam_object.project_stops p),(SELECT jsonb_agg(to_jsonb(w) ORDER BY w.id) FROM agenteam_object.project_work w))`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
