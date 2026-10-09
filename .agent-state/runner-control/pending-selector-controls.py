#!/usr/bin/env python3
"""Closed pending-child mapping; pure gates and local discovery children only."""
import contextlib
import io
import os
from pathlib import Path
import re
import runpy
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DRIVER = ROOT / '.agent-state/task-planning-recovery/pg_only_driver.go'
GO = '/workspace/toolchains/go1.27.1/bin/go'
SELECTOR = '^TestRunnerControlProcessCrashRecovery$/^pending_persisted_before_backend_admission$'
PARENT = 'TestRunnerControlProcessCrashRecovery'
CHILD = PARENT + '/pending_persisted_before_backend_admission'
checks = []


def check(label, good):
    assert good, label
    checks.append(label)


source, driver = SUPERVISOR.read_text(), DRIVER.read_text()
constant = "RUNNER_PENDING_CRASH_SELECTOR = " + repr(SELECTOR) + '\n'
check('exact supervisor literal once', source.count(constant) == 1)
start, end = source.index('\n\ndef observe_runner_pending_crash('), source.index('\n\ndef observe_runner_native_safety(')
reduced = source[:start] + source[end:]
reduced = reduced.replace(constant, '')
new_condition = "if not args.root_chain and args.run in ('^TestRunnerControlProcessCrashRecovery$', RUNNER_PENDING_CRASH_SELECTOR):"
old_condition = "if not args.root_chain and args.run == '^TestRunnerControlProcessCrashRecovery$':"
check('one shared original closure entry', reduced.count(new_condition) == 1)
reduced = reduced.replace(new_condition, old_condition)
hook = '''            if args.run == RUNNER_PENDING_CRASH_SELECTOR:
                log.flush()
                if not observe_runner_pending_crash(log_path, log):
                    code = 1
'''
check('one terminal case gate before original TCP tail', reduced.count(hook) == 1)
reduced = reduced.replace(hook, '')
base = subprocess.check_output(['git', 'show', 'eea4ced0:' + str(SUPERVISOR.relative_to(ROOT))], cwd=ROOT, text=True)
check('every previous supervisor byte preserved', reduced == base)

literal = 'const runnerPendingCrashSelector = "' + SELECTOR + '"\n'
check('exact driver literal once', driver.count(literal) == 1)
start = driver.index('\tif *selector == runnerPendingCrashSelector {')
end = driver.index('\n\tnonce, err :=', start)
discovery = driver[start:end]
old_driver = (driver[:start] + driver[end:]).replace(literal, '').replace(' && *selector != runnerPendingCrashSelector', '')
# The removed branch occupied a line immediately before nonce generation.
old_driver = old_driver.replace('\tdefer cancel()\n\n\tnonce, err :=', '\tdefer cancel()\n\tnonce, err :=')
base_driver = subprocess.check_output(['git', 'show', 'eea4ced0:' + str(DRIVER.relative_to(ROOT))], cwd=ROOT, text=True)
check('every previous driver byte preserved', old_driver == base_driver)
check('discovery actual parent only and original context', 'exec.CommandContext(ctx, *binary, "-test.list=^' + PARENT + '$")' in discovery)
check('original child run still uses original selected literal', driver.count('"-test.run="+*selector') == 1)

# Reuse the original 34 closure/mutation/deadline controls on the byte-identical
# old projection. New-case deadline and observer controls below use actual source.
read_text = Path.read_text
def projected_read(path, *args, **kwargs):
    return reduced if path == SUPERVISOR else read_text(path, *args, **kwargs)
with patch.object(Path, 'read_text', projected_read):
    runpy.run_path(str(Path(__file__).with_name('crash-input-controls.py')), run_name='__review__')
check('original closure controls retained', True)

