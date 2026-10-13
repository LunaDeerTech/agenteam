//go:build integration

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// The only Execution seed is the original installed Coordinator/Handoff.
// The manager's actual initial empty-directory return stays owned until Stop;
// no worker, model, runner or SQL seed manufactures this historical record.
func TestTaskExecutionReadDefaultApp(t *testing.T) {
	t.Run("enabled-owner-and-disabled-history", taskExecutionReadApp)
}

func taskExecutionReadFixture(t *testing.T, origin string, configure func(*dependencies, *executionSchedulerApp)) (*executionSchedulerApp, []string, wc.Task) {
	t.Helper()
	var deployment []string
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		deployment = append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s")
		v.root.cfg = guardConfiguration(t, append(append([]string(nil), deployment...), "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+executionSchedulerAppConfig,
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s"))
		if configure != nil { configure(d, v) }
	})
	v.seed(t)
	v.originalFacts(t, 1)
	v.assembly.mu.Lock()
	readOnly := v.assembly.executionReads
	actual := v.assembly.executions
	v.assembly.mu.Unlock()
	if readOnly != nil || actual != v.runtime || actual.executionCalls.TaskReader() == nil {
		t.Fatal("enabled history does not use the original Execution pair")
	}
	taskID, err := f.ParseID[wc.Task](v.execution.Trigger.TaskID)
	workAppCheck(t, "execution-read-task-id", err)
	task, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, taskID)
	workAppCheck(t, "execution-read-formal-task", err)
	return v, deployment, task
}

func taskExecutionReadPath(project id.ProjectID, task string, execution id.ExecutionID) string {
	path := "/api/v1/projects/"+project.String()+"/tasks/"+task+"/executions"
	if execution.Validate() == nil { path += "/"+execution.String() }
	return path
}

func taskExecutionExpected(v *executionSchedulerApp) ec.TaskExecution {
	s := v.execution
	return ec.TaskExecution{ID:s.ID, ProjectID:s.ProjectID, TaskID:s.Trigger.TaskID, AgentID:s.AgentID, Purpose:s.Purpose, Status:ec.Created, Version:1, CreatedAt:s.CreatedAt}
}

func taskExecutionHTTP(v *workAppFixture, client *http.Client, cookie, method, path string) (artifactOwnerHTTPResponse, error) {
	req, err := http.NewRequestWithContext(v.ctx, method, v.address+path, nil)
	if err != nil { return artifactOwnerHTTPResponse{}, err }
	origin, err := url.Parse(v.root.cfg.PublicOrigin())
	if err != nil { return artifactOwnerHTTPResponse{}, err }
	req.Host = origin.Host
	req.Header.Set("Origin", origin.String())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != "" { req.AddCookie(&http.Cookie{Name:"agenteam_local_session",Value:cookie}) }
	response, err := client.Do(req)
	if err != nil { return artifactOwnerHTTPResponse{}, err }
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw)>1<<20 { return artifactOwnerHTTPResponse{}, errors.New("original Execution response did not return") }
	if len(response.Header.Values("X-Request-ID")) != 1 { return artifactOwnerHTTPResponse{}, errors.New("original Execution RID missing") }
	if _, err = f.ParseID[f.Request](response.Header.Get("X-Request-ID")); err != nil { return artifactOwnerHTTPResponse{}, errors.New("original Execution RID invalid") }
	if response.Header.Get("Cache-Control") != "no-store" { return artifactOwnerHTTPResponse{}, errors.New("Execution response cache boundary") }
	if method != http.MethodHead && response.ContentLength != int64(len(raw)) { return artifactOwnerHTTPResponse{}, errors.New("Execution response length differs") }
	return artifactOwnerHTTPResponse{status:response.StatusCode,header:response.Header.Clone(),body:raw}, nil
}

func taskExecutionGet(t *testing.T, v *workAppFixture, client *http.Client, cookie, method, path string, status int) artifactOwnerHTTPResponse {
	t.Helper()
	r, err := taskExecutionHTTP(v,client,cookie,method,path)
	workAppCheck(t,"execution-read-http",err)
	if r.status != status { t.Fatalf("Execution read status=%d wanted=%d",r.status,status) }
	return r
}

