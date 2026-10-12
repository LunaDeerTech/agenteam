//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type variableProjectAppCall struct {
	ID         string     `json:"id"`
	Project    string     `json:"project_id"`
	Process    string     `json:"process_id"`
	Instance   string     `json:"instance_id"`
	Lane       string     `json:"lane"`
	Class      string     `json:"class"`
	Phase      string     `json:"phase"`
	Registered time.Time  `json:"registered_at"`
	Closed     *time.Time `json:"closed_at"`
}

type variableProjectApp struct{ *agentProjectApp }

const variableProjectAppSecret = "VARIABLE_APP_PRIVATE_TEST_MATERIAL_8dc16"

// The existing handoff retires every setup owner and its real guard before
// public app.Run. It owns the original verified TLS bridge, signal channel,
// actual Run return and guard-release observation; no fixture serves Variables.
// Its failure retention keeps the original graph until return or the unchanged
// Go/outer hard deadline, not a successful bounded local Join.
func variableProjectAppHandoff(t *testing.T, exercise func(*variableProjectApp)) {
	t.Helper()
	agentProjectAppHandoff(t, func(base *agentProjectApp) {
		v := &variableProjectApp{agentProjectApp: base}
		if len(v.variableCalls(t)) != 0 {
			t.Fatal("App startup fabricated Variables public calls")
		}
		exercise(v)
		v.requestStop()
		select {
		case <-v.done:
		case <-v.ctx.Done():
			t.Fatal("original Variables App did not return")
		}
		agentAppRequire(t, v.runErr)
		for _, row := range v.variableCalls(t) {
			if row.Process != v.process || row.Phase != "business_closed" || row.Closed == nil {
				t.Fatal("App returned before its original Variables calls settled")
			}
		}
		v.logs.mu.Lock()
		leaked := bytes.Contains(v.logs.data.Bytes(), []byte(variableProjectAppSecret))
		v.logs.mu.Unlock()
		if leaked {
			t.Fatal("App log exposed private Secret fixture material")
		}
		// The enclosing original helper still checks Run's drained outcome,
		// canonical process retirement and actual guard flock release.
	})
}

func (v *variableProjectApp) variableCalls(t *testing.T) []variableProjectAppCall {
	t.Helper()
	var raw []byte
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.project_calls c WHERE project_id=$1`, v.setup.p2.project.ID.String()).Scan(&raw))
	var rows []variableProjectAppCall
	agentAppRequire(t, json.Unmarshal(raw, &rows))
	return rows
}

func (v *variableProjectApp) variableReturned(t *testing.T, before []variableProjectAppCall, lane, class string) variableProjectAppCall {
	t.Helper()
	after := v.variableCalls(t)
	prior := make(map[string]variableProjectAppCall, len(before))
	for _, row := range before {
		prior[row.ID] = row
	}
	var added []variableProjectAppCall
	for _, row := range after {
		if original, found := prior[row.ID]; found {
			if !reflect.DeepEqual(original, row) {
				t.Fatal("Variables HTTP rewrote another original call")
			}
		} else {
			added = append(added, row)
		}
	}
	if len(after) != len(before)+1 || len(added) != 1 {
		t.Fatal("one Variables HTTP call did not retain exactly one CallID")
	}
	row := added[0]
	if row.ID == "" || row.Project != v.setup.p2.project.ID.String() || row.Process != v.process || row.Instance == "" || row.Lane != lane || row.Class != class || row.Phase != "business_closed" || row.Registered.IsZero() || row.Closed == nil || row.Closed.Before(row.Registered) {
		t.Fatal("Variables HTTP return omitted its complete closed call identity")
	}
	for _, other := range before {
		if row.Process != other.Process || row.Instance != other.Instance {
			t.Fatal("default App split ordinary and Secret across ownership pairs")
		}
	}
	var claimed bool
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.process_claims WHERE process_id=$1 AND state IN ('claimed','stopped'))`, row.Process).Scan(&claimed))
	if !claimed {
		t.Fatal("Variables call lacks its canonical real process claim")
	}
	return row
}

