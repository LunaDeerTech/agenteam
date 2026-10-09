#!/usr/bin/env python3
"""Independent pending-body/closed-selector checks; no PG or OS listener."""
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path('/workspace/agenteam-runner-control')
OWN = Path(__file__).resolve().parents[2]
OUT = OWN / 'output/ai/work-owner-planning-ui/implementation/runner-pending-review'
OUT.mkdir(parents=True, exist_ok=True)
GO = '/workspace/toolchains/go1.27.1/bin/go'
SELECTOR = '^TestRunnerControlProcessCrashRecovery$/^pending_persisted_before_backend_admission$'
PARENT = 'TestRunnerControlProcessCrashRecovery'
CHILD = PARENT + '/pending_persisted_before_backend_admission'
paths = ['tests/runnercontrol/process_crash_linux_test.go', '.agent-state/task-planning-recovery/pg_only_driver.go', '.agent-state/task-planning-recovery/pg_only_supervisor.py']
frozen = {p:(ROOT/p).read_bytes() for p in paths}
source, driver, supervisor = (frozen[p].decode() for p in paths)

def old(path):
    return subprocess.check_output(['git','show','eea4ced0:'+path],cwd=ROOT).decode()

def remove(text, fragment):
    assert text.count(fragment) == 1
    return text.replace(fragment,'',1)

