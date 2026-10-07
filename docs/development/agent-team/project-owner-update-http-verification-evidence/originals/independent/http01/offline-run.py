#!/usr/bin/env python3
"""Own-scratch exact offline commands, 45s execution and actual owned joins."""
from pathlib import Path
import ctypes
import hashlib
import json
import os
import signal
import subprocess
import sys
import time

SUFFIX = {'.go','.s','.S','.c','.h','.cc','.cpp','.cxx','.m','.mm','.f','.F','.for','.f90','.swig','.swigcxx','.syso'}
REPO = Path('/workspace/agenteam')

def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def load(row):
    if sha(row['path']) != row['sha256']:
        raise RuntimeError('bound input mismatch: '+row['path'])
    return json.loads(Path(row['path']).read_text())

def write(path, value):
    path.write_text(json.dumps(value, indent=2)+'\n')

def inputs(plan, plan_path, plan_sha):
    delta = load(plan['closure'])
    parent = load(delta['parent_input'])
    candidate = load(delta['candidate'])
    overlay = load(delta['overlay'])['Replace']
    expected = dict(parent['files'])
    # Bind the frozen effective sources, never active originals hidden by overlay.
    for virtual, backing in overlay.items():
        expected.pop(virtual, None)
        if backing:
            expected[backing] = delta['files'][backing]
    expected.update(delta['files'])
    expected.update(plan['tools'])
    expected.update({str(plan_path):plan_sha, str(Path(__file__).resolve()):plan['runner_sha256'],
                     plan['closure']['path']:plan['closure']['sha256'],
                     delta['parent_input']['path']:delta['parent_input']['sha256'],
                     delta['candidate']['path']:delta['candidate']['sha256']})
    actual = {path:sha(path) for path in expected}
    if actual != expected:
        raise RuntimeError('effective input bytes changed')
    for directory,row in parent['package_file_sets'].items():
        names = {p.name for p in Path(directory).iterdir() if p.is_file() and (p.suffix in SUFFIX or row['embed_directory'])}
        for virtual,backing in overlay.items():
            path = Path(virtual)
            if str(path.parent) != directory:
                continue
            if not backing:
                names.discard(path.name)
            elif path.suffix in SUFFIX or row['embed_directory']:
                names.add(path.name)
        wanted = set(row['names']) | set(delta['package_file_additions'].get(directory, []))
        if names != wanted:
            raise RuntimeError('effective package file set changed: '+directory)
    if any(sha(overlay.get(str(REPO/path),str(REPO/path))) != wanted
           for path,wanted in candidate['source_files'].items()):
        raise RuntimeError('effective frozen candidate drift')
    return {'accepted':True,'files_count':len(actual),
            'digest':hashlib.sha256(json.dumps(actual,sort_keys=True,separators=(',',':')).encode()).hexdigest(),
            'candidate':delta['candidate'],'overlay':delta['overlay'],'parent_input':delta['parent_input']}

def processes():
    result = {}
    for path in Path('/proc').iterdir():
        if not path.name.isdigit():
            continue
        try:
            value = (path/'stat').read_text().rsplit(') ',1)[1].split()
            result[int(path.name)] = {'pid':int(path.name),'starttime':value[19],
                                     'ppid':int(value[1]),'pgrp':int(value[2]),'state':value[0]}
        except (OSError,ValueError,IndexError):
            continue
    return result

def observe(main,known):
    now = processes()
    owned = {main} | {pid for pid,row in now.items() if row['ppid']==os.getpid()}
    while True:
        expanded = owned | {pid for pid,row in now.items() if row['ppid'] in owned}
        if expanded == owned:
            break
        owned = expanded
    for pid in owned:
        if pid in now:
            known[str(pid)+':'+now[pid]['starttime']] = now[pid]
    return now

def live(main,known):
    now = observe(main,known)
    return [row for row in known.values() if row['pid'] in now and now[row['pid']]['starttime']==row['starttime']]

def signal_owned(rows,sig,actions):
    for row in rows:
        current = processes().get(row['pid'])
        if current and current['starttime']==row['starttime'] and current['state']!='Z':
            try:
                os.kill(row['pid'],sig)
            except ProcessLookupError:
                continue
            actions.append({'pid':row['pid'],'starttime':row['starttime'],'signal':sig.name})

