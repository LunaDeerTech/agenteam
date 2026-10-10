import ctypes
import datetime
import hashlib
import importlib.util
import json
import os
import re
import shlex
from pathlib import Path
import signal
import stat
import subprocess
import sys
import time

ROOT = Path('/workspace/agenteam-agent-system-integration')
BASE = ROOT / 'output/ai/agent-system-integration'
OUT = BASE / 'scheduler-busy-compile-01'
CANDIDATE = BASE / 'scheduler-busy-race-01.test'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
CACHE = Path('/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build')
MODS = Path('/workspace/shared/agenteam-deps/go-mod')
SUPERVISOR = Path('/workspace/agenteam-agent-configuration-integration/.agent-state/task-planning-recovery/pg_only_supervisor.py')
SUPERVISOR_SHA = 'dfa4284e92f22c3b4349885b6ece0028404916087c5ee4797dd00efb15a89559'


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def save(name, value):
    (OUT / name).write_text(json.dumps(value, indent=2) + '\n')


def group_absent(pid):
    try:
        os.killpg(pid, 0)
        return False
    except ProcessLookupError:
        return True
    except PermissionError:
        return False


if digest(SUPERVISOR) != SUPERVISOR_SHA:
    raise SystemExit('fixed metadata 01157924 supervisor changed; zero Go')
if OUT.exists() or CANDIDATE.exists():
    raise SystemExit('fresh output and candidate required; zero Go')
OUT.mkdir(mode=0o700)
runtime, private = OUT / 'runtime', OUT / 'private'
runtime.mkdir(mode=0o700)
private.mkdir(mode=0o700)
telemetry = private / 'xdg/go/telemetry'
telemetry.mkdir(mode=0o700, parents=True)
(telemetry / 'mode').write_text('off\n')
os.chmod(telemetry / 'mode', 0o600)
env = os.environ.copy()
for name in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD'):
    env.pop(name, None)
env.update({'AGENTEAM_GO': str(GO), 'GOTOOLCHAIN': 'local', 'GOENV': 'off',
            'GOTELEMETRY': 'off', 'GOPROXY': 'off', 'GOSUMDB': 'off',
            'GOWORK': 'off', 'GOFLAGS': '-mod=readonly', 'GOMODCACHE': str(MODS),
            'GOCACHE': str(CACHE), 'GOMAXPROCS': '2',
            'XDG_CONFIG_HOME': str(private / 'xdg'),
            'TMPDIR': str(runtime), 'GOTMPDIR': str(runtime)})
if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
    raise SystemExit('subreaper setup failed; zero Go')

# Reuse the accepted supervisor's actual implementation; do not treat a
# missing /proc/PID/task/PID/children virtual file as an empty child set.
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('metadata_compile_supervisor', SUPERVISOR)
supervisor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(supervisor)

space = os.statvfs(ROOT)
fresh = space.f_bavail * space.f_frsize
result = {'source': '232c7af5', 'started_utc': now(), 'outer_pid': os.getpid(),
          'fresh_available_bytes': fresh, 'commands': [], 'result': 'FAIL'}
print(json.dumps({'stage': 'preflight', **{k: result[k] for k in
                 ('started_utc', 'outer_pid', 'fresh_available_bytes', 'source')}}), flush=True)
MODULE = 'github.com/LunaDeerTech/agenteam/'


def inputs():
    # Freeze repository Go/import/embed inputs only. Runtime entry/README
    # changes are not compiler inputs and are bound by the later actual run.
    files = {ROOT / 'go.mod', ROOT / 'go.sum', GO}
    pending, seen = ['tests/projectvariable'], set()
    while pending:
        relative = pending.pop()
        if relative in seen:
            continue
        seen.add(relative)
        directory = ROOT / relative
        if not directory.is_dir() or directory.is_symlink():
            raise RuntimeError('missing or symlink imported package')
        sources = [p for p in directory.glob('*.go')
                   if relative == 'tests/projectvariable' or not p.name.endswith('_test.go')]
        if not sources:
            raise RuntimeError('empty imported package')
        for path in sources:
            files.add(path)
            source = path.read_text()
            imports = re.findall(r'^import\s+(?:[\w.]+\s+)?"([^"\n]+)"', source, re.M)
            for block in re.findall(r'^import\s*\((.*?)^\)', source, re.M | re.S):
                imports.extend(re.findall(r'"([^"\n]+)"', block))
            pending.extend(name[len(MODULE):] for name in imports if name.startswith(MODULE))
            for line in re.findall(r'^//go:embed[ \t]+(.+)$', source, re.M):
                for pattern in shlex.split(line):
                    pattern = pattern.removeprefix('all:')
                    if pattern.startswith('/') or '..' in Path(pattern).parts:
                        raise RuntimeError('unsafe embed input')
                    matches = list(directory.glob(pattern))
                    if not matches:
                        raise RuntimeError('missing embedded input')
                    for match in matches:
                        if match.is_dir():
                            files.update(p for p in match.rglob('*') if p.is_file() or p.is_symlink())
                        else:
                            files.add(match)
    output = {}
    for path in sorted(files):
        if not stat.S_ISREG(path.lstat().st_mode) or path.is_symlink():
            raise RuntimeError('nonregular source input')
        output[str(path)] = digest(path)
    return output