// This private comparison excludes asynchronously delivered Outbox state. It
// never formats the canonical values, credential references or receipt bytes.
func (v *variableProjectApp) business(t *testing.T) string {
	t.Helper()
	var snapshot string
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT jsonb_build_object(
 'variables',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.variables x WHERE project_id=$1),
 'commands',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.commands x WHERE project_id=$1),
 'secret_commands',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.secret_commands x WHERE project_id=$1),
 'history',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.history x WHERE project_id=$1),
 'secret_history',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_projectvariable.secret_history x WHERE project_id=$1),
 'receipts',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_secret.project_variable_receipts x WHERE project_id=$1))::text`, v.setup.p2.project.ID.String()).Scan(&snapshot))
	return snapshot
}

func variableProjectAppSafe(t *testing.T, response variableHTTPResponse) {
	t.Helper()
	if bytes.Contains(response.body, []byte(variableProjectAppSecret)) {
		t.Fatal("Secret HTTP exposed private fixture material")
	}
}

func (v *variableProjectApp) createSecret(t *testing.T) (vc.SecretVariableMutation, f.IdempotencyKey) {
	t.Helper()
	target := id[i.ProjectVariable](t)
	key := f.IdempotencyKey("owned-app-secret-create")
	body := []byte(secretHTTPCreateBody(t, target, variableProjectAppSecret))
	defer clear(body)
	before := v.variableCalls(t)
	response := v.http.request(t, v.setup.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(v.setup.p2.project.ID, "/secret-variables"), body, key, false)
	variableProjectAppSafe(t, response)
	receipt := secretHTTPMutation(t, response)
	if receipt.Fields().Command != vc.SecretCreateCommand || !receipt.Fields().Changed || receipt.Fields().Variable.Fields().ID != target || receipt.Fields().Variable.Fields().ProjectID != v.setup.p2.project.ID {
		t.Fatal("actual App Secret Create lost its original committed receipt")
	}
	v.variableReturned(t, before, "secret", "mutation")
	var matched int
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT count(*)
 FROM agenteam_projectvariable.secret_commands c
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id
 JOIN agenteam_projectvariable.variables x ON (x.project_id,x.id)=(c.project_id,c.target_id)
 WHERE c.project_id=$1 AND c.target_id=$2 AND c.idempotency_key=$3
 AND c.command_name='project.secret_variable.create' AND c.receipt=$4::jsonb
 AND r.project_id=c.project_id AND r.variable_id=c.target_id AND r.command_kind=c.command_name
 AND r.user_id=c.actor_user_id AND r.credential_id=c.credential_id AND r.credential_id=x.credential_id
 AND r.effect='create' AND NOT r.deleted AND r.result_version=1
 AND c.secret_effect=r.effect AND NOT c.secret_deleted AND c.credential_version=r.result_version
 AND x.type='secret' AND x.credential_version=r.result_version AND x.version=1 AND x.deleted_at IS NULL`, v.setup.p2.project.ID.String(), target.String(), string(key), jsonBytes(t, receipt)).Scan(&matched))
	if matched != 1 {
		t.Fatal("actual App Secret command did not retain its exact D04 receipt")
	}
	return receipt, key
}

