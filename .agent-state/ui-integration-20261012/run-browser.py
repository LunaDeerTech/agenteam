#!/usr/bin/env python3
"""Run the two reviewed UI cases through the repository's real fixture chain.

No new services, full-source hashing, or global network/cleanup policy. Original
Go and Playwright budgets stay in their fixtures. Record actual child waits and
check only resources this invocation owns. Save under .agent-state before use.
"""
import argparse
import ctypes
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

SELECTOR = '^(TestTaskSearchWeb|TestProjectVariablesWeb)$'
EXPECTED = {'TestTaskSearchWeb', 'TestTaskSearchWeb/server-search-beyond-first-page',
            'TestProjectVariablesWeb', 'TestProjectVariablesWeb/create-update-clear-and-delete'}

def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')

def children():
    # Same parent-map walk as the existing PG supervisor. Some kernels omit
    # /proc/<pid>/task/<tid>/children; /proc/*/stat remains the supported source.
    parents = {}
    for stat in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = stat.read_text().rsplit(')', 1)[1].split()
            parents[int(stat.parent.name)] = int(fields[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    result = {os.getpid()}
    changed = True
    while changed:
        added = {pid for pid, parent in parents.items() if parent in result} - result
        changed = bool(added)
        result |= added
    return sorted(result - {os.getpid()})

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--binary', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--runtime', type=Path, required=True)
    p.add_argument('--dist', type=Path, required=True)
    p.add_argument('--input-hash', required=True)
    p.add_argument('--pg-current-only', action='store_true',
                   help='explicitly omit the unused PG16 negative-control fixture')
    a = p.parse_args()
    for path in (a.root, a.binary, a.output, a.runtime, a.dist):
        assert path.is_absolute()
    assert a.binary.is_file() and (a.dist / 'index.html').is_file()
    assert re.fullmatch('[0-9a-f]{64}', a.input_hash)
    a.output.mkdir(mode=0o700)
    a.runtime.mkdir(mode=0o700)
    for name in ('task-search', 'variable-delete'):
        (a.output / name).mkdir(mode=0o700)
    if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0):
        raise OSError(ctypes.get_errno(), 'subreaper unavailable')
    env = dict(os.environ, TMPDIR=str(a.runtime), AGENTEAM_AUTH_WEB_RUNTIME=str(a.runtime),
               AGENTEAM_FIXTURE_TEST_BINARY=str(a.binary),
               AGENTEAM_FIXTURE_TEST_CWD=str(a.root / 'tests/projectvariable'),
               AGENTEAM_FIXTURE_OWNED_RECORD=str(a.output / 'owned-resources.json'))
    env.pop('AGENTEAM_FIXTURE_PG_CURRENT_ONLY', None)
    if a.pg_current_only:
        env['AGENTEAM_FIXTURE_PG_CURRENT_ONLY'] = '1'
    for prefix, name in (('AGENTEAM_TASK_SEARCH_WEB', 'task-search'),
                         ('AGENTEAM_PROJECT_VARIABLES_WEB', 'variable-delete')):
        env.update({prefix+'_DIST': str(a.dist), prefix+'_EVIDENCE': str(a.output/name),
                    prefix+'_INPUT_HASH': a.input_hash})
    command = ['sh', 'scripts/test-objects.sh', '-run', SELECTOR]
    save(a.output/'invocation.json', {'command':command,'cwd':str(a.root),
         'input_hash':a.input_hash,'binary':str(a.binary),'dist':str(a.dist),
         'runtime':str(a.runtime),'supervisor_pid':os.getpid(),
         'pg_current_only':a.pg_current_only})
    began = time.monotonic()
    failed = False
    adopted = []
    child = None
    def stop(signum, frame):
        nonlocal failed
        failed = True
        if child is not None and child.poll() is None:
            child.terminate()  # Original shell forwards TERM and waits fixture cleanup.
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, stop)
    with (a.output/'browser.log').open('x', buffering=1) as log:
        child = subprocess.Popen(command, cwd=a.root, env=env, stdout=log,
                                 stderr=subprocess.STDOUT, start_new_session=True)
        print(json.dumps({'fixture_pid':child.pid,'supervisor_pid':os.getpid()}), flush=True)
        try:
            actual = child.wait(timeout=540)
        except subprocess.TimeoutExpired:
            failed = True
            child.terminate()
            try:
                actual = child.wait(timeout=60)
            except subprocess.TimeoutExpired:
                child.kill()
                actual = child.wait(timeout=5)
        log.write(f'UI actual_fixture_wait pid={child.pid} code={actual}\n')
        # Chromium may orphan already-exiting descendants to the owning subreaper.
        deadline = time.monotonic()+5
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                break
            if pid:
                adopted.append({'pid':pid,'actual_wait_status':status})
                log.write(f'UI adopted_actual_wait pid={pid} status={status}\n')
                failed |= status != 0
            elif time.monotonic() < deadline:
                time.sleep(.02)
            else:
                failed = True
                # A leak is FAIL; terminate only children actually owned by us.
                for pid in children():
                    try: os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError: pass
                deadline = time.monotonic()+3
                while children() and time.monotonic() < deadline:
                    try:
                        pid, status = os.waitpid(-1, os.WNOHANG)
                    except ChildProcessError:
                        break
                    if pid:
                        adopted.append({'pid':pid,'actual_wait_status':status})
                        log.write(f'UI adopted_actual_wait pid={pid} status={status}\n')
                    else: time.sleep(.02)
                break
        log.flush()
    raw = (a.output/'browser.log').read_text()
    runs = re.findall(r'^=== RUN\s+(\S+)\s*$', raw, re.M)
    results = re.findall(r'^\s*--- (PASS|FAIL|SKIP): (\S+) \(', raw, re.M)
    exact = (len(runs)==4 and set(runs)==EXPECTED and len(results)==4
             and all(s=='PASS' for s,n in results) and {n for s,n in results}==EXPECTED
             and not re.search(r'^FAIL(?:\s|$)|^panic:|WARNING: DATA RACE',raw,re.M))
    go_waits = re.findall(r'^D03 explicit test actual_wait pid=(\d+) code=(-?\d+) selector=(.*)$',raw,re.M)
    node_waits = re.findall(r'Node actual_wait pid=(\d+) success=(true|false)',raw)
    exact &= (len(go_waits)==1 and go_waits[0][1:]==('0',SELECTOR)
              and len(node_waits)==2 and all(ok=='true' for pid,ok in node_waits))
    resources=[]
    record=a.output/'owned-resources.json'
    if record.is_file():
        owned=json.loads(record.read_text())
        for r in owned['resources']:
            assert r['kind'] in ('container','network') and re.fullmatch('[0-9a-f]{64}',r['id'])
            check=subprocess.run(['docker',r['kind'],'inspect',r['id']],capture_output=True,text=True,timeout=15)
            # Don't treat a daemon/permission error as absence.
            absent=check.returncode==1 and ('No such '+r['kind'] in check.stderr or 'not found' in check.stderr)
            resources.append(dict(r,absent=absent,inspect_exit=check.returncode))
        directories_absent=all(not Path(d).exists() for d in owned['directories'])
    else:
        directories_absent=False
    leftovers=sorted(str(p.relative_to(a.runtime)) for p in a.runtime.iterdir())
    expected_containers = 3 if a.pg_current_only else 4
    resources_match = (sum(r['kind']=='container' for r in resources)==expected_containers
                       and sum(r['kind']=='network' for r in resources)==3)
    okay=(not failed and actual==0 and exact and resources_match
          and all(r['absent'] for r in resources) and directories_absent
          and not leftovers and not children())
    if not leftovers: a.runtime.rmdir()
    result={'fixture_pid':child.pid,'fixture_actual_wait_exit':actual,
            'elapsed_seconds':round(time.monotonic()-began,3),'exact_tests':bool(exact),
            'go_actual_waits':go_waits,'node_actual_waits':node_waits,'adopted_actual_waits':adopted,
            'resources':resources,'private_directories_absent':directories_absent,
            'runtime_leftovers':leftovers,'remaining_children':children(),'exit_code':int(not okay)}
    save(a.output/'result.json',result)
    print(json.dumps(result),flush=True)
    return int(not okay)

if __name__=='__main__':
    raise SystemExit(main())
