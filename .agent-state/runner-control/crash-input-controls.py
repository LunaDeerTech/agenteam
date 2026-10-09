#!/usr/bin/env python3
"""Offline input-closure controls; no driver, fixture or cmd is executed."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
HERE = ROOT / 'output/ai/runner-control/tmp/crash-input-review'
HERE.mkdir(exist_ok=True)
spec = importlib.util.spec_from_file_location('crash_inputs', Path(__file__).with_name('crash_inputs.py'))
subject = importlib.util.module_from_spec(spec)
spec.loader.exec_module(subject)
checks = []


def check(name, good):
    assert good, name
    checks.append(name)


def rejected(name, operation):
    try:
        operation()
    except (ValueError, OSError):
        check(name, True)
    else:
        check(name, False)


supervisor = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
before = subprocess.check_output(['git', 'show', 'f031b494:' + str(supervisor.relative_to(ROOT))], cwd=ROOT, text=True)
source = supervisor.read_text()
start = source.index('    crash_inputs = None\n')
end = source.index("    baseline_times = ", start)
reduced = source[:start] + source[end:]
budget = '''    if crash_inputs is not None:
        started = crash_started
        driver_timeout = max(0, driver_timeout - (time.monotonic() - crash_started))
'''
check('one original-budget debit', reduced.count(budget) == 1)
reduced = reduced.replace(budget, '')
new_tail = '''            if crash_inputs is not None:
                same = crash_module.unchanged(crash_inputs)
            else:
                same = all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest
                           for p, digest in inputs.items())
'''
old_tail = '''            same = all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest
                       for p, digest in inputs.items())
'''
check('one safe terminal comparison', reduced.count(new_tail) == 1)
reduced = reduced.replace(new_tail, old_tail)
check('all old supervisor bytes remain identical', reduced == before)
check('only non-root exact B selector enables closure', source.count("if not args.root_chain and args.run == '^TestRunnerControlProcessCrashRecovery$':") == 1)

with tempfile.TemporaryDirectory(dir=HERE) as tmp:
    root = Path(tmp)
    command = root / 'cmd/agenteam-runner'
    dependency = root / 'internal/example'
    command.mkdir(parents=True)
    dependency.mkdir(parents=True)
    (root / 'go.mod').write_text('module ' + subject.MODULE + '\n\ngo 1.27.1\n')
    (root / 'go.sum').write_text('')
    harness = root / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
    harness.parent.mkdir(parents=True)
    harness.write_text('# declared supervisor\n')
    builder = root / 'tests/runnercontrol/process_crash_linux_test.go'
    builder.parent.mkdir(parents=True)
    builder.write_text('// original build command\n')
    main = command / 'main.go'
    main.write_text('package main\nfunc main(){}\n')
    dep = dependency / 'value.go'
    dep.write_text('package example\n')
    ignored = dependency / 'other.go'
    ignored.write_text('//go:build windows\n\npackage example\n')
    embedded = dependency / 'assets'
    embedded.mkdir()
    asset = embedded / 'one.txt'
    asset.write_text('original input\n')
    (dependency.parent / 'value.go').write_text('package outside\n')
    driver, binary = root / 'driver', root / 'candidate'
    driver.write_bytes(b'original driver')
    binary.write_bytes(b'original candidate')
    module = {'Path': subject.MODULE, 'Main': True, 'Dir': str(root), 'GoMod': str(root / 'go.mod')}
    packages = [
        {'ImportPath': subject.COMMAND, 'Dir': str(command), 'Module': module, 'GoFiles': ['main.go'],
         'Imports': [subject.MODULE + '/internal/example']},
        {'ImportPath': subject.MODULE + '/internal/example', 'Dir': str(dependency), 'Module': module,
         'GoFiles': ['value.go'], 'IgnoredGoFiles': ['other.go'], 'EmbedFiles': ['assets/one.txt'],
         'EmbedPatterns': ['assets/*.txt'], 'Imports': []},
    ]
    freeze = lambda value=packages: subject.project(value, driver, binary, root)
    original = freeze()
    check('original local source/embed/module/build inputs', all(str(path) in original['files'] for path in
          (main, dep, ignored, asset, driver, binary, harness, builder, root / 'go.mod', root / 'go.sum')))
    check('unchanged original closure', subject.unchanged(original))
    with patch.object(subject.subprocess, 'run') as run:
        check('terminal comparison starts no child', subject.unchanged(original) and not run.called)
    for label, path in [('selected Go mutation', dep), ('ignored build-tag mutation', ignored),
                        ('embed mutation', asset), ('module mutation', root / 'go.mod'),
                        ('builder mutation', builder), ('candidate mutation', binary)]:
        raw = path.read_bytes()
        path.write_bytes(raw + b'changed')
        check(label, not subject.unchanged(original))
        path.write_bytes(raw)
    for label, path in [('added package Go source', dependency / 'added.go'),
                        ('added embed match', embedded / 'added.txt')]:
        path.write_bytes(b'new input')
        check(label, not subject.unchanged(original))
        path.unlink()
    raw = asset.read_bytes()
    asset.unlink()
    check('missing original embed file', not subject.unchanged(original))
    asset.write_bytes(raw)
    raw = dep.read_bytes()
    dep.unlink()
    dep.symlink_to(asset)
    check('changed source is a symlink', not subject.unchanged(original))
    dep.unlink()
    dep.write_bytes(raw)
    for label, change in [
        ('omitted local dependency package', lambda p: p.pop()),
        ('omitted selected source', lambda p: p[1].update(GoFiles=[])),
        ('omitted ignored source', lambda p: p[1].update(IgnoredGoFiles=[])),
        ('omitted embed list', lambda p: p[1].update(EmbedFiles=[])),
        ('foreign local module', lambda p: p[1].update(Module={**module, 'Dir': '/foreign'})),
        ('escaped embed', lambda p: p[1].update(EmbedFiles=['../value.go'])),
        ('new native local source', lambda p: p[1].update(CgoFiles=['cgo.go'])),
        ('duplicate package identity', lambda p: p.append(p[0])),
    ]:
        value = copy.deepcopy(packages)
        change(value)
        rejected(label, lambda: freeze(value))
    rejected('missing original command', lambda: freeze(packages[1:]))
    rejected('Go metadata reports a dependency error', lambda: subject.decode_packages('{"DepsErrors":[{"Err":"private"}]}'))
    check('restored closure after mutations', subject.unchanged(original))

base = ROOT / 'output/ai/runner-control'
environment = dict(os.environ, PATH=str(subject.GO.parent) + ':' + os.environ['PATH'],
                   AGENTEAM_GO=str(subject.GO), GOTOOLCHAIN='local', GOENV='off', GOWORK='off',
                   GOPROXY='off', GOSUMDB='off', GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=1',
                   GOMAXPROCS='2', GOCACHE=str(base / 'gocache'), GOMODCACHE=str(base / 'go-mod'),
                   GOTMPDIR=str(base / 'tmp'), XDG_CONFIG_HOME=str(base / 'go-config'))
with patch.dict(os.environ, environment, clear=True):
    actual = subject.capture(base / 'pg-only-driver', base / 'runnercontrol-process-crash-race-2.test', subject.time.monotonic() + 30)
    check('actual fixed Go dependency process and real files', subject.unchanged(actual) and len(actual['directories']) == 8)
    with patch.object(subject.subprocess, 'run') as run:
        rejected('expired original budget starts no metadata child', lambda: subject.capture(driver, binary, subject.time.monotonic() - 1))
        check('no child on expired budget', not run.called)
    with patch.dict(os.environ, {'GOFLAGS': '-toolexec=unreviewed'}):
        rejected('different build environment rejected', lambda: subject.capture(driver, binary, subject.time.monotonic() + 1))
print(json.dumps({'passed': len(checks), 'controls': checks, 'local_packages': len(actual['directories']),
                  'frozen_files': len(actual['files']), 'actual_cmd_executed': False, 'resources_started': False}))
