#!/usr/bin/env python3
"""Run the unchanged complete Go check script in an explicitly granted window.

The first invocation used an inline wrapper; this is its recoverable successor,
not a byte-for-byte copy. Reap and TCP decisions execute the original supervisor
source fragments. An earlier metadata-only TypeError remains a recorded FAIL.
"""
import argparse
import builtins
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[2]
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
PYTHON = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3')
NODE = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node')
SCHEMA_SOURCES = {
    'AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON': 'internal/central/audit/http/project_wire_test.go',
    'AGENTEAM_PROJECT_MODEL_SCHEMA_PYTHON': 'internal/central/model/http_project_wire_test.go',
    'AGENTEAM_PROJECT_CREDENTIAL_SCHEMA_PYTHON': 'internal/central/model/http_project_credentials_wire_test.go',
    'AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON': 'internal/central/model/http_project_configuration_wire_test.go',
    'AGENTEAM_PROJECT_READ_SCHEMA_PYTHON': 'internal/central/project/http/wire_test.go',
    'AGENTEAM_PROJECT_VARIABLE_SCHEMA_PYTHON': 'internal/central/projectvariable/http/wire_test.go',
    'AGENTEAM_WORK_HTTP_SCHEMA_PYTHON': 'internal/central/work/http/wire_test.go',
    'AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON': 'internal/central/skill/http/schema_test.go',
    'AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON': 'internal/central/knowledge/http/schema_test.go',
    'AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON': 'internal/central/knowledge/contenthttp/schema_test.go',
    'AGENTEAM_USAGE_SCHEMA_PYTHON': 'internal/central/usage/http/wire_test.go',
}
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
DIAGNOSTICS = ROOT / '.agent-state/skills-owner-http/tcp_diagnostics.py'


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def original_fragment(start, end):
    source = SUPERVISOR.read_text()
    if source.count(start) != 1 or source.count(end) != 1:
        raise ValueError('supervisor fragment anchors changed')
    fragment = source[source.index(start):source.index(end)]
    return ''.join(line[12:] if line.strip() else line for line in fragment.splitlines(True))


def reap_fragment():
    return original_fragment('            survivors = descendants(os.getpid())',
                             '            if args.root_chain and not (content_root')


def tcp_fragment():
    return original_fragment('            tail_deadline = time.monotonic() + 75',
                             '            if secret_owner:\n                try:\n                    same =')


def schema_environment():
    found = set()
    for directory in ('internal', 'tests'):
        for path in (ROOT / directory).rglob('*.go'):
            found.update(re.findall(r'os\.Getenv\("(AGENTEAM_[A-Z_]+_SCHEMA_PYTHON)"\)', path.read_text()))
    if found != set(SCHEMA_SOURCES):
        raise ValueError('schema Python source inventory changed')
    for variable, source in SCHEMA_SOURCES.items():
        if f'os.Getenv("{variable}")' not in (ROOT / source).read_text():
            raise ValueError('schema source mapping changed: ' + variable)
    if 'os.Getenv("AGENTEAM_USAGE_SCHEMA_NODE")' not in (ROOT / SCHEMA_SOURCES['AGENTEAM_USAGE_SCHEMA_PYTHON']).read_text():
        raise ValueError('Usage Node source mapping changed')
    for executable in (GO, PYTHON, NODE):
        if not executable.is_absolute() or not os.access(executable, os.X_OK):
            raise ValueError('required absolute executable unavailable')
    return dict({key: str(PYTHON) for key in SCHEMA_SOURCES}, AGENTEAM_USAGE_SCHEMA_NODE=str(NODE))


def final_record(record, code, started, script_code):
    return dict(record, terminal=code, script_exit=script_code,
                seconds=builtins.round(time.monotonic() - started, 3))


