#!/usr/bin/env python3
"""Independent actual Account producer -> exact Variables refusal method.

Only SQL results are explicit substitutes. Uses a read-only Go overlay so no
source/cache in the reviewed tree is written. No server, socket or PG starts.
"""
import json, os, subprocess
from pathlib import Path
ROOT=Path('/workspace/agenteam-project-variables-ui')
REVIEW=Path(__file__).resolve().parent
OWN=REVIEW.parents[1]
BASE=OWN/'output/ai/runner-control'
OUTPUT=BASE/'tmp/variables-native-review'
HEADER=r'''package main
import("bytes";"context";"encoding/json";"fmt";"io";"net/http";"net/http/httptest";"os";"strings";"sync"
"github.com/LunaDeerTech/agenteam/internal/central/account"
f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
"github.com/LunaDeerTech/agenteam/internal/central/httpapi")
type report struct { failed int; logs []string }
func (r *report) Error(...any){r.failed++}
func (r *report) Log(v ...any){r.logs=append(r.logs,fmt.Sprint(v...))}
type fakeRow struct{ values [6]int; err error }
func (r fakeRow) Scan(dest ...any) error { if r.err!=nil{return r.err};if len(dest)!=6{panic("scan arity")};for i,d:=range dest{*d.(*int)=r.values[i]};return nil }
type fakeStore struct{ calls int; values [6]int; key string }
func(s *fakeStore) QueryRow(ctx context.Context, query string, args ...any)fakeRow{
 s.calls++;if ctx.Err()!=nil{return fakeRow{err:ctx.Err()}}
 if strings.Count(query,"SELECT")!=7||len(args)!=7||args[0]!=project||args[1]!=target||args[2]!=s.key||args[3]!="CUSTOM_VALUE"||args[4]!="seed description"||args[5]!="seed value"||args[6]!="1"{panic("SQL original identity/preimage binding")}
 if !strings.Contains(query,"idempotency_key=$3")||!strings.Contains(query,"deleted_at IS NULL")||strings.Contains(query,"UPDATE ")||strings.Contains(query,"DELETE "){panic("SQL boundary")}
 return fakeRow{values:s.values}
}
type owner struct{ t *report; ids map[string]string; store *fakeStore; stopProxy func() }
type variableWebAttempt struct {Index int;Method,Path,Query,Project,Target,Command,Key,RequestID string;Body,Receipt []byte;Status int;EOF,Closed bool}
type projectVariablesWebFixture struct{mode string;owner *owner;mu sync.Mutex;attempts []*variableWebAttempt;targets map[string]string;initial map[string]any;setupFacts map[string][2]int}
const project="01970000-0000-7000-8000-000000000010"
const target="01970000-0000-7000-8000-000000000020"
const requestID="01970000-0000-7000-8000-000000000090"
const endpoint="/api/v1/projects/"+project+"/variables/"+target
func makeFixture()(*projectVariablesWebFixture,*report,*fakeStore,*int){
 stopped:=new(int);r:=&report{};s:=&fakeStore{values:[6]int{0,1,1,1,1,1},key:"original-private-key"}
 raw,_:=json.Marshal(map[string]any{"type":"urn:agenteam:problem:project-not-active","title":"Project not active","status":409,"detail":"The project is not active.","instance":endpoint,"request_id":requestID,"code":"PROJECT_NOT_ACTIVE","commit_state":"not_committed"})
 a:=&variableWebAttempt{Index:1,Method:"PATCH",Path:endpoint,Project:project,Target:target,Command:"project.variable.update",Key:s.key,RequestID:requestID,Body:[]byte(`{"expected_version":"1","request":{"value":"prepared before archive"}}`),Receipt:raw,Status:409,EOF:true,Closed:true}
 v:=&projectVariablesWebFixture{mode:"authority",owner:&owner{t:r,ids:map[string]string{"main":project},store:s,stopProxy:func(){*stopped++}},attempts:[]*variableWebAttempt{a},targets:map[string]string{"main":target},initial:map[string]any{"main":map[string]any{"name":"CUSTOM_VALUE","description":"seed description","value":"seed value","version":"1"}},setupFacts:map[string][2]int{project:{1,1}}}
 return v,r,s,stopped
}
'''
MAIN=r'''func main(){
 v,r,s,_:=makeFixture()
 recorder:=httptest.NewRecorder()
 handler:=httpapi.WithRequestID(nil,http.HandlerFunc(func(w http.ResponseWriter, req *http.Request){
  (&account.HTTPBoundary{}).WriteProblem(w,req,f.NewFault(f.ProjectNotActive,f.NotCommitted))
 }))
 handler.ServeHTTP(recorder,httptest.NewRequest("PATCH","https://offline.invalid"+endpoint,nil))
 var problem httpapi.Problem
 if json.Unmarshal(recorder.Body.Bytes(),&problem)!=nil {panic("producer decode")}
 if problem.Instance!="/api/v1"||problem.Status!=409||problem.Code!=f.ProjectNotActive {panic("unexpected producer")}
 if err:=os.WriteFile(os.Args[1],recorder.Body.Bytes(),0600);err!=nil{panic("producer save")}
 v.attempts[0].Receipt=append([]byte(nil),recorder.Body.Bytes()...)
 v.attempts[0].RequestID=recorder.Header().Get("X-Request-ID")
 v.verifyAuthorityRefusal(context.Background())
 fmt.Printf("actual_boundary_instance=%s exact_method_failures=%d sql_calls=%d\n",problem.Instance,r.failed,s.calls)
 if r.failed!=0||s.calls!=1 {panic("actual producer refused before SQL by diagnostic")}
}
'''
OUTPUT.mkdir(parents=True,exist_ok=True)
fixture=(ROOT/'tests/account/project_variables_web_fixture_test.go').read_text()
method=fixture[fixture.index('func (v *projectVariablesWebFixture) verifyAuthorityRefusal('):fixture.index('// Independent SQL postconditions')]
source=OUTPUT/'authority-boundary-current.go';source.write_text(HEADER+method+MAIN)
virtual=ROOT/'.agent-state/project-variables-ui/independent-boundary.go'
overlay=OUTPUT/'authority-overlay-current.json'
overlay.write_text(json.dumps({'Replace':{str(virtual):str(source)}}))
env=dict(os.environ,PATH='/workspace/toolchains/go1.27.1/bin:'+os.environ['PATH'],AGENTEAM_GO='/workspace/toolchains/go1.27.1/bin/go',GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOPROXY='off',GOSUMDB='off',GOTELEMETRY='off',GOFLAGS='-mod=readonly -p=1',GOMAXPROCS='2',GOCACHE=str(BASE/'gocache'),GOMODCACHE=str(BASE/'go-mod'),GOTMPDIR=str(BASE/'tmp'),XDG_CONFIG_HOME=str(BASE/'go-config'))
result=subprocess.run(['/workspace/toolchains/go1.27.1/bin/go','run','-overlay',str(overlay),str(virtual),str(OUTPUT/'authority-boundary.json')],cwd=ROOT,env=env,timeout=60)
if result.returncode:raise SystemExit(result.returncode)
from jsonschema import Draft202012Validator
schema=json.loads((ROOT/'api/openapi/common.json').read_text());schema['$ref']='#/components/schemas/Problem'
Draft202012Validator(schema).validate(json.loads((OUTPUT/'authority-boundary.json').read_text()))
print('actual producer formal schema: PASS',flush=True)
for name in ('boundary-observer.cjs','retirement-deadline.cjs'):
 result=subprocess.run(['node',str(REVIEW/name)],cwd=OWN,env=env,timeout=30)
 if result.returncode:raise SystemExit(result.returncode)