def main():
    plan_path = Path(sys.argv[1]).resolve()
    plan_sha, label = sys.argv[2:4]
    plan = load({'path':str(plan_path),'sha256':plan_sha})
    if sha(__file__) != plan['runner_sha256']:
        raise RuntimeError('runner differs from frozen plan')
    command = next(row for row in plan['commands'] if row['label']==label)
    run = Path(plan['output_root'])/label
    run.mkdir(parents=True,exist_ok=False)
    before = inputs(plan,plan_path,plan_sha)
    write(run/'input-before.json',before)
    Path(plan['environment']['TMPDIR']).mkdir(parents=True,exist_ok=True)
    if ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)!=0:
        raise RuntimeError('subreaper unavailable')
    tool_before = sha(command['argv'][0])
    started = time.monotonic()
    known, actions, waits = {}, [], []
    record = {'argv':command['argv'],'env':plan['environment'],'cwd':plan['cwd'],
              'budget_seconds':45,'driver_sha256':sha(__file__),'plan_sha256':plan_sha,
              'tool_before_sha256':tool_before,'started_unix':time.time(),'subreaper':True}
    write(run/'command.json',record)
    with (run/'stdout.log').open('xb') as out,(run/'stderr.log').open('xb') as err:
        child = subprocess.Popen(command['argv'],cwd=plan['cwd'],env=plan['environment'],
                                 stdout=out,stderr=err,start_new_session=True)
        observe(child.pid,known)
        record['main_pid']=child.pid
        write(run/'command.json',record)
        while child.poll() is None:
            observe(child.pid,known)
            if time.monotonic()-started>=45:
                signal_owned(live(child.pid,known),signal.SIGTERM,actions)
                try:
                    child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    signal_owned(live(child.pid,known),signal.SIGKILL,actions)
                break
            time.sleep(.025)
        code = child.wait()
    terminal = time.monotonic()
    tail_started, sent = time.monotonic(),set()
    while True:
        observe(child.pid,known)
        try:
            while True:
                pid,status = os.waitpid(-1,os.WNOHANG)
                if not pid:
                    break
                identity = next((row for row in known.values() if row['pid']==pid),None)
                waits.append({'pid':pid,'identity':identity,'status':status,
                              'actual_exit':os.waitstatus_to_exitcode(status),'actual_wait':True})
        except ChildProcessError:
            pass
        remaining = live(child.pid,known)
        if not remaining:
            break
        sig = signal.SIGKILL if time.monotonic()-tail_started>=3 else signal.SIGTERM
        fresh = [row for row in remaining if (row['pid'],row['starttime'],sig) not in sent]
        signal_owned(fresh,sig,actions)
        sent.update((row['pid'],row['starttime'],sig) for row in fresh)
        time.sleep(.025)
    cleanup=[]
    for i in range(2):
        cleanup.append({'time':time.time(),'remaining':live(child.pid,known)})
        if i==0:
            time.sleep(.1)
    try:
        after=inputs(plan,plan_path,plan_sha)
    except Exception as exc:
        after={'accepted':False,'error':str(exc)}
    write(run/'input-after.json',after)
    extra=True
    if 'expected_lines' in command:
        extra=sorted((run/'stdout.log').read_text().splitlines())==sorted(command['expected_lines'])
    if command.get('stdout_empty'):
        extra=(run/'stdout.log').stat().st_size==0
    record.update(actual_exit=code,actual_wait=True,execution_seconds=terminal-started,
                  total_seconds=time.monotonic()-started,observed_processes=list(known.values()),
                  adopted_waits=waits,cleanup_actions=actions,cleanup_twice=cleanup,
                  input_unchanged=before==after,tool_after_sha256=sha(command['argv'][0]),
                  stdout_sha256=sha(run/'stdout.log'),stderr_sha256=sha(run/'stderr.log'),success_extra=extra)
    record['passed']=code==0 and not actions and before==after and tool_before==record['tool_after_sha256'] and extra and all(not row['remaining'] for row in cleanup)
    if command.get('binary_output') and code==0:
        record['binary_sha256']=sha(command['binary_output'])
    write(run/'result.json',record)
    print(json.dumps({key:record[key] for key in ['actual_exit','actual_wait','execution_seconds','total_seconds','input_unchanged','passed']}),flush=True)
    return 0 if record['passed'] else 1

if __name__=='__main__':
    raise SystemExit(main())