ns = {'__name__': 'offline_only', '__file__': str(SUPERVISOR)}
exec(compile(source, str(SUPERVISOR), 'exec'), ns)
with tempfile.TemporaryDirectory(prefix='pending-selector-', dir=ROOT / 'output/ai/runner-control/tmp') as temporary:
    temp = Path(temporary)
    log = temp / 'observed.log'
    valid = f'=== RUN   {PARENT}\n=== RUN   {CHILD}\n--- PASS: {PARENT} (1.00s)\n    --- PASS: {CHILD} (0.50s)\nPASS\n'
    cases = [('exact parent and child', valid, True),
             ('missing parent run', valid.replace(f'=== RUN   {PARENT}\n', ''), False),
             ('missing child run', valid.replace(f'=== RUN   {CHILD}\n', ''), False),
             ('duplicate child run', valid + f'=== RUN   {CHILD}\n', False),
             ('missing parent pass', valid.replace(f'--- PASS: {PARENT} (1.00s)\n', ''), False),
             ('missing child pass', valid.replace(f'    --- PASS: {CHILD} (0.50s)\n', ''), False),
             ('failed child', valid.replace(f'--- PASS: {CHILD}', f'--- FAIL: {CHILD}'), False),
             ('skipped parent', valid.replace(f'--- PASS: {PARENT}', f'--- SKIP: {PARENT}'), False),
             ('duplicate pass', valid + f'--- PASS: {CHILD} (0.50s)\n', False),
             ('wrong sibling', valid.replace('pending_persisted_before_backend_admission', 'backend_committed_before_active_file_publication'), False),
             ('extra sibling', valid + f'=== RUN   {PARENT}/backend_committed_before_active_file_publication\n--- PASS: {PARENT}/backend_committed_before_active_file_publication (1.00s)\n', False)]
    for name, raw, expected in cases:
        log.write_text(raw)
        check(name, ns['observe_runner_pending_crash'](log, io.StringIO()) is expected)
    log.write_bytes(valid.encode() + b'\xff')
    safe = io.StringIO()
    check('invalid UTF8 safely rejects', not ns['observe_runner_pending_crash'](log, safe) and safe.getvalue() == 'RUNNER pending_crash_exact_case=False log_unreadable=True\n')
    log.unlink()
    check('missing log safely rejects', not ns['observe_runner_pending_crash'](log, io.StringIO()))

    for boundary in (123.0, 123.1):
        clock, launches, captured = [0.0], [], []
        class UnexpectedLaunch(BaseException):
            pass
        class Loader:
            def exec_module(self, module):
                def capture(*args):
                    captured.append(args)
                    clock[0] = 122.9
                    return {}
                module.capture = capture
        def baseline():
            clock[0] = boundary
            return set()
        def launch(*args, **kwargs):
            launches.append(True)
            raise UnexpectedLaunch()
        gate = {'__name__': 'offline_only', '__file__': str(SUPERVISOR)}
        exec(compile(source, str(SUPERVISOR), 'exec'), gate)
        gate.update(time=types.SimpleNamespace(monotonic=lambda: clock[0], monotonic_ns=lambda: int(clock[0]*1e9)),
                    tcp=baseline, ctypes=types.SimpleNamespace(CDLL=lambda *a, **kw: types.SimpleNamespace(prctl=lambda *a: 0)),
                    importlib=types.SimpleNamespace(util=types.SimpleNamespace(spec_from_file_location=lambda *a: types.SimpleNamespace(loader=Loader()), module_from_spec=lambda *a: types.SimpleNamespace())),
                    subprocess=types.SimpleNamespace(Popen=launch, STDOUT=subprocess.STDOUT, TimeoutExpired=subprocess.TimeoutExpired))
        fake_driver, binary = temp / 'driver', temp / 'binary'
        fake_driver.write_bytes(b'closed control driver')
        binary.write_bytes(b'closed control binary')
        args = ['offline', '--driver', str(fake_driver), '--binary', str(binary), '--run', SELECTOR, '--output', str(temp / 'out')]
        with patch.object(sys, 'argv', args), contextlib.redirect_stdout(io.StringIO()):
            try:
                result = gate['main']()
            except UnexpectedLaunch:
                result = 'unexpected'
        check('new case no driver after original deadline ' + str(boundary), result == 1 and not launches and len(captured) == 1 and captured[0][-1] == 123)

    constants = '\n'.join(line for line in driver.splitlines() if line.startswith('const runner'))
    begin = driver.index('\topts := flag.NewFlagSet(')
    finish = driver.index('\tstart := time.Now()', begin)
    guard = driver[begin:finish].replace('opts.Parse(os.Args[1:])', 'opts.Parse(args)')
    go_source = r'''package mapping_test
import (
 "context"
 "flag"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "syscall"
 "testing"
 "time"
)
func fail(string) int {return 1}
''' + constants + '\nfunc actualGuard(args []string) int {\n' + guard + '\nreturn 0\n}\n' + 'func actualDiscovery(ctx context.Context, path string) int {\nbinary:=&path\nselected:=runnerPendingCrashSelector\nselector:=&selected\n' + discovery + '\nreturn 0\n}\n' + r'''
func TestActualPendingGuard(t *testing.T) {
 good:=[]string{runnerPendingCrashSelector,"^TestRunnerControlProcessCrashRecovery$",runnerNativeSafetySelector,runnerCurrentAuthoritySelector,"^TestRunnerControlNativeGeneration$"}
 for _,s:=range good {if actualGuard([]string{"--test-binary","owned","--directory","fresh","--run",s})!=0 {t.Fatal("exact original selector rejected")}}
 bad:=[]string{"TestRunnerControlProcessCrashRecovery/pending_persisted_before_backend_admission","^TestRunnerControlProcessCrashRecovery$/pending_persisted_before_backend_admission","^TestRunnerControlProcessCrashRecovery$/^pending.*$","^TestRunnerControlProcessCrashRecovery$/^backend_committed_before_active_file_publication$","^TestRunnerControlProcessCrashRecovery$/^(pending_persisted_before_backend_admission|backend_committed_before_active_file_publication)$","^TestRunnerControlProcessCrashRecovery$/^pending_persisted_before_backend_admission$|^TestOther$",runnerPendingCrashSelector+"/extra","^TestRunnerControlProcessCrashRecovery$/^Pending_persisted_before_backend_admission$"}
 for _,s:=range bad {if actualGuard([]string{"--test-binary","owned","--directory","fresh","--run",s})!=1 {t.Fatal("non-exact child selector admitted")}}
 if actualGuard([]string{"--test-binary","owned","--directory","fresh","--run",runnerPendingCrashSelector,"extra"})!=1 {t.Fatal("extra argument admitted")}
}
func TestActualParentDiscovery(t *testing.T) {
 for _,c:=range []struct{name,body string; want int}{
  {"exact", "printf '%s\\n' TestRunnerControlProcessCrashRecovery",0},
  {"duplicate", "printf '%s\\n' TestRunnerControlProcessCrashRecovery TestRunnerControlProcessCrashRecovery",1},
  {"wrong_parent", "printf '%s\\n' TestOther",1},
  {"child_not_discovery", "printf '%s\\n' TestRunnerControlProcessCrashRecovery/pending_persisted_before_backend_admission",1},
  {"list_error", "exit 1",1},
  {"original_context_expired", "exec sleep 5",1},
 } {
  t.Run(c.name,func(t *testing.T){
   path:=filepath.Join(t.TempDir(),"owned-discovery")
   body:="#!/bin/sh\n[ \"$1\" = '-test.list=^TestRunnerControlProcessCrashRecovery$' ] || exit 9\n"+c.body+"\n"
   if err:=os.WriteFile(path,[]byte(body),0700);err!=nil {t.Fatal("owned discovery control setup")}
   duration:=time.Second
   if c.name=="original_context_expired" {duration=50*time.Millisecond}
   ctx,cancel:=context.WithTimeout(context.Background(),duration);defer cancel()
   if actualDiscovery(ctx,path)!=c.want {t.Fatal("actual parent discovery result mismatch")}
  })
 }
}
'''
    # This string is Go source; preserve real shell newlines in its Go literals.
    go_source = go_source.replace('\\\\n', '\\n')
    test = temp / 'actual_driver_test.go'
    test.write_text(go_source)
    env = os.environ.copy()
    env.update({'PATH': str(Path(GO).parent)+':'+env.get('PATH',''), 'AGENTEAM_GO':GO,
                'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOPROXY':'off','GOSUMDB':'off','GOTELEMETRY':'off',
                'GOFLAGS':'-mod=readonly -p=1','GOMAXPROCS':'2',
                'GOCACHE':str(ROOT/'output/ai/runner-control/gocache'),
                'GOMODCACHE':str(ROOT/'output/ai/runner-control/go-mod'),
                'GOTMPDIR':str(ROOT/'output/ai/runner-control/tmp'),
                'XDG_CONFIG_HOME':str(ROOT/'output/ai/runner-control/go-config')})
    result = subprocess.run([GO,'test','-race','-count=1','-timeout=10s','-v',str(test)],cwd=ROOT,env=env,timeout=45)
    check('actual Go guard and parent discovery controls', result.returncode == 0)
print('PASS pending selector controls:', len(checks), 'plus original closure controls and actual Go guard/discovery; no fixture/socket')