func TestVariableProjectCallApp(t *testing.T) {
	if !t.Run("default-http-shared-variable-owner", func(t *testing.T) {
		variableProjectAppHandoff(t, func(v *variableProjectApp) {
			owner := v.setup.p2.base.ownerBrowser
			project := v.setup.p2.project.ID
			collection := variableHTTPPath(project, "/variables")
			request := createInput(t, "APP_ORDINARY", "owned ordinary fixture value")
			key := f.IdempotencyKey("owned-app-variable-create")
			before := v.variableCalls(t)
			created := mutationHTTP(t, v.http.request(t, owner, http.MethodPost, collection, jsonBytes(t, map[string]any{"request": request}), key, false))
			if created.Fields().Command != vc.CreateCommand || !created.Fields().Changed || created.Fields().Variable.Fields().ID != request.Fields().ID || created.Fields().Variable.Fields().ProjectID != project {
				t.Fatal("actual App ordinary Create lost its original committed receipt")
			}
			v.variableReturned(t, before, "ordinary", "mutation")
			facts := v.business(t)
			plainPath := collection + "/" + request.Fields().ID.String()
			before = v.variableCalls(t)
			get := v.http.request(t, owner, http.MethodGet, plainPath, nil, "", false)
			if get.status != http.StatusOK || !bytes.Equal(get.body, jsonBytes(t, created.Fields().Variable)) {
				t.Fatal("actual App ordinary Get changed the original value")
			}
			v.variableReturned(t, before, "ordinary", "read")
			before = v.variableCalls(t)
			found := lookupHTTP(t, v.http.request(t, owner, http.MethodPost, collection+"/commands/lookup", jsonBytes(t, map[string]any{"command": vc.CreateCommand, "request": request}), key, false))
			if found.Status() != vc.LookupCommitted || found.Receipt() == nil || !bytes.Equal(jsonBytes(t, *found.Receipt()), jsonBytes(t, created)) {
				t.Fatal("actual App ordinary Lookup changed the original receipt")
			}
			v.variableReturned(t, before, "ordinary", "read")
			if v.business(t) != facts {
				t.Fatal("ordinary reads changed committed business facts")
			}

			secretCreated, secretKey := v.createSecret(t)
			secretTarget := secretCreated.Fields().Variable.Fields().ID
			secretCollection := variableHTTPPath(project, "/secret-variables")
			secretPath := secretCollection + "/" + secretTarget.String()
			facts = v.business(t)
			before = v.variableCalls(t)
			get = v.http.request(t, owner, http.MethodGet, secretPath, nil, "", false)
			variableProjectAppSafe(t, get)
			var safe vc.SecretVariable
			agentAppRequire(t, json.Unmarshal(get.body, &safe))
			if get.status != http.StatusOK || safe.Validate() != nil || !bytes.Equal(get.body, jsonBytes(t, secretCreated.Fields().Variable)) {
				t.Fatal("actual App Secret Get lost its safe original projection")
			}
			v.variableReturned(t, before, "secret", "read")
			before = v.variableCalls(t)
			lookup := v.http.request(t, owner, http.MethodPost, secretCollection+"/commands/lookup", jsonBytes(t, map[string]any{"command": vc.SecretCreateCommand, "target_id": secretTarget}), secretKey, false)
			variableProjectAppSafe(t, lookup)
			secretFound := secretHTTPLookup(t, lookup)
			if secretFound.Status() != vc.SecretLookupCommitted || secretFound.Receipt() == nil || !bytes.Equal(jsonBytes(t, *secretFound.Receipt()), jsonBytes(t, secretCreated)) {
				t.Fatal("actual App Secret Lookup changed the original safe receipt")
			}
			v.variableReturned(t, before, "secret", "read")
			before = v.variableCalls(t)
			for _, path := range []string{plainPath, secretPath} {
				denied := v.http.request(t, v.setup.p2.base.otherBrowser, http.MethodGet, path, nil, "", false)
				variableProjectAppSafe(t, denied)
				agentAppProblem(t, denied, http.StatusNotFound, f.NotFound)
			}
			if len(before) != 6 || !reflect.DeepEqual(before, v.variableCalls(t)) || v.business(t) != facts {
				t.Fatal("read/denied HTTP registered extra calls or changed business facts")
			}
			t.Log("variable_project_call_app public_run=true ordinary_create_get_lookup=3 secret_create_get_lookup=3 distinct_closed_calls=6 same_process_instance=true real_d04_receipt=true denied_registration=0")
		})
	}) {
		return
	}
	t.Run("secret-checkpoint-drains-before-guard", func(t *testing.T) {
		variableProjectAppHandoff(t, func(v *variableProjectApp) { testVariableProjectAppSecretTail(t, v) })
	})
}