func taskExecutionPage(t *testing.T, raw []byte, expected []ec.TaskExecution) {
	t.Helper()
	var page ec.TaskExecutionPage
	artifactOwnerHTTPDecode(t,raw,&page)
	if page.Items == nil || page.NextCursor != nil || !reflect.DeepEqual(page.Items,expected) { t.Fatal("actual Execution page differs from original canonical seed") }
	var wire map[string]json.RawMessage
	workAppCheck(t,"execution-page-wire",json.Unmarshal(raw,&wire))
	if len(wire)!=2 || string(wire["next_cursor"])!="null" { t.Fatal("Execution final cursor not explicit null") }
	var rows []json.RawMessage
	workAppCheck(t,"execution-page-items",json.Unmarshal(wire["items"],&rows))
	for _, row := range rows { taskExecutionWire(t,row) }
}

func taskExecutionWire(t *testing.T, raw []byte) {
	t.Helper()
	var fields map[string]json.RawMessage
	workAppCheck(t,"execution-safe-wire",json.Unmarshal(raw,&fields))
	keys := []string{"id","project_id","task_id","agent_id","purpose","status","version","created_at","started_at","completed_at","cancel_requested_at","snapshot_id","reason"}
	if len(fields)!=len(keys) { t.Fatal("Execution DTO has private or missing fields") }
	for _, key := range keys { if _, ok:=fields[key]; !ok { t.Fatal("Execution DTO missing required field") } }
	if string(fields["version"])!=`"1"` { t.Fatal("Execution version lost canonical decimal string") }
	for _, key:=range []string{"started_at","completed_at","cancel_requested_at","snapshot_id","reason"} { if string(fields[key])!="null" { t.Fatal("Created Execution invented runtime/terminal facts") } }
}

func taskExecutionDetail(t *testing.T, raw []byte, expected ec.TaskExecution) {
	t.Helper()
	var out struct { Execution ec.TaskExecution `json:"execution"` }
	artifactOwnerHTTPDecode(t,raw,&out)
	if !reflect.DeepEqual(out.Execution,expected) { t.Fatal("Execution detail differs from original canonical seed") }
	var wire map[string]json.RawMessage
	workAppCheck(t,"execution-detail-wire",json.Unmarshal(raw,&wire))
	if len(wire)!=1 { t.Fatal("Execution detail envelope differs") }
	taskExecutionWire(t,wire["execution"])
}

// Independent read-only snapshots exclude technical project_calls but include
// canonical Execution/Work/Audit facts. No read can create a business result.
func taskExecutionFacts(t *testing.T, v *workAppFixture) string {
	t.Helper()
	var raw []byte
	workAppCheck(t,"execution-original-business-facts",v.admin.QueryRow(v.ctx,`SELECT jsonb_build_object(
 'execution',COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_execution.executions e),'[]'::jsonb),
 'task',COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t),'[]'::jsonb),
 'audit',COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a),'[]'::jsonb),
 'snapshots',(SELECT count(*) FROM agenteam_execution.snapshots),
 'preparations',(SELECT count(*) FROM agenteam_execution.preparation_attempts),
 'models',(SELECT count(*) FROM agenteam_model.invocations),
 'operations',(SELECT count(*) FROM agenteam_tool.operations))`).Scan(&raw))
	return string(raw)
}

func taskExecutionReadClosed(t *testing.T, v *workAppFixture, process string) {
	t.Helper()
	var total, live, origins int
	workAppCheck(t,"execution-read-original-call-close",v.admin.QueryRow(v.ctx,`SELECT count(*),count(*) FILTER(WHERE phase<>'business_closed'),count(*) FILTER(WHERE original_result IS NOT NULL)
 FROM agenteam_execution.project_calls WHERE process_id=$1::uuid AND invocation_identity LIKE '%task_execution%'`,process).Scan(&total,&live,&origins))
	if total==0 || live!=0 || origins!=0 { t.Fatal("Execution read lost original close or invented created origin") }
}