def remaining_script(source):
    ordinary_test = '"$AGENTEAM_GO" test ./...\n'
    if source.count(ordinary_test) != 1:
        raise ValueError('ordinary test anchor changed')
    return source.replace(ordinary_test, '', 1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--source', required=True)
    parser.add_argument('--remaining', action='store_true',
                        help='run the original remaining stages after separately accepted ordinary tests')
    args = parser.parse_args()
    os.chdir(ROOT)
    command = ['sh', 'scripts/check-go.sh']
    if args.remaining:
        command = ['sh', '-c', remaining_script((ROOT / 'scripts/check-go.sh').read_text()),
                   'scripts/check-go.sh']
    output = args.output.resolve()
    if not output.is_relative_to(ROOT / 'output/ai/owner-feature-integration'):
        raise ValueError('output must be task-owned')
    schema = schema_environment()
    free = shutil.disk_usage(ROOT).free
    if free < 5 * 1024 ** 3:
        raise ValueError('fresh disk below 5 GiB')
    output.mkdir(parents=True, exist_ok=False)
    runtime, config = output / 'runtime', output / 'config'
    runtime.mkdir(mode=0o700)
    telemetry = config / 'go/telemetry/mode'
    telemetry.parent.mkdir(parents=True)
    telemetry.write_text('off\n')
    telemetry.chmod(0o600)
    env = dict(os.environ)
    removed = []
    for key in list(env):
        if (key.startswith('AGENTEAM_') and ('FIXTURE' in key or key.endswith('_NATIVE'))) or key in (
                'TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD',
                'AGENTEAM_USAGE_SCHEMA_EVIDENCE', 'AGENTEAM_TREE_HTTP_SCHEMA_VECTORS'):
            removed.append(key)
            del env[key]
    env.update(schema, AGENTEAM_GO=str(GO), GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off',
               GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=2', GOMAXPROCS='2',
               GOMODCACHE='/workspace/shared/agenteam-deps/go-mod',
               GOCACHE=str(ROOT / 'output/ai/skills-http-integration/go-build'),
               XDG_CONFIG_HOME=str(config), TMPDIR=str(runtime), GOTMPDIR=str(runtime),
               PYTHONDONTWRITEBYTECODE='1')
    # Identity-only preflights do not open sockets or download dependencies.
    python_info = subprocess.check_output([str(PYTHON), '-c',
        'import json,sys,importlib.metadata; import jsonschema,referencing; '
        'print(json.dumps(dict(executable=sys.executable,python=sys.version.split()[0],'
        'jsonschema=importlib.metadata.version("jsonschema"),referencing=importlib.metadata.version("referencing"))))'],
        env=env, text=True).strip()
    node_info = subprocess.check_output([str(NODE), '-e',
        'new RegExp("^[a-z]+$", "u"); console.log(JSON.stringify({executable:process.execPath,node:process.version}))'],
        env=env, text=True).strip()
    supervisor = load(SUPERVISOR, 'final_check_supervisor')
    diagnostic_module = load(DIAGNOSTICS, 'final_check_diagnostics')
    if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), 'subreaper setup failed')
    started = time.monotonic()
    diagnostics = diagnostic_module.TCPDiagnostics(output, supervisor.descendants)
    sample = diagnostic_module.observe_tcp(supervisor.tcp, diagnostics)
    baseline = sample()
    record = dict(source=args.source, command=command, remaining=args.remaining, outer_pid=os.getpid(),
                  utc_start=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), fresh_free_bytes=free,
                  schema_environment=schema, python=json.loads(python_info), node=json.loads(node_info),
                  removed_environment_names=sorted(removed),
                  supervisor_sha256=hashlib.sha256(SUPERVISOR.read_bytes()).hexdigest())
    with (output / 'check-go.log').open('x', buffering=1) as log:
        child = subprocess.Popen(command, cwd=ROOT, env=env,
                                 stdout=log, stderr=subprocess.STDOUT)
        record['script_pid'] = child.pid
        (output / 'result.json').write_text(json.dumps(record, indent=2) + '\n')
        print(json.dumps(record), flush=True)
        code = child.wait()
        log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} actual=True code={code}\n')
        scope = dict(supervisor.__dict__, os=os, signal=signal, time=time, code=code,
                     child=child, log=log, args=SimpleNamespace(root_chain=True, run='check-go.sh'),
                     nonroot_reap_deadline=None, term_grace=60)
        exec(compile(reap_fragment(), str(SUPERVISOR), 'exec'), scope)
        code = scope['code']
        for observation in (1, 2):
            empty_runtime = not any(runtime.iterdir())
            log.write(f'FINAL_CHECK private_observation={observation} runtime_empty={empty_runtime}\n')
            if not empty_runtime:
                code = 1
        scope.update(code=code, tcp=sample, baseline=baseline)
        exec(compile(tcp_fragment(), str(SUPERVISOR), 'exec'), scope)
        code = scope['code']
        diagnostics.finish()
        record = final_record(record, code, started, child.returncode)
        (output / 'result.json').write_text(json.dumps(record, indent=2) + '\n')
        log.write('FINAL_CHECK ' + json.dumps(record) + '\n')
    print(json.dumps(record), flush=True)
    return code


if __name__ == '__main__':
    sys.exit(main())