start = source.index('// HTTP/1.1 detects peer loss')
stop = source.index('// This transport has two finite holds',start)
helper = source[source.index('func runnerCrashPendingBody(',start):stop]
branch = '''				if !runnerCrashPendingBody(request) {
					v.mu.Lock()
					v.failed = true
					v.mu.Unlock()
					http.Error(w, "unavailable", http.StatusBadGateway)
					return
				}
'''
assert remove(source[:start]+source[stop:],branch) == old(paths[0])
start = driver.index('\tif *selector == runnerPendingCrashSelector {')
stop = driver.index('\n\tnonce, err :=',start)
discovery = driver[start:stop]
reduced = driver[:start]+driver[stop:]
reduced = remove(reduced, 'const runnerPendingCrashSelector = "'+SELECTOR+'"\n')
reduced = remove(reduced, ' && *selector != runnerPendingCrashSelector')
reduced = reduced.replace('\tdefer cancel()\n\n\tnonce, err :=','\tdefer cancel()\n\tnonce, err :=',1)
assert reduced == old(paths[1])
start = supervisor.index('\n\ndef observe_runner_pending_crash(')
stop = supervisor.index('\n\ndef observe_runner_native_safety(',start)
reduced = remove(supervisor[:start]+supervisor[stop:], 'RUNNER_PENDING_CRASH_SELECTOR = '+repr(SELECTOR)+'\n')
reduced = reduced.replace("if not args.root_chain and args.run in ('^TestRunnerControlProcessCrashRecovery$', RUNNER_PENDING_CRASH_SELECTOR):", "if not args.root_chain and args.run == '^TestRunnerControlProcessCrashRecovery$':",1)
reduced = remove(reduced, '''            if args.run == RUNNER_PENDING_CRASH_SELECTOR:
                log.flush()
                if not observe_runner_pending_crash(log_path, log):
                    code = 1
''')
assert reduced == old(paths[2])
ns={'__name__':'independent_readonly','__file__':str(ROOT/paths[2])}
exec(compile(supervisor,str(ROOT/paths[2]),'exec'),ns)
with tempfile.TemporaryDirectory(dir=OUT,prefix='pure-') as directory:
    folder=Path(directory)
    log=folder/'log'
    good=f'=== RUN   {PARENT}\n=== RUN   {CHILD}\n    --- PASS: {CHILD} (1.0s)\n--- PASS: {PARENT} (1.1s)\nPASS\n'
    cases=[(good,True),(good+f'=== RUN   {PARENT}\n',False),(good.replace('PASS: '+CHILD,'SKIP: '+CHILD),False),(good.replace('PASS: '+PARENT+' (','FAIL: '+PARENT+' ('),False),(good.replace(CHILD,CHILD+'/extra'),False)]
    for raw,want in cases:
        log.write_text(raw)
        assert ns['observe_runner_pending_crash'](log,io.StringIO()) is want
    log.write_bytes(good.encode()+b'\xff')
    assert not ns['observe_runner_pending_crash'](log,io.StringIO())
    assert not ns['observe_runner_pending_crash'](folder,io.StringIO())
    print('PASS 3 source inverse checks and 7 closed log controls',flush=True)
    h=folder/'helper_test.go'
    h.write_text('''package pending_test
import("io";"net/http";p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol")
'''+helper)
    memory=(ROOT/'.agent-state/runner-control/pending_request_body_test.go').read_text()
    old_read='''			n, err := io.Copy(io.Discard, io.LimitReader(r.Body, 1025))
			closeErr := r.Body.Close()
			bodyDone <- err == nil && closeErr == nil && n == r.ContentLength && n == 7'''
    assert memory.count(old_read)==1
    memory=memory.replace(old_read,'\t\t\tbodyDone <- runnerCrashPendingBody(r)')
    (folder/'memory_test.go').write_text(memory)
    # Independent controls require the original Close to return, retain original
    # read errors even after declared bytes, and execute the actual discovery.
    extra=r'''package pending_test
import("bytes";"context";"errors";"fmt";"io";"net/http";"os/exec";"strings";"syscall";"testing";"time")
type heldBody struct { *bytes.Reader; entered, release chan struct{}; failure error }
func(b *heldBody)Close()error {close(b.entered);<-b.release;return b.failure}
func TestIndependentOriginalClose(t *testing.T){
 for _,bad:=range []bool{false,true}{t.Run(fmt.Sprint(bad),func(t *testing.T){
  b:=&heldBody{Reader:bytes.NewReader([]byte("payload")),entered:make(chan struct{}),release:make(chan struct{})}
  if bad {b.failure=errors.New("owned Close error")}
  result:=make(chan bool,1); go func(){result<-runnerCrashPendingBody(&http.Request{Body:b,ContentLength:7})}()
  select{case<-b.entered:case<-time.After(time.Second):t.Fatal("original Close not entered")}
  select{case<-result:t.Fatal("checkpoint before original Close returned");default:}
  close(b.release)
  select{case got:=<-result:if got==bad {t.Fatal("original Close result lost")};case<-time.After(time.Second):t.Fatal("helper not joined")}
 })}
}
type bytesAndError struct{}
func(bytesAndError)Read(b []byte)(int,error){return copy(b,"payload"),io.ErrUnexpectedEOF}
func(bytesAndError)Close()error{return nil}
func TestIndependentOriginalReadError(t *testing.T){if runnerCrashPendingBody(&http.Request{Body:bytesAndError{},ContentLength:7}){t.Fatal("declared byte length concealed failed EOF")}}
func fail(string)int{return 1}
const runnerPendingCrashSelector="SELECTOR"
func originalDiscovery(ctx context.Context,path string)int { binary:=&path; chosen:=runnerPendingCrashSelector; selector:=&chosen
DISCOVERY
return 0
}
func TestIndependentActualDiscovery(t *testing.T){
 binary:="/workspace/agenteam-runner-control/output/ai/runner-control/runnercontrol-process-crash-race-2.test"
 ctx,cancel:=context.WithTimeout(context.Background(),time.Second*3);defer cancel()
 if originalDiscovery(ctx,binary)!=0 {t.Fatal("actual precompiled exact parent not discovered")}
 expired,stop:=context.WithCancel(context.Background());stop()
 if originalDiscovery(expired,binary)!=1 {t.Fatal("cancelled discovery admitted")}
 if originalDiscovery(ctx,binary+".absent")!=1 {t.Fatal("failed Start admitted")}
}
'''.replace('SELECTOR',SELECTOR).replace('DISCOVERY',discovery)
    (folder/'independent_test.go').write_text(extra)
    env=os.environ.copy()
    env.update(PATH=str(Path(GO).parent)+os.pathsep+env.get('PATH',''),GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOPROXY='off',GOSUMDB='off',GOTELEMETRY='off',GOMAXPROCS='2',GOFLAGS='-mod=readonly -p=1',GOMODCACHE='/workspace/agenteam/output/ai/model-ui-recovery/go-mod',GOCACHE=str(OWN/'output/ai/work-owner-planning-ui/implementation/gocache'))
    virtuals=[ROOT/'tests/runnercontrol'/('review_pending_'+name) for name in ('helper_test.go','memory_test.go','independent_test.go')]
    overlay=folder/'overlay.json'
    overlay.write_text(json.dumps({'Replace':dict(zip(map(str,virtuals),map(str,[h,folder/'memory_test.go',folder/'independent_test.go'])))}))
    result=subprocess.run([GO,'test','-race','-count=1','-overlay='+str(overlay),'-timeout=12s','-v','-run=^Test(PendingCancellationAfterBodyEOF|Independent)',*map(str,virtuals)],cwd=ROOT,env=env,timeout=45)
    assert all((ROOT/p).read_bytes()==v for p,v in frozen.items())
    raise SystemExit(result.returncode)