// The enabled fixture's original joined() asserts exactly its Launch history.
// New history reads add real ownership rows; keep every original resource-tail
// check here without calling that old one-Launch count assertion.
func taskExecutionJoinEnabled(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	usageAppWait(t,v.directory.returned,"execution-read-manager-original-return")
	usageAppWait(t,v.root.done,"execution-read-enabled-root-return")
	workAppCheck(t,"execution-read-enabled-root",v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() { t.Fatal("enabled history App lost original owners") }
	taskExecutionGuardReleased(t,v.workAppFixture)
}

func taskExecutionGuardReleased(t *testing.T, v *workAppFixture) {
	t.Helper()
	workAppCheck(t,"execution-read-original-guard-release",syscall.Flock(int(v.claim.Fd()),syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t,"execution-read-guard-observer-unlock",syscall.Flock(int(v.claim.Fd()),syscall.LOCK_UN))
	var stopped, gone bool
	workAppCheck(t,"execution-read-original-exit-facts",v.admin.QueryRow(v.ctx,`SELECT (SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`,v.objects.process.String()).Scan(&stopped,&gone))
	if !stopped || !gone { t.Fatal("original Execution App Guard or database did not retire") }
}

// Restart the actual default App against the SAME database and deployment
// keys, after the original enabled App/manager/Guard/Store have all returned.
func taskExecutionDisabled(t *testing.T, previous *executionSchedulerApp, env []string) *workAppFixture {
	t.Helper()
	r:=&modelRootApp{db:previous.root.db,cfg:guardConfiguration(t,env),store:&modelRootStore{},logs:newEventLog(),signals:make(chan os.Signal,4),done:make(chan struct{})}
	ctx,cancel:=context.WithCancel(context.Background()); r.cancel=cancel
	v:=&workAppFixture{root:r,ctx:previous.ctx,admin:previous.admin,actor:previous.actor,cookie:previous.cookie,csrf:previous.csrf,a:previous.a,b:previous.b,client:&http.Client{Transport:&http.Transport{Proxy:nil},Timeout:8*time.Second}}
	d:=dependencies{
		open:func(ctx context.Context,cfg postgres.Config)(database,error){ raw,err:=postgres.Open(ctx,cfg);r.store.Store=raw;return r.store,err },
		bind:func(ctx context.Context,cfg config.Config,db database,owned *resources,d *dependencies)error{r.owned,r.store.owned=owned,owned;return bindAccounts(ctx,cfg,db,owned,d)},
		observeAccount:func(core *account.Service){r.core=core},
		listen:func(ctx context.Context,network,address string)(net.Listener,error){return (&net.ListenConfig{}).Listen(ctx,network,address)},
	}
	logger,err:=logging.New(logging.Central,slog.LevelInfo,r.logs);workAppCheck(t,"execution-history-logger",err)
	t.Cleanup(func(){v.requestStop();cancel();<-r.done;v.client.CloseIdleConnections();if v.claim!=nil{_ = v.claim.Close()}})
	go func(){r.err=run(ctx,r.cfg,logger,r.signals,d);close(r.done)}()
	v.address=workRootStartupAddress(t,r.logs,r.done,func()error{return r.err})
	v.assembly=r.owned.accounts().(*accountAssembly)
	v.assembly.mu.Lock()
	drivers,reads:=v.assembly.executions,v.assembly.executionReads
	v.planning,_=v.assembly.planning.(*workPlanningAssembly)
	v.projects,_=v.assembly.projects.(*projectCommandWork)
	v.assembly.mu.Unlock()
	v.objects,_=r.owned.objects().(*objectAssembly)
	if _,enabled:=r.cfg.ExecutionRuntime();enabled || drivers!=nil || reads==nil || reads.pair==nil || reads.pair.TaskReader()==nil || v.objects==nil || v.objects.process==previous.objects.process { t.Fatal("disabled history did not install only its own fresh read owner") }
	v.claim,err=os.OpenFile(filepath.Join(r.cfg.Objects().SpoolDirectory()+".processes",v.objects.process.String()+".claim"),os.O_RDWR,0)
	workAppCheck(t,"execution-history-guard-observer",err);v.requireGuard(t)
	return v
}

func taskExecutionReadApp(t *testing.T) {
	v,env,task:=taskExecutionReadFixture(t,"http://localhost:8080",nil)
	expected:=taskExecutionExpected(v)
	list:=taskExecutionReadPath(v.a,task.ID.String(),id.ExecutionID{})
	detail:=taskExecutionReadPath(v.a,task.ID.String(),v.execution.ID)
	other:=wc.TaskCreate{TaskID:guardID[wc.Task](t),SprintID:task.SprintID,Title:"Task without execution",Type:wc.TaskTypeTask,Priority:wc.TaskPriorityMedium,Plan:"Historical scope isolation"}
	var receipt wc.TaskMutation
	v.httpSeed(t,"/tasks",map[string]any{"request":other},&receipt)
	if receipt.Validate()!=nil || receipt.Task.ID!=other.TaskID {t.Fatal("formal second Task receipt differs")}
	foreign:=artifactOwnerHTTPForeignClient(t,&humanArtifactApp{workAppFixture:v.workAppFixture})
	before:=taskExecutionFacts(t,v.workAppFixture)
	page:=taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"GET",list+"?limit=1",200)
	taskExecutionPage(t,page.body,[]ec.TaskExecution{expected})
	taskExecutionDetail(t,taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"GET",detail,200).body,expected)
	for _,pair:=range []struct{path string;size int}{{list+"?limit=1",len(page.body)},{detail,len(taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"GET",detail,200).body)}} {
		r:=taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"HEAD",pair.path,200)
		if len(r.body)!=0 || r.header.Get("Content-Length")!=strconv.Itoa(pair.size) {t.Fatal("HEAD did not preserve the actual GET representation")}
	}
	empty:=taskExecutionReadPath(v.a,other.TaskID.String(),id.ExecutionID{})
	taskExecutionPage(t,taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"GET",empty,200).body,[]ec.TaskExecution{})
	for _,path:=range []string{empty+"/"+v.execution.ID.String(),taskExecutionReadPath(v.b,task.ID.String(),v.execution.ID),taskExecutionReadPath(v.a,guardID[wc.Task](t).String(),v.execution.ID)} {
		taskExecutionGet(t,v.workAppFixture,v.client,v.cookie,"GET",path,404)
	}
	for _,path:=range []string{list,detail} {taskExecutionGet(t,v.workAppFixture,foreign,"","GET",path,404)}
	taskExecutionGet(t,v.workAppFixture,v.client,"","GET",list,401)
	// Real technical-close COMMIT is complete while its original Store return
	// remains held. The HTTP caller and original reader cannot finish early.
	v.store.mu.Lock();v.store.lookupProject=v.a.String();v.store.mu.Unlock()
	done:=make(chan struct{});var heldResponse artifactOwnerHTTPResponse;var heldErr error
	go func(){defer close(done);heldResponse,heldErr=taskExecutionHTTP(v.workAppFixture,v.client,v.cookie,"GET",detail)}()
	t.Cleanup(func(){v.store.releaseLookup();<-done})
	var held executionSchedulerAppReturn
	select{case held=<-v.store.lookup:case <-done:t.Fatal("Execution read missed original return hold");case <-time.After(5*time.Second):t.Fatal("Execution read original return not reached")}
	if held.result.State()!=f.Committed {t.Fatal("Execution read close was not committed")}
	select{case <-held.returned:t.Fatal("Execution Store returned before release");default:}
	select{case <-done:t.Fatal("Execution HTTP returned before original Store");default:}
	v.store.releaseLookup();usageAppWait(t,held.returned,"execution-read-store-return");usageAppWait(t,done,"execution-read-http-return")
	workAppCheck(t,"execution-read-held-http",heldErr)
	if heldResponse.status!=200{t.Fatal("held original read did not finish")};taskExecutionDetail(t,heldResponse.body,expected)
	taskExecutionReadClosed(t,v.workAppFixture,v.objects.process.String())
	if taskExecutionFacts(t,v.workAppFixture)!=before{t.Fatal("history GET/HEAD changed canonical business facts")}
	taskExecutionJoinEnabled(t,v)
	second:=taskExecutionDisabled(t,v,env)
	taskExecutionPage(t,taskExecutionGet(t,second,second.client,second.cookie,"GET",list+"?limit=1",200).body,[]ec.TaskExecution{expected})
	taskExecutionDetail(t,taskExecutionGet(t,second,second.client,second.cookie,"GET",detail,200).body,expected)
	taskExecutionReadClosed(t,second,second.objects.process.String())
	if taskExecutionFacts(t,second)!=before {t.Fatal("disabled restart changed original history or invoked work")}
	second.requestStop();usageAppWait(t,second.root.done,"execution-history-root-return");workAppCheck(t,"execution-history-root",second.root.err)
	if !second.assembly.executionReads.Joined() || !second.assembly.Joined() || !second.root.owned.producersJoined(){t.Fatal("disabled original read owner did not join")}
	taskExecutionGuardReleased(t,second)
	t.Log("TASK_EXECUTION_READ_APP enabled_same_pair=true disabled_same_db_history=true original_coordinator_handoff=true canonical_scope=true current_owner=true head=true actual_limit1_final_page=true original_store_return=true original_manager_guard_db_joined=true no_model=true no_sql_seed=true")
}
