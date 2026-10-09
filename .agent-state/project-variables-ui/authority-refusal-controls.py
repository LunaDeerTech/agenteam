#!/usr/bin/env python3
"""Actual Go refusal method and typed projections with explicit SQL substitutes.

No database/process/browser fixture is started. SQL results are independent
stimuli, so this proves binding/failure control flow, not PostgreSQL semantics.
"""
from pathlib import Path
import os
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
source = (root / 'tests/account/project_variables_web_fixture_test.go').read_text()
method = source[source.index('func (v *projectVariablesWebFixture) verifyAuthorityRefusal('):source.index('// Independent SQL postconditions')]
types = source[source.index('type variableWebNetworkFailure struct'):source.index('// Runs on both browser success')]
top = (root / 'tests/account/project_variables_web_test.go').read_text()
assert top.index('defer fixture.verifyAuthorityRefusal(ctx)') < top.index('result := fixture.owner.browser(ctx)')
program = r'''package main
import("bytes";"context";"encoding/json";"fmt";"io";"net/http";"runtime";"strings";"sync"
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
func modifyProblem(a *variableWebAttempt,key string,value any){var m map[string]any;json.Unmarshal(a.Receipt,&m);m[key]=value;a.Receipt,_=json.Marshal(m)}
func verifyProjection(){
 decode:=func(raw []byte)bool{var n variableWebAuthorityDiagnostic;d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if d.Decode(&n)!=nil{return false};var tail any;if d.Decode(&tail)!=io.EOF{return false};return n.valid()}
 original:=[]byte(`{"installed":true,"joined":true,"request_bound":true,"private_bound":true,"native_retired":true,"consumer":{"calls":1,"target_calls":1,"fulfilled":0,"rejected":1,"synchronous_throws":0,"status":409,"native_before":0,"native_after":1,"pending":0,"typed_problem":true,"instance_matches":true,"entry_authenticated":true,"entry_idle":true,"entry_identity":true,"identity_current":true,"authenticated":true,"owner_idle":true,"progress_rejected":true,"receipt_absent":true,"draft_matches":true,"document_matches":true,"target_route":true,"native_request_matches":true,"problem_request_matches":true,"observer_failed":false,"hooks_retired":true,"code":"PROJECT_NOT_ACTIVE","commit_state":"not_committed"}}`)
 if !decode(original){panic("authority positive")}
 for _,bad:=range []struct{key string;value any}{{"calls",4097},{"pending",-1},{"status",600},{"code","PRIVATE"},{"commit_state","PRIVATE"},{"owner_idle",1},{"private","PRIVATE"}}{var m map[string]any;json.Unmarshal(original,&m);m["consumer"].(map[string]any)[bad.key]=bad.value;raw,_:=json.Marshal(m);if decode(raw){panic("authority negative "+bad.key)}}
 if decode(append(append([]byte{},original...),[]byte("{}")...))||decode(original[:len(original)-1]){panic("authority EOF")}
 for _,n:=range []variableWebIncomplete{{Index:0,Method:"PATCH",Route:"update",Expected:"none"},{Index:2,Method:"PATCH",Route:"update",Expected:"none"},{Index:1,Method:"PRIVATE",Route:"update",Expected:"none"},{Index:1,Method:"PATCH",Route:"PRIVATE",Expected:"none"},{Index:1,Method:"PATCH",Route:"update",Expected:"PRIVATE"},{Index:1,Method:"PATCH",Route:"update",Expected:"none",Finished:true}}{if n.valid(1){panic("incomplete projection")}}
 if !(variableWebIncomplete{Index:1,Method:"PATCH",Route:"update",Expected:"none",Status:409,Failed:true}).valid(1){panic("incomplete positive")}
}
func main(){
 verifyProjection();cases:=0
 v,r,s,stopped:=makeFixture();v.verifyAuthorityRefusal(context.Background());if r.failed!=0||s.calls!=1||*stopped!=1||len(r.logs)!=1{panic("positive refusal")};cases++
 changes:=[]func(*projectVariablesWebFixture){
 func(v *projectVariablesWebFixture){v.attempts=append(v.attempts,v.attempts[0])},
 func(v *projectVariablesWebFixture){v.attempts[0].Command="project.variable.create"},
 func(v *projectVariablesWebFixture){v.attempts[0].Key=""},
 func(v *projectVariablesWebFixture){v.attempts[0].Query="different=1"},
 func(v *projectVariablesWebFixture){v.attempts[0].Path+="/different"},
 func(v *projectVariablesWebFixture){v.attempts[0].EOF=false},
 func(v *projectVariablesWebFixture){v.attempts[0].Closed=false},
 func(v *projectVariablesWebFixture){v.attempts[0].Status=200},
 func(v *projectVariablesWebFixture){modifyProblem(v.attempts[0],"code","INVALID_STATE")},
 func(v *projectVariablesWebFixture){modifyProblem(v.attempts[0],"commit_state","unknown")},
 func(v *projectVariablesWebFixture){modifyProblem(v.attempts[0],"instance","/different")},
 func(v *projectVariablesWebFixture){modifyProblem(v.attempts[0],"request_id",project)},
 func(v *projectVariablesWebFixture){modifyProblem(v.attempts[0],"private","PRIVATE")},
 func(v *projectVariablesWebFixture){v.attempts[0].Receipt=append(v.attempts[0].Receipt,[]byte("{}")...)},
 func(v *projectVariablesWebFixture){v.attempts=append(v.attempts,&variableWebAttempt{RequestID:requestID})},
 func(v *projectVariablesWebFixture){v.attempts=append(v.attempts,&variableWebAttempt{Project:project,Key:v.attempts[0].Key})},
 func(v *projectVariablesWebFixture){v.initial["main"]=nil},
 func(v *projectVariablesWebFixture){v.attempts[0].Body=[]byte(`{"expected_version":"2","request":{"value":"prepared before archive"}}`)},
 func(v *projectVariablesWebFixture){v.attempts[0].Body=[]byte(`{"request":{"value":"prepared before archive"},"expected_version":"1"}`)},
 }
 for i,change:=range changes{v,r,s,stopped=makeFixture();change(v);v.verifyAuthorityRefusal(context.Background());if r.failed!=1||s.calls!=0||*stopped!=1{panic(fmt.Sprint("frame negative ",i))};cases++}
 for i:=range 6{v,r,s,_=makeFixture();s.values[i]++;v.verifyAuthorityRefusal(context.Background());if r.failed!=1||s.calls!=1{panic(fmt.Sprint("fact negative ",i))};cases++}
 v,r,s,_=makeFixture();v.attempts=nil;v.verifyAuthorityRefusal(context.Background());if r.failed!=0||s.calls!=0||len(r.logs)!=1||!strings.Contains(r.logs[0],"not observed"){panic("unexecuted is not verified")};cases++
 v,r,s,stopped=makeFixture();v.mode="crud";v.verifyAuthorityRefusal(context.Background());if r.failed!=0||s.calls!=0||*stopped!=0{panic("non-authority changed")};cases++
 v,r,s,_=makeFixture();ctx,cancel:=context.WithCancel(context.Background());cancel();v.verifyAuthorityRefusal(ctx);if r.failed!=1||s.calls!=1{panic("original context failure")};cases++
 v,r,s,stopped=makeFixture();done:=make(chan struct{});go func(){defer close(done);defer v.verifyAuthorityRefusal(context.Background());runtime.Goexit()}();<-done;if r.failed!=0||s.calls!=1||*stopped!=1{panic("Fatal Goexit tail")};cases++
 // The observation is read-only: no SQL call on malformed original evidence,
 // no generic success from a missing attempt, no cancellation-budget replacement.
 fmt.Printf("actual authority method: %d binding/fact/Goexit controls; typed closed projection controls PASS\n",cases)
}
'''
program = program.replace('func main(){', types + '\n' + method + '\nfunc main(){', 1)
output = root / 'output/ai/project-variables-ui/implementation'
env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOMAXPROCS='2',
           GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod',
           GOCACHE=str(output / 'gocache'), GOTMPDIR=str(output / 'tmp'))
with tempfile.TemporaryDirectory(prefix='authority-refusal-', dir=output) as temporary:
    path = Path(temporary) / 'main.go'
    path.write_text(program)
    result = subprocess.run(['/workspace/toolchains/go1.27.1/bin/go', 'run', '-mod=readonly', '-p=1', str(path)],
                            cwd=root, env=env, timeout=45, check=False)
    raise SystemExit(result.returncode)
