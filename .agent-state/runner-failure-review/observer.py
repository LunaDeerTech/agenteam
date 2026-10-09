#!/usr/bin/env python3
"""Actual C observer, stdlib only. --expect-reject verifies the deadline repair."""
import ast
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path('/workspace/agenteam-runner-control')
OWN = Path('/workspace/agenteam-work-ui')
GO = '/workspace/toolchains/go1.27.1/bin/go'
source = (ROOT/'tests/process/runner_failure_test.go').read_text()
script = ast.parse((ROOT/'.agent-state/runner-control/failure-transport-controls.py').read_text())
main = next(n for n in script.body if isinstance(n, ast.FunctionDef) and n.name == 'main')
assign = next(n for n in main.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='helpers' for t in n.targets))
prefix = ast.literal_eval(assign.value.left)
helpers = prefix + source[source.index('// This transport may hold'):source.index('func newRunnerFailureCentral(')]
PROBE = r'''package process_test
import (
 "runtime"
 "strings"
 "testing"
 "time"
)
func TestIndependentExpiredEmptySnapshot(t *testing.T) {
 v := &runnerFailureTransport{}
 v.mu.Lock()
 locked := true
 defer func(){ if locked { v.mu.Unlock() } }()
 done := make(chan struct{})
 go func(){ defer close(done); v.controlRetired(t) }()
 // Confirm the original method has created its original deadline and is
 // actually inside the blocked snapshot. No start/sleep inference.
 deadline := time.Now().Add(time.Second)
 for {
  stack := make([]byte,65536)
  stack=stack[:runtime.Stack(stack,true)]
  found:=false
  for _, part := range strings.Split(string(stack),"\n\n") {
   if strings.Contains(part,"runnerFailureTransport).upgradeRemainder") && strings.Contains(part,"runnerFailureTransport).controlRetired") { found=true; break }
  }
  if found { break }
  if time.Now().After(deadline) { t.Fatal("original locked snapshot was not reached") }
  runtime.Gosched()
 }
 timer:=time.NewTimer(3100*time.Millisecond)
 <-timer.C
 v.mu.Unlock(); locked=false
 select { case <-done: case <-time.After(time.Second): t.Fatal("original retirement method did not return") }
 if !t.Failed() { t.Fatal("EXPIRED_EMPTY_SNAPSHOT_ACCEPTED") }
}
'''
env=os.environ.copy()
env.update({'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off',
 'GOTELEMETRY':'off','GOMAXPROCS':'2','GOFLAGS':'-mod=readonly',
 'GOCACHE':str(OWN/'output/ai/work-owner-planning-ui/implementation/gocache')})
output=OWN/'output/ai/work-owner-planning-ui/runner-failure-review'
output.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(dir=output) as tmp:
 tmp=Path(tmp)
 h=tmp/'actual_helper_test.go'; h.write_text(helpers)
 c=tmp/'owner_controls_test.go'; c.write_text((ROOT/'.agent-state/runner-control/failure_transport_controls_test.go').read_text())
 p=tmp/'independent_deadline_test.go'; p.write_text(PROBE)
 command=[GO,'test','-race','-count=1','-timeout=10s','-v']
 if '--deadline-only' not in sys.argv:
  result=subprocess.run(command+['-run=^TestFailureTransport',str(h),str(c)],cwd=ROOT,env=env,timeout=30)
  if result.returncode: raise SystemExit(result.returncode)
 result=subprocess.run(command+['-run=^TestIndependentExpiredEmptySnapshot$',str(h),str(p)],cwd=ROOT,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
 print(result.stdout,end='')
 accepted='EXPIRED_EMPTY_SNAPSHOT_ACCEPTED' in result.stdout
 rejected='original default control transport has not joined' in result.stdout
 if '--expect-reject' in sys.argv:
  if result.returncode!=1 or accepted or not rejected: raise SystemExit('expected original-deadline rejection was not observed')
  print('PASS expected actual gate rejection after its original deadline; no resource execution')
 else:
  if result.returncode!=1 or not accepted: raise SystemExit('original missing-deadline control not reproduced')
  raise SystemExit(1)
