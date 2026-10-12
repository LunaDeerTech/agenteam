#!/usr/bin/python3
"""Build the two Runner read-file commands; prepare the original PG chain, never run it here.

compile: two actual offline go-list closures, Runner race build, PG race-c and exact list.
prepare: read successful compile evidence and emit a fresh native request only.
The original supervisor remains the sole owner of budgets, resources and TCP.
"""
import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SELECTOR = '^TestRunnerReadFileApp$'
EXPECTED = (
    'TestRunnerReadFileApp',
    'TestRunnerReadFileApp/explicit-cli-range-and-identity-restart',
    'TestRunnerReadFileApp/default-empty-capability-unsupported',
    'TestRunnerReadFileApp/invalid-root-and-explicit-config-before-connect',
)
DRIVER = ROOT / '.agent-state/work-owner-http/root_chain_driver.py'
SUPERVISOR = ROOT / '.agent-state/task-planning-recovery/pg_only_supervisor.py'
RECORDER_SHA = '56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20'
ENV = {
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
    'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTELEMETRY': 'off',
    'GOMAXPROCS': '1', 'CGO_ENABLED': '1', 'GOFLAGS': '-mod=readonly -p=1',
    'GOCACHE': '/workspace/agenteam-skill-package-http/output/ai/skill-package-http/go-build',
    'GOMODCACHE': '/workspace/agenteam-skills-milestone-resume/output/ai/skills-milestone-resume/go-mod',
    'PYTHONDONTWRITEBYTECODE': '1',
}


def save(directory, name, value):
    with (directory / name).open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def read(directory, name):
    return json.loads((directory / name).read_text())


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def head():
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT,
                                   text=True).strip()


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def inputs(driver, binary):
    # Use the unchanged native input producer, including every same-package
    # fixture, all migrations/embeds and its conservative full-internal scope.
    return {str(path): digest(path) for path in driver.metadata_inputs(binary, SELECTOR)}


def run(out, label, command, environment):
    started = time.monotonic()
    with (out / (label + '.log')).open('xb') as stream:
        process = subprocess.Popen(command, cwd=ROOT, env=environment,
                                   stdout=stream, stderr=subprocess.STDOUT)
        save(out, label + '-process.json', {'pid': process.pid,
                                           'started_monotonic': started})
        print(json.dumps({'stage': label, 'pid': process.pid}), flush=True)
        code = process.wait()
    return {'pid': process.pid, 'actual_exit_code': code,
            'elapsed_seconds': round(time.monotonic() - started, 3)}


SOURCE_FIELDS = ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'HFiles',
                 'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles', 'EmbedFiles')
DESCRIPTOR_NAME = 'runner-read-file-inputs.json'