func testVariableProjectAppSecretTail(t *testing.T, v *variableProjectApp) {
	t.Helper()
	receipt, _ := v.createSecret(t)
	target := receipt.Fields().Variable.Fields().ID
	prior, facts := v.variableCalls(t), v.business(t)
	conn, err := v.setup.p2.base.db.Fixture.Connect(v.ctx, v.setup.p2.base.db.Name)
	agentAppRequire(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		agentAppRequire(t, conn.Close(ctx))
	}()
	tx, err := conn.Begin(v.ctx)
	agentAppRequire(t, err)
	var releaseOnce sync.Once
	var releaseErr error
	release := func() {
		releaseOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			releaseErr = tx.Rollback(ctx)
		})
	}
	defer func() { release(); agentAppRequire(t, releaseErr) }()
	key, err := f.ProjectLock(v.setup.p2.project.ID.String())
	agentAppRequire(t, err)
	_, err = tx.Exec(v.ctx, `SELECT pg_advisory_xact_lock_shared($1)`, key.AdvisoryKey())
	agentAppRequire(t, err)
	holder := int32(conn.PgConn().PID())
	requestCtx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	done := make(chan struct{})
	var response variableHTTPResponse
	var responseErr error
	go func() {
		defer close(done)
		response, responseErr = agentProjectAppGet(requestCtx, v.http, v.setup.p2.base.ownerBrowser, variableHTTPPath(v.setup.p2.project.ID, "/secret-variables/")+target.String())
	}()
	defer func() {
		release()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("original Secret HTTP did not return; retaining its dependencies")
			<-done // The inherited fixture must not destroy an active borrower's DB.
		}
	}()
	ready, finishReady := context.WithTimeout(v.ctx, time.Second)
	first, err := v.waiter(ready, key, holder, nil)
	finishReady()
	agentAppRequire(t, err)
	running := v.variableCalls(t)
	if len(prior) != 1 || len(running) != 2 {
		t.Fatal("real Secret Get did not register exactly its original call")
	}
	var original variableProjectAppCall
	for _, row := range running {
		if row.ID != prior[0].ID {
			original = row
		}
	}
	if original.ID == "" || original.Phase != "running" || original.Closed != nil || original.Lane != "secret" || original.Class != "read" || original.Project != prior[0].Project || original.Process != v.process || original.Instance != prior[0].Instance || original.Registered.IsZero() {
		t.Fatal("blocked Secret checkpoint lacked its original shared-pair owner")
	}
	select {
	case <-done:
		t.Fatal("Secret HTTP returned before the original checkpoint statement")
	default:
	}
	v.requireGuardHeld(t)
	// Same exact Project tuple, owned blocker, backend and xact_start as the
	// proven waiter. Cancel only that one statement, within the original 2s HTTP
	// read budget; its real rollback is NotCommitted, never COMMIT Unknown.
	v.cancelCheckpoint(t, key, holder, first)
	select {
	case <-done:
	case <-requestCtx.Done():
		t.Fatal("original Secret HTTP did not actually return")
	}
	agentAppRequire(t, responseErr)
	variableProjectAppSafe(t, response)
	if response.status != http.StatusOK || !bytes.Equal(response.body, jsonBytes(t, receipt.Fields().Variable)) || !reflect.DeepEqual(v.variableCalls(t), running) || v.business(t) != facts {
		t.Fatal("checkpoint failure rewrote known Secret HTTP result or business")
	}
	var firstActive bool
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND xact_start=$2)`, first.PID, first.Started).Scan(&firstActive))
	if firstActive {
		t.Fatal("Secret HTTP returned before its original checkpoint transaction ended")
	}
	v.requestStop()
	drainReady, finishDrainReady := context.WithTimeout(v.ctx, 3*time.Second)
	second, err := v.waiter(drainReady, key, holder, &first)
	finishDrainReady()
	agentAppRequire(t, err)
	select {
	case <-v.done:
		t.Fatal("App returned while the original Secret checkpoint borrowed providers")
	default:
	}
	v.requireGuardHeld(t)
	if !reflect.DeepEqual(v.variableCalls(t), running) {
		t.Fatal("App Drain replaced the original pending Secret CallID")
	}
	var live bool
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT state='claimed' AND stopped_at IS NULL FROM agenteam_object.process_claims WHERE process_id=$1`, v.process).Scan(&live))
	if !live {
		t.Fatal("App retired its guard fact before the Secret checkpoint returned")
	}
	// Release in the original 10s graceful budget. This is not Force validation
	// and does not change root Store.ForceClose or D04 maintenance ownership.
	release()
	agentAppRequire(t, releaseErr)
	select {
	case <-v.done:
	case <-v.ctx.Done():
		t.Fatal("App did not return after the original holder Rollback")
	}
	agentAppRequire(t, v.runErr)
	settled := v.variableReturned(t, prior, "secret", "read")
	if settled.ID != original.ID || settled.Registered != original.Registered || settled.Process != original.Process || settled.Instance != original.Instance || v.business(t) != facts {
		t.Fatal("App Drain recreated the Secret call or changed committed business")
	}
	t.Logf("variable_project_call_app original_secret_http_returned=true checkpoint_not_committed=true same_call_closed=true guard_held_until_checkpoint=true holder_backend=%d original_checkpoint_backend=%d drain_checkpoint_backend=%d", holder, first.PID, second.PID)
}
