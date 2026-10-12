#!/usr/bin/python3
"""Explicit basic/compile windows and offline native preparation. No auto retry."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
OUTPUT = ROOT / 'output/ai/runner-write-file-wss'
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
CONTROL = './internal/runner/control'
TESTS = {
    './internal/runnerprotocol': (
        'TestWriteFilePayloadStrictAndCanonicalBase64',
        'TestWriteFileBoundsAndWholeMessage',
        'TestWriteFileCommittedResultAndTerminalVariants',
        'TestWriteFileDefaultProjectionRedacts'),
    './internal/runner/writefile': (
        'TestHandlerRejectsBeforeWorkspaceAdmission',
        'TestHandlerKeepsPublicationAndCompletionFacts',
        'TestHandlerCancellationWaitsForActualWriteAndClose',
        'TestHandlerRealWorkspaceWriteAndRead'),
    CONTROL: (
        'TestWriteFileExplicitClientKeepsDefaultUnbound',
        'TestWriteFileControlSessionUsesRealHandler',
        'TestReadFileExplicitClientKeepsDefaultUnbound',
        'TestDirectoryControlExplicitClientsKeepExactCapabilities'),
}
TOP = 'TestWriteFileWSS'
SELECTOR = '^TestWriteFileWSS$/^committed-write-and-overwrite-conflict$'
ENV = dict(GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOPROXY='off',
           GOSUMDB='off', GOTELEMETRY='off', GOFLAGS='-mod=readonly -p=1',
           GOMAXPROCS='1', CGO_ENABLED='1',
           GOCACHE='/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build',
           GOMODCACHE='/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod')
FIELDS = ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles',
          'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles', 'EmbedFiles',
          'TestGoFiles', 'XTestGoFiles', 'TestEmbedFiles', 'XTestEmbedFiles')


def sha(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def save(out, name, value):
    with (out / name).open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def read(out, name):
    return json.loads((out / name).read_text())


def snapshot(paths):
    return {str(p): sha(p) for p in sorted(paths)}


def run(out, stage, command, env):
    command = [str(p) for p in command]
    save(out, stage + '-invocation.json', {'command': command, 'cwd': str(ROOT)})
    started = time.monotonic()
    with (out / (stage + '.log')).open('xb') as log:
        child = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
        save(out, stage + '-process.json', {'pid': child.pid, 'wrapper_pid': os.getpid()})
        print(json.dumps({'stage': stage, 'pid': child.pid}), flush=True)
        code = child.wait()
        log.flush()
        os.fsync(log.fileno())
    result = {'pid': child.pid, 'actual_wait_exit': code,
              'elapsed_seconds': round(time.monotonic() - started, 3)}
    save(out, stage + '-result.json', result)
    print(json.dumps({'stage': stage, **result}), flush=True)
    return code


def rows(path):
    text, decoder, offset = path.read_text(), json.JSONDecoder(), 0
    result = []
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        value, offset = decoder.raw_decode(text, offset)
        result.append(value)
    return result


def discover(out, when, packages, env, plain):
    imports = {'github.com/LunaDeerTech/agenteam/' + p[2:] for p in packages}
    paths = {ROOT / 'go.mod', ROOT / 'go.sum', GO,
             ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'}
    paths.update(HERE / name for name in ('run-checks.py', 'native.py', 'record-native.py', 'profile-controls.py'))
    generated, selected = set(), set()
    modes = [('race', ['-race'])] + ([('plain', [])] if plain else [])
    for mode, flags in modes:
        label = 'deps-' + when + '-' + mode
        code = run(out, label, [GO, 'list', *flags, '-deps', '-test', '-json', *packages], env)
        if code:
            return code, paths, selected, generated
        for row in rows(out / (label + '.log')):
            if row.get('Name') == 'main' and row['ImportPath'].endswith('.test'):
                generated.add(row['ImportPath'])
                continue
            directory = Path(row['Dir'])
            for field in FIELDS:
                if field.startswith(('Test', 'XTest')) and row['ImportPath'] not in imports:
                    continue
                for name in row.get(field, []):
                    path = directory / name
                    paths.add(path)
                    if field in ('TestGoFiles', 'XTestGoFiles') and row['ImportPath'] in imports:
                        selected.update((row['ImportPath'], name) for name in
                                        re.findall(r'^func (Test[A-Za-z0-9_]+)\(', path.read_text(), re.M))
            module = row.get('Module', {})
            if module.get('GoMod'):
                paths.add(Path(module['GoMod']))
    return 0, paths, selected, generated


def checks(action, out):
    tmp = out / 'tmp'
    tmp.mkdir(mode=0o700)
    overrides = dict(ENV, TMPDIR=str(tmp), GOTMPDIR=str(tmp))
    env = dict(os.environ, **overrides)
    packages = list(TESTS) if action == 'basic' else [CONTROL]
    save(out, 'invocation.json', {'action': action, 'packages': packages,
         'source_head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
         'environment': overrides, 'wrapper_pid': os.getpid(), 'script_sha256': sha(__file__)})
    code, paths, selected, generated = discover(out, 'before', packages, env, action == 'basic')
    if code:
        save(out, 'result.json', {'actual_checks_exit': code, 'stage': 'deps-before'})
        return code
    expected = {('github.com/LunaDeerTech/agenteam/' + p[2:], name)
                for p in packages for name in (TESTS[p] if action == 'basic' else (TOP,))}
    if not expected <= selected:
        raise ValueError('exact test declarations missing: ' + repr(sorted(expected - selected)))
    before = snapshot(paths)
    save(out, 'inputs-before.json', before)
    save(out, 'selection.json', {'expected_top_tests': sorted(expected),
         'generated_test_mains_excluded': sorted(generated),
         'scope': 'actual compiler sources/embeds/modules plus explicit method files; no whole-tree scan'})
    if action == 'basic':
        selector = '^(' + '|'.join(name for _, name in sorted(expected)) + ')$'
        code = run(out, 'unit', [GO, 'test', '-race', '-count=1', '-p=1', '-timeout=120s',
                               '-json', '-run', selector, *packages], env)
        events = []
        for line in (out / 'unit.log').read_text().splitlines():
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                pass  # Keep original compiler diagnostics unchanged in unit.log.
        passed = {(e['Package'], e['Test']) for e in events if e.get('Action') == 'pass' and e.get('Test')}
        failed = [e for e in events if e.get('Action') in ('fail', 'skip')]
        exact = {item for item in passed if '/' not in item[1]} == expected and not failed
        save(out, 'unit-selection.json', {'passed': sorted(passed), 'expected_top_tests': sorted(expected),
             'exact_top_pass': exact, 'top_pass_count': sum('/' not in t for _, t in passed),
             'subtest_pass_count': sum('/' in t for _, t in passed)})
        after_unit = snapshot(paths)
        save(out, 'inputs-after-unit.json', after_unit)
        code = int(code or not exact or after_unit != before)
        if not code:
            code = run(out, 'vet', [GO, 'vet', '-p=1', *packages], env)
    else:
        binary = out / 'write-file-wss.test'
        code = run(out, 'compile', [GO, 'test', '-race', '-c', '-p=1', '-o', binary, CONTROL], env)
        after_compile = snapshot(paths)
        save(out, 'inputs-after-compile.json', after_compile)
        code = int(code or after_compile != before)
        if not code:
            save(out, 'binary.json', {'path': str(binary), 'sha256': sha(binary), 'size': binary.stat().st_size})
            code = run(out, 'list', [binary, '-test.list', '^' + TOP + '$'], env)
            exact = (out / 'list.log').read_text().splitlines() == [TOP]
            save(out, 'list-selection.json', {'exact_single_top': exact, 'selector': '^' + TOP + '$'})
            code = int(code or not exact)
    listed, after_paths, _, _ = discover(out, 'after', packages, env, action == 'basic')
    after = snapshot(after_paths)
    save(out, 'inputs-after.json', after)
    unchanged = not listed and before == after
    code = int(code or listed or not unchanged)
    save(out, 'result.json', {'actual_checks_exit': code, 'inputs_unchanged': unchanged,
         'input_count_before': len(before), 'input_count_after': len(after)})
    return code


def prepare(compilation, out, namespace):
    result = read(compilation, 'result.json')
    if result.get('actual_checks_exit') != 0 or not result.get('inputs_unchanged'):
        raise ValueError('successful frozen compile/list required')
    before = read(compilation, 'inputs-before.json')
    if (before != read(compilation, 'inputs-after.json') or
            before != read(compilation, 'inputs-after-compile.json') or
            before != snapshot(map(Path, before))):
        raise ValueError('compiled input changed')
    if not read(compilation, 'list-selection.json')['exact_single_top']:
        raise ValueError('unique top list required')
    binary = read(compilation, 'binary.json')
    path = Path(binary['path'])
    if binary != {'path': str(path), 'sha256': sha(path), 'size': path.stat().st_size}:
        raise ValueError('compiled binary changed')
    if (not re.fullmatch(r'/tmp/rwfw[0-9]{2}', str(namespace)) or namespace.exists()
            or namespace.is_symlink()):
        raise ValueError('fresh exact /tmp/rwfwNN namespace required')
    inputs = dict(before, **{str(path): binary['sha256']})
    save(out, 'input-hashes.json', inputs)
    save(out, 'plan.json', {'status': 'prepared only; separate root native window required',
         'compiler_input_count': len(before), 'runtime_input_count': len(inputs),
         'binary': binary, 'namespace': str(namespace), 'selector': SELECTOR,
         'test_timeout_seconds': 60, 'outer_wait_seconds': 70,
         'tcp_tail_seconds': 75, 'tcp_empty_observations': 2,
         'resources': 'fixture-owned TLS/WSS/session/worker/handles/Manager; no PG or Docker',
         'input_hash': hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()})
    save(out, 'invocation.json', {'cwd': str(ROOT), 'environment': dict(ENV, TMPDIR=str(namespace / 'tmp')),
         'command': ['/usr/bin/python3', '-B', str(HERE / 'native.py'), '--request', str(out),
                     '--output', str(namespace)]})
    with (out / 'record-native.py').open('xb') as stream:
        stream.write((HERE / 'record-native.py').read_bytes())
    save(out, 'recorder-identity.json', {'sha256': {name: sha(out / name) for name in
         ('record-native.py', 'invocation.json', 'input-hashes.json', 'plan.json')}})
    print(json.dumps({'prepared_only': str(out), 'runtime_input_count': len(inputs), 'binary': binary}), flush=True)
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='action', required=True)
    for action in ('basic', 'compile', 'prepare'):
        sub = subs.add_parser(action)
        sub.add_argument('--output', required=True, type=Path)
        if action == 'prepare':
            sub.add_argument('--compile', required=True, type=Path)
            sub.add_argument('--namespace', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    if not out.is_relative_to(OUTPUT):
        parser.error('output must belong to this task')
    out.mkdir(parents=True, exist_ok=False)
    started = time.monotonic()
    try:
        code = (prepare(args.compile.resolve(), out, args.namespace) if args.action == 'prepare'
                else checks(args.action, out))
    except Exception as exc:
        save(out, 'exception.json', {'type': type(exc).__name__, 'message': str(exc)})
        code = 1
    save(out, 'outer-terminal.json', {'wrapper_pid': os.getpid(), 'exit_code': code,
         'elapsed_seconds': round(time.monotonic() - started, 3)})
    return code


if __name__ == '__main__':
    raise SystemExit(main())