def source_paths(driver):
    # The original conservative native provenance, before any artifact exists.
    # Do not call metadata_inputs with GO: this selector has no native bypass.
    paths = set(driver.input_paths(driver.GO))
    paths.update((ROOT / 'tests/projectvariable').glob('*.go'))
    paths.update(p for p in (ROOT / 'internal').rglob('*') if p.is_file())
    paths.update((ROOT / 'tests/testsupport').rglob('*.go'))
    paths.update((ROOT / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
    paths.update(ROOT / name for name in driver.METADATA_INPUTS[SELECTOR])
    return paths


def snapshot(paths):
    result = {}
    for raw in sorted(paths, key=str):
        path = Path(raw)
        if not path.is_file() or path.is_symlink() or path.resolve(strict=True) != path:
            raise ValueError('canonical regular compiler/runtime input required')
        result[str(path)] = digest(path)
    return result


def rows(raw):
    decoder, offset = json.JSONDecoder(), 0
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            return
        value, offset = decoder.raw_decode(raw, offset)
        yield value


def compiler_paths(log, target):
    paths, packages, generated = set(), [], []
    for row in rows(log.read_text()):
        name = row['ImportPath']
        if row.get('Name') == 'main' and name.endswith('.test'):
            generated.append(name)
            continue
        directory = Path(row['Dir'])
        packages.append(name)
        for field in SOURCE_FIELDS:
            paths.update(directory / item for item in row.get(field, ()))
        module = row.get('Module', {})
        if module.get('GoMod'):
            paths.add(Path(module['GoMod']))
    if target not in packages:
        raise ValueError('go list omitted its actual target')
    return paths, {'target': target, 'packages': packages, 'paths': sorted(map(str, paths)),
                   'generated_test_mains_excluded': generated}


@contextmanager
def runner_environment(compilation):
    overrides = {
        'AGENTEAM_RUNNER_BINARY': str(compilation / 'agenteam-runner'),
        'AGENTEAM_RUNNER_READ_FILE_APP_INPUTS': str(compilation / DESCRIPTOR_NAME),
    }
    previous = {name: os.environ.get(name) for name in overrides}
    os.environ.update(overrides)
    try:
        yield overrides
    finally:
        for name, value in previous.items():
            if value is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = value


def artifact(path):
    return {'path': str(path), 'sha256': digest(path), 'size_bytes': path.stat().st_size}


def compile_pg(out, driver):
    out.mkdir(parents=True, exist_ok=False)
    environment = dict(os.environ, **ENV)
    prior = snapshot(source_paths(driver))
    save(out, 'discovery-inputs-before.json', prior)
    save(out, 'invocation.json', {
        'cwd': str(ROOT), 'environment': ENV, 'selector': SELECTOR,
        'source_checkpoint': head(), 'wrapper_sha256': digest(Path(__file__)),
        'scope': 'two actual compiler closures plus original conservative native provenance; no PG/services',
    })
    closures, union = {}, set(map(Path, prior))
    for kind, flags, package in (
            ('runner', [], './cmd/agenteam-runner'),
            ('pg', ['-tags=integration', '-test'], './tests/projectvariable')):
        label = 'deps-' + kind
        command = [str(driver.GO), 'list', '-race', '-mod=readonly', '-p=1',
                   *flags, '-deps', '-json', package]
        save(out, label + '-invocation.json', {'command': command, 'cwd': str(ROOT), 'environment': ENV})
        result = run(out, label, command, environment)
        after = snapshot(source_paths(driver))
        save(out, label + '-inputs-after.json', after)
        result['repository_inputs_unchanged'] = after == prior
        save(out, label + '-result.json', result)
        if result['actual_exit_code'] or after != prior:
            return result['actual_exit_code'] or 1
        discovered, closure = compiler_paths(out / (label + '.log'),
                                             'github.com/LunaDeerTech/agenteam/' + package[2:])
        # Local sources were already bound before discovery. External module/
        # stdlib files are discovered now and frozen before either build.
        if any(path.is_relative_to(ROOT) and str(path) not in prior for path in discovered):
            raise ValueError('go list reached an unbound repository source')
        closures[kind] = closure
        union.update(discovered)
    before = snapshot(union)
    if any(before[name] != value for name, value in prior.items()):
        raise ValueError('source changed during compiler discovery')
    save(out, 'compiler-closures.json', closures)
    save(out, 'inputs-before.json', before)
    binaries = {'runner': out / 'agenteam-runner', 'pg': out / 'projectvariable.test'}
    commands = {
        'runner': [str(driver.GO), 'build', '-race', '-mod=readonly', '-p=1',
                   '-o', str(binaries['runner']), './cmd/agenteam-runner'],
        'pg': [str(driver.GO), 'test', '-race', '-tags=integration', '-mod=readonly',
               '-c', '-p=1', '-o', str(binaries['pg']), './tests/projectvariable'],
    }
    for kind in ('runner', 'pg'):
        label = 'compile-' + kind
        stage_before = snapshot(union)
        save(out, label + '-inputs-before.json', stage_before)
        if stage_before != before or snapshot(source_paths(driver)) != prior:
            raise ValueError('original inputs changed before ' + label)
        save(out, label + '-invocation.json', {'command': commands[kind], 'cwd': str(ROOT), 'environment': ENV})
        result = run(out, label, commands[kind], environment)
        after = snapshot(union)
        save(out, label + '-inputs-after.json', after)
        result['inputs_unchanged'] = before == after and snapshot(source_paths(driver)) == prior
        save(out, label + '-result.json', result)
        if result['actual_exit_code'] or not result['inputs_unchanged']:
            return result['actual_exit_code'] or 1
        save(out, kind + '-binary.json', artifact(binaries[kind]))
    artifacts = {kind: read(out, kind + '-binary.json') for kind in binaries}
    command = [str(binaries['pg']), '-test.list', SELECTOR]
    save(out, 'list-invocation.json', {'command': command, 'cwd': str(ROOT), 'environment': ENV})
    result = run(out, 'list', command, environment)
    after_list = snapshot(union)
    save(out, 'inputs-after-list.json', after_list)
    result['inputs_unchanged'] = before == after_list and snapshot(source_paths(driver)) == prior
    result['artifacts_unchanged'] = artifacts == {kind: artifact(path) for kind, path in binaries.items()}
    result['exact_selector'] = (out / 'list.log').read_text().splitlines() == [EXPECTED[0]]
    save(out, 'list-result.json', result)
    if (result['actual_exit_code'] or not result['inputs_unchanged']
            or not result['artifacts_unchanged'] or not result['exact_selector']):
        return result['actual_exit_code'] or 1
    save(out, DESCRIPTOR_NAME, {
        'schema': 1, 'selector': SELECTOR, 'artifacts': artifacts,
        'source_inputs': before,
        'compiler_inputs': {kind: closures[kind]['paths'] for kind in binaries},
    })
    with runner_environment(out):
        runtime = inputs(driver, binaries['pg'])
    expected = dict(before, **{item['path']: item['sha256'] for item in artifacts.values()})
    expected[str(out / DESCRIPTOR_NAME)] = digest(out / DESCRIPTOR_NAME)
    if runtime != expected:
        raise ValueError('original native scope differs from two actual builds and descriptor')
    save(out, 'runtime-input-hashes.json', runtime)
    save(out, 'result.json', {'actual_exit_code': 0, 'inputs_unchanged': True,
                              'artifact_count': 2, 'input_count': len(before)})
    print(json.dumps({'compiled': artifacts, 'input_count': len(before),
                      'runtime_input_count': len(runtime)}), flush=True)
    return 0


def prepare_native(compilation, out, namespace, driver):
    # This path never runs Go/the test binary, inspects TCP, or starts a service.
    # Documents may advance independently; actual compiled inputs must match.
    original = read(compilation, 'inputs-before.json')
    if snapshot(original) != original:
        raise ValueError('original compiler/runtime sources changed')
    prior = read(compilation, 'discovery-inputs-before.json')
    if snapshot(source_paths(driver)) != prior:
        raise ValueError('original repository provenance changed')
    for kind in ('runner', 'pg'):
        for suffix in ('before', 'after'):
            if read(compilation, 'compile-' + kind + '-inputs-' + suffix + '.json') != original:
                raise ValueError('build input records differ')
        result = read(compilation, 'compile-' + kind + '-result.json')
        if result['actual_exit_code'] != 0 or result['inputs_unchanged'] is not True:
            raise ValueError('both original builds must actually pass')
        discovery = read(compilation, 'deps-' + kind + '-result.json')
        if discovery['actual_exit_code'] != 0 or discovery['repository_inputs_unchanged'] is not True:
            raise ValueError('both compiler discoveries must actually pass')
    result = read(compilation, 'list-result.json')
    if (result['actual_exit_code'] != 0 or result['inputs_unchanged'] is not True
            or result['artifacts_unchanged'] is not True or result['exact_selector'] is not True
            or read(compilation, 'inputs-after-list.json') != original
            or (compilation / 'list.log').read_text().splitlines() != [EXPECTED[0]]):
        raise ValueError('exact original PG list must actually pass')
    artifacts = {kind: read(compilation, kind + '-binary.json') for kind in ('runner', 'pg')}
    binary = compilation / 'projectvariable.test'
    if artifacts != {'runner': artifact(compilation / 'agenteam-runner'), 'pg': artifact(binary)}:
        raise ValueError('compiled artifacts changed')
    binary_identity = artifacts['pg']
    with runner_environment(compilation):
        runtime = inputs(driver, binary)
    expected = dict(original, **{item['path']: item['sha256'] for item in artifacts.values()})
    expected[str(compilation / DESCRIPTOR_NAME)] = digest(compilation / DESCRIPTOR_NAME)
    if runtime != read(compilation, 'runtime-input-hashes.json') or runtime != expected:
        raise ValueError('runtime input scope changed after compilation')
    if not namespace.is_absolute() or namespace.is_symlink() or namespace.resolve() != namespace:
        raise ValueError('canonical fresh namespace required')
    with runner_environment(compilation):
        plan = driver.configuration(binary, SELECTOR, namespace)
    supervisor = load(SUPERVISOR, 'runner_read_file_app_original_supervisor')
    if supervisor.METADATA_GROUPS[SELECTOR] != frozenset(EXPECTED) or supervisor.budgets(True) != (540, 60):
        raise ValueError('original profile or budget differs')
    recorder = Path(__file__).with_name('record-native.py')
    if digest(recorder) != RECORDER_SHA:
        raise ValueError('original thin recorder changed')
    out.mkdir(parents=True, exist_ok=False)
    stat = os.statvfs(out)
    environment = dict(ENV, PATH='/workspace/toolchains/go1.27.1/bin:/usr/local/bin:/usr/bin:/bin',
                       LANG='C.UTF-8', LC_ALL='C.UTF-8', TZ='UTC')
    with runner_environment(compilation) as overrides:
        environment.update(overrides)
    for name, leaf in (('GOTMPDIR', 'go-tmp'), ('TMPDIR', 'tmp'),
                       ('XDG_CONFIG_HOME', 'go-config'), ('XDG_CACHE_HOME', 'xdg-cache'),
                       ('DOCKER_CONFIG', 'docker-config')):
        environment[name] = str(out / leaf)
    invocation = {
        'command': ['/usr/bin/python3', str(SUPERVISOR), '--driver', str(DRIVER),
                    '--binary', str(binary), '--run', SELECTOR,
                    '--output', str(namespace), '--root-chain'],
        'cwd': str(ROOT), 'environment': environment,
        'source_checkpoint': head(), 'compilation_directory': str(compilation),
        'compiled_source_checkpoint': read(compilation, 'invocation.json')['source_checkpoint'],
        'binary': binary_identity, 'runner_binary': artifacts['runner'], 'input_count': len(runtime),
        'same_package_go_count': len(list((ROOT / 'tests/projectvariable').glob('*.go'))),
        'effective_target': driver.TARGETS[SELECTOR], 'expected_tests': list(EXPECTED),
        'driver_budget': list(supervisor.budgets(True)), 'test_timeout': plan['test_timeout'],
        'capacity': {'available_bytes': stat.f_bavail * stat.f_frsize,
                     'available_inodes': stat.f_favail, 'observation': 'preparation only; not runtime peak'},
        'host_tcp_method': 'original automatic baseline delta; two empty observations within 75s; no filtering',
        'scope': 'actual prebuilt Runner CLI enrollment/stored identity, explicit read-file range, default unsupported and invalid configuration; public process Wait/identity/Manager shutdown; original seven owned PG resources and all tail gates, no native rebuild',
        'status': 'prepared only; native requires separate root window',
        'preparation_wrapper_sha256': digest(Path(__file__)),
    }
    save(out, 'invocation.json', invocation)
    save(out, 'plan.json', plan)
    save(out, 'input-hashes.json', runtime)
    with (out / 'record-native.py').open('xb') as stream:
        stream.write(recorder.read_bytes())
    save(out, 'recorder-identity.json', {
        'scope': 'outer recording only; original supervisor is sole resource authority',
        'sha256': {name: digest(out / name) for name in
                   ('record-native.py', 'invocation.json', 'input-hashes.json', 'plan.json')},
    })
    print(json.dumps({'prepared_only': str(out), 'input_count': len(runtime),
                      'binary': binary_identity, 'namespace': str(namespace)}), flush=True)
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    compile_parser = commands.add_parser('compile', help='two offline builds and exact PG list; requires a root Go window')
    compile_parser.add_argument('--output', type=Path, required=True)
    prepare_parser = commands.add_parser('prepare', help='generate a request only; never start native')
    prepare_parser.add_argument('--compile', type=Path, required=True)
    prepare_parser.add_argument('--output', type=Path, required=True)
    prepare_parser.add_argument('--namespace', type=Path, required=True)
    args = parser.parse_args()
    driver = load(DRIVER, 'runner_read_file_app_original_driver')
    if driver.TARGETS[SELECTOR] != 'tests/projectvariable':
        raise ValueError('original effective target differs')
    if args.action == 'compile':
        return compile_pg(args.output.resolve(), driver)
    return prepare_native(args.compile.resolve(), args.output.resolve(), args.namespace, driver)


if __name__ == '__main__':
    raise SystemExit(main())
