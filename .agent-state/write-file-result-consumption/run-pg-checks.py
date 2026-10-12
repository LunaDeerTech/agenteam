#!/usr/bin/python3
"""Prepare the original WriteFile Result Consumption PG chain; never start native here.

compile: one root-authorized offline race-c and exact list, with actual Wait.
prepare: read successful compile evidence and emit a fresh native request only.
The original supervisor remains the sole owner of budgets, resources and TCP.
"""
import argparse
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
SELECTOR = '^TestWriteFileResultConsumption$'
EXPECTED = (
    'TestWriteFileResultConsumption',
    'TestWriteFileResultConsumption/revoked-dispatch-grants-consume-and-project',
    'TestWriteFileResultConsumption/commit-loss-lookup-no-resend',
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


def compile_pg(out, driver):
    out.mkdir(parents=True, exist_ok=False)
    before = inputs(driver, driver.GO)
    save(out, 'inputs-before.json', before)
    required = {str(path) for path in (ROOT / 'tests/projectvariable').glob('*.go')}
    required.update(str(path) for path in (ROOT / 'db/migrations').glob('*.sql'))
    if not required.issubset(before) or str(ROOT / 'db/migrations/00070_runner_command_operations.sql') not in before:
        raise ValueError('original profile omitted same-package sources or migration inputs')
    binary = out / 'projectvariable.test'
    command = [str(driver.GO), 'test', '-race', '-tags=integration', '-c',
               '-p=1', '-o', str(binary), './tests/projectvariable']
    save(out, 'invocation.json', {
        'command': command, 'cwd': str(ROOT), 'environment': ENV,
        'selector': SELECTOR, 'source_checkpoint': head(),
        'wrapper_sha256': digest(Path(__file__)),
        'scope': 'original complete driver input scope through migration 70; no new migration; one compile and exact list; no PG/services',
    })
    environment = os.environ.copy()
    environment.update(ENV)
    result = run(out, 'compile', command, environment)
    after = inputs(driver, driver.GO)
    save(out, 'inputs-after.json', after)
    result['inputs_unchanged'] = before == after
    save(out, 'result.json', result)
    print(json.dumps({'compile': result}), flush=True)
    if result['actual_exit_code'] or not result['inputs_unchanged']:
        return result['actual_exit_code'] or 1
    save(out, 'binary.json', {'path': str(binary), 'sha256': digest(binary),
                              'size_bytes': binary.stat().st_size})
    command = [str(binary), '-test.list', SELECTOR]
    save(out, 'list-invocation.json', {'command': command, 'cwd': str(ROOT), 'environment': ENV})
    result = run(out, 'list', command, environment)
    after_list = inputs(driver, driver.GO)
    save(out, 'inputs-after-list.json', after_list)
    result['inputs_unchanged'] = before == after_list
    result['exact_selector'] = (out / 'list.log').read_text().splitlines() == [EXPECTED[0]]
    save(out, 'list-result.json', result)
    print(json.dumps({'list': result}), flush=True)
    if result['actual_exit_code'] or not result['inputs_unchanged'] or not result['exact_selector']:
        return result['actual_exit_code'] or 1
    runtime = inputs(driver, binary)
    if set(runtime) != set(before) | {str(binary)} or any(runtime[path] != value for path, value in before.items()):
        raise ValueError('compiled/runtime input scope mismatch')
    save(out, 'runtime-input-hashes.json', runtime)
    return 0


def prepare_native(compilation, out, namespace, driver):
    # This path never runs Go/the test binary, inspects TCP, or starts a service.
    # Documents may advance independently; actual compiled inputs must match.
    original = read(compilation, 'inputs-before.json')
    for name in ('inputs-after.json', 'inputs-after-list.json'):
        if read(compilation, name) != original:
            raise ValueError('compile input records differ')
    for name in ('result.json', 'list-result.json'):
        result = read(compilation, name)
        if result['actual_exit_code'] != 0 or result['inputs_unchanged'] is not True:
            raise ValueError('compile/list did not actually pass')
    if (read(compilation, 'list-result.json')['exact_selector'] is not True
            or (compilation / 'list.log').read_text().splitlines() != [EXPECTED[0]]):
        raise ValueError('exact original list is required')
    binary_identity = read(compilation, 'binary.json')
    binary = compilation / 'projectvariable.test'
    if (binary_identity != {'path': str(binary), 'sha256': digest(binary),
                            'size_bytes': binary.stat().st_size}
            or inputs(driver, driver.GO) != original):
        raise ValueError('compiled source or binary changed')
    runtime = inputs(driver, binary)
    if (runtime != read(compilation, 'runtime-input-hashes.json')
            or set(runtime) != set(original) | {str(binary)}
            or any(runtime[path] != value for path, value in original.items())):
        raise ValueError('runtime input scope changed after compilation')
    if not namespace.is_absolute() or namespace.is_symlink() or namespace.resolve() != namespace:
        raise ValueError('canonical fresh namespace required')
    plan = driver.configuration(binary, SELECTOR, namespace)
    supervisor = load(SUPERVISOR, 'write_file_result_consumption_original_supervisor')
    if supervisor.METADATA_GROUPS[SELECTOR] != frozenset(EXPECTED) or supervisor.budgets(True) != (540, 60):
        raise ValueError('original profile or budget differs')
    recorder = Path(__file__).with_name('record-native.py')
    if digest(recorder) != RECORDER_SHA:
        raise ValueError('original thin recorder changed')
    out.mkdir(parents=True, exist_ok=False)
    stat = os.statvfs(out)
    environment = dict(ENV, PATH='/workspace/toolchains/go1.27.1/bin:/usr/local/bin:/usr/bin:/bin',
                       LANG='C.UTF-8', LC_ALL='C.UTF-8', TZ='UTC')
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
        'binary': binary_identity, 'input_count': len(runtime),
        'same_package_go_count': len(list((ROOT / 'tests/projectvariable').glob('*.go'))),
        'effective_target': driver.TARGETS[SELECTOR], 'expected_tests': list(EXPECTED),
        'driver_budget': list(supervisor.budgets(True)), 'test_timeout': plan['test_timeout'],
        'capacity': {'available_bytes': stat.f_bavail * stat.f_frsize,
                     'available_inodes': stat.f_favail, 'observation': 'preparation only; not runtime peak'},
        'host_tcp_method': 'original automatic baseline delta; two empty observations within 75s; no filtering',
        'scope': 'real 512KiB Write and stopped dispatch owner; formal Tool/Mount revocation then first receipt consumption and Determine/Projection; consume COMMIT ack loss followed only by original/new-consumer Lookup',
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
    compile_parser = commands.add_parser('compile', help='one offline compile/list; requires a root Go window')
    compile_parser.add_argument('--output', type=Path, required=True)
    prepare_parser = commands.add_parser('prepare', help='generate a request only; never start native')
    prepare_parser.add_argument('--compile', type=Path, required=True)
    prepare_parser.add_argument('--output', type=Path, required=True)
    prepare_parser.add_argument('--namespace', type=Path, required=True)
    args = parser.parse_args()
    driver = load(DRIVER, 'write_file_result_consumption_original_driver')
    if driver.TARGETS[SELECTOR] != 'tests/projectvariable':
        raise ValueError('original effective target differs')
    if args.action == 'compile':
        return compile_pg(args.output.resolve(), driver)
    return prepare_native(args.compile.resolve(), args.output.resolve(), args.namespace, driver)


if __name__ == '__main__':
    raise SystemExit(main())