def method_inputs():
    return {str(p): digest(p) for p in (Path(__file__).resolve(), Path(sys.executable).resolve(), SUPERVISOR)}


before = inputs()
method_before = method_inputs()
save('inputs-before.json', before)
result['input_count'] = len(before)
result['method_inputs_before'] = method_before


def run(name, argv, timeout):
    item = {'name': name, 'argv': argv}
    result['commands'].append(item)
    start = time.monotonic()
    with (OUT / (name + '.log')).open('wb') as log:
        child = subprocess.Popen(argv, cwd=ROOT, env=env, stdout=log,
                                 stderr=subprocess.STDOUT, start_new_session=True)
        item['pid'] = child.pid
        print(json.dumps({'stage': name, 'utc': now(), 'pid': child.pid,
                          'outer_pid': os.getpid(), 'source': result['source']}), flush=True)
        try:
            code = child.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            item['timeout'] = True
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                code = child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                code = child.wait()
    item.update(actual_wait=code, seconds=round(time.monotonic() - start, 3))
    item['group_absent_first'] = group_absent(child.pid)
    item['descendants_first'] = sorted(supervisor.descendants(os.getpid()))
    adopted = []
    for _ in item['descendants_first']:
        try:
            pid, status = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:
            break
        if pid == 0:
            break
        adopted.append({'pid': pid, 'actual_wait': os.waitstatus_to_exitcode(status)})
    item['adopted_waits'] = adopted
    survivors = sorted(supervisor.descendants(os.getpid()))
    item['survivors_before_cleanup'] = survivors
    if not item['group_absent_first']:
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    for pid in survivors:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    if survivors:
        while True:
            try:
                pid, status = os.waitpid(-1, 0)
                adopted.append({'pid': pid, 'actual_wait': os.waitstatus_to_exitcode(status)})
            except ChildProcessError:
                break
    item['group_absent_second'] = group_absent(child.pid)
    item['runtime_samples'] = [sorted(p.name for p in runtime.iterdir())]
    item['descendants_second'] = sorted(supervisor.descendants(os.getpid()))
    time.sleep(0.2)
    item['runtime_samples'].append(sorted(p.name for p in runtime.iterdir()))
    item['descendants_final'] = sorted(supervisor.descendants(os.getpid()))
    okay = (code == 0 and not item.get('timeout', False)
            and item['group_absent_first'] and item['group_absent_second']
            and not survivors and all(v['actual_wait'] == 0 for v in adopted)
            and not item['descendants_second'] and not item['descendants_final']
            and item['runtime_samples'] == [[], []])
    if name == 'list':
        item['exact_one_top'] = (OUT / 'list.log').read_text().splitlines() == ['TestSchedulerBusyCompensation']
        okay = okay and item['exact_one_top']
    print(json.dumps({'stage': name + '-wait', 'actual_wait': code,
                      'seconds': item['seconds'], 'tails_closed': okay}), flush=True)
    save('result.json', result)
    return okay


okay = False
try:
    if fresh < 5 * 1024**3:
        result['preflight_failure'] = 'fresh_available_below_5GiB'
    elif not GO.is_file() or not CACHE.is_dir() or not MODS.is_dir():
        result['preflight_failure'] = 'fixed_toolchain_or_cache_missing'
    else:
        okay = run('compile', [str(GO), 'test', '-p=2', '-race', '-tags=integration',
                              '-c', '-o', str(CANDIDATE), './tests/projectvariable'], 300)
        if okay:
            os.chmod(CANDIDATE, 0o700)
            okay = run('list', [str(CANDIDATE), '-test.list', '^TestSchedulerBusyCompensation$'], 30)
    after = inputs()
    save('inputs-after.json', after)
    result['inputs_unchanged'] = before == after
    result['method_inputs_after'] = method_inputs()
    result['method_inputs_unchanged'] = method_before == result['method_inputs_after']
    okay = okay and result['inputs_unchanged'] and result['method_inputs_unchanged']
    if CANDIDATE.exists():
        artifact = CANDIDATE.lstat()
        result['candidate'] = {'path': str(CANDIDATE), 'bytes': artifact.st_size,
                               'sha256': digest(CANDIDATE), 'regular': stat.S_ISREG(artifact.st_mode),
                               'nlink': artifact.st_nlink, 'mode': oct(stat.S_IMODE(artifact.st_mode))}
        okay = okay and stat.S_ISREG(artifact.st_mode) and artifact.st_nlink == 1
    result['result'] = 'PASS' if okay else 'FAIL'
except BaseException as exc:
    result['exception_kind'] = type(exc).__name__
    raise
finally:
    result['finished_utc'] = now()
    save('result.json', result)
    print(json.dumps({'stage': 'outer-complete', 'result': result['result'],
                      'utc': result['finished_utc'], 'outer_pid': os.getpid(),
                      'inputs_unchanged': result.get('inputs_unchanged'),
                      'result_path': str(OUT / 'result.json')}), flush=True)
sys.exit(0 if okay else 1)
