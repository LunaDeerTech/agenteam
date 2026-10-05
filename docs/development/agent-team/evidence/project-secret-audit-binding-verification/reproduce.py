#!/usr/bin/env python3
"""Rebuild a frozen Project/Secret verification input; default only prepares files.

--pure runs the selected independent pure checks. --fixture requires an already
assigned exclusive Docker window and runs the original PostgreSQL fixture driver.
Modes preserve the original one-shot selections and the fixed sixteen-trial
diagnostic; they never retry failures or alter the original six-minute budget.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workspace', type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument('--source-commit', default='81fe7427ceb4672247b3d30a51c10a2e2808ba04')
    parser.add_argument('--mode', choices=['binding', 'original-top', 'baseline-diagnostic', 'candidate-diagnostic'], default='binding')
    parser.add_argument('--pure', action='store_true')
    parser.add_argument('--fixture', action='store_true')
    parser.add_argument('--go', default='/workspace/toolchains/go1.27.1/bin/go')
    parser.add_argument('--modcache', default='/workspace/agenteam-dependency-cache/modcache')
    args = parser.parse_args()
    evidence = Path(__file__).resolve().parent
    inputs = json.loads((evidence / 'fixed-inputs.json').read_text())
    results = json.loads((evidence / 'verification-results.json').read_text())
    root = Path(tempfile.mkdtemp(prefix='agenteam-project-secret-binding-reproduce-'))
    snapshot = root / 'snapshot'; snapshot.mkdir()
    archive = root / 'base.tar'
    with archive.open('wb') as output:
        subprocess.run(['git', '-C', str(args.workspace), 'archive', inputs['baseline']], stdout=output, check=True)
    if sha(archive.read_bytes()) != inputs['archive_sha256']:
        raise RuntimeError('Frozen baseline archive mismatch')
    with tarfile.open(archive) as source:
        source.extractall(snapshot, filter='data')
    source_hashes = {}
    if args.mode != 'baseline-diagnostic':
        for path, expected in inputs['sources'].items():
            data = subprocess.check_output(['git', '-C', str(args.workspace), 'show', args.source_commit + ':' + path])
            if sha(data) != expected:
                raise RuntimeError('Frozen candidate source mismatch: ' + path)
            destination = snapshot / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
            source_hashes[path] = sha(data)
    for name in ['go.mod', 'go.sum']:
        if sha((snapshot / name).read_bytes()) != inputs[name.replace('.', '_') + '_sha256']:
            raise RuntimeError('Module input mismatch: ' + name)
    if args.mode == 'binding':
        probes = {
            'independent_binding_test.go.txt': 'tests/project/independent_binding_test.go',
            'independent_binding_map_test.go.txt': 'internal/central/project/independent_binding_map_test.go',
        }
        selector = '^TestProjectBindingIndependentAuthorityAndOriginalWriter$'
    elif args.mode == 'original-top':
        probes = {
            'b03_r3_fixture_observed_test.go.txt': 'tests/project/b03_r3_fixture_test.go',
            'independent_terminal_observer_test.go.txt': 'tests/project/independent_terminal_observer_test.go',
        }
        selector = '^TestOutboxLifecycleInspectionAuthorityBoundary$'
    else:
        probes = {'independent_terminal_time_test.go.txt': 'tests/project/independent_terminal_time_test.go'}
        selector = '^TestProjectBindingIndependentTerminalTimeDiagnostic$'
    replacements = {}
    probe_dir = root / 'probe'; probe_dir.mkdir()
    for name, target in probes.items():
        data = (evidence / name).read_bytes()
        if sha(data) != results['probe_files'][name]:
            raise RuntimeError('Probe mismatch: ' + name)
        destination = probe_dir / name.removesuffix('.txt')
        destination.write_bytes(data)
        replacements[str(snapshot / target)] = str(destination)
    overlay = root / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': replacements}, indent=2) + '\n')
    for name in ['runtime', 'cache', 'docker-config', 'logs']:
        (root / name).mkdir()
    env = os.environ.copy()
    overrides = {
        'AGENTEAM_GO': args.go,
        'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
        'GOMODCACHE': args.modcache, 'GOCACHE': str(root / 'cache'),
        'GOTMPDIR': str(root / 'runtime'), 'TMPDIR': str(root / 'runtime'),
        'GOFLAGS': '-mod=readonly -overlay=' + str(overlay) + ' -v',
        'DOCKER_CONFIG': str(root / 'docker-config'), 'DOCKER_HOST': 'unix:///var/run/docker.sock',
    }
    env.update(overrides)
    unset = ['DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH', 'DOCKER_API_VERSION',
             'AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_POSTGRES_FIXTURE', 'AGENTEAM_NETWORK_FIXTURE']
    for key in unset:
        env.pop(key, None)
    record = {'root': str(root), 'mode': args.mode, 'baseline': inputs['baseline'], 'source_commit': args.source_commit,
              'source_hashes': source_hashes, 'environment': overrides, 'unset': unset, 'selector': selector}
    (root / 'prepared.json').write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps(record), flush=True)
    if not (args.pure or args.fixture):
        return
    version = subprocess.check_output([args.go, 'env', 'GOVERSION'], env=env, text=True).strip()
    if version != 'go1.27.1':
        raise RuntimeError('Exact Go 1.27.1 required')
    commands = []
    if args.pure:
        if args.mode == 'binding':
            commands.append(('map-race', [args.go, 'test', '-race', '-count=1', '-run', '^TestProjectBindingIndependentCallerMapCannotChangeDispatch$', './internal/central/project']))
        commands.extend([
            ('integration-compile', [args.go, 'test', '-tags=integration', '-race', '-run', '^$', './tests/project']),
            ('integration-vet', [args.go, 'vet', '-tags=integration', './internal/central/project', './tests/project']),
        ])
    if args.fixture:
        commands.append(('fixture', [args.go, 'run', './tests/testsupport/postgres/cmd/fixture', '-run', selector]))
    executed = []
    for name, command in commands:
        before = None
        watcher = None
        if name == 'fixture':
            def docker_ids():
                return {kind: sorted(subprocess.check_output(['docker', *argv], env=env, text=True).split())
                        for kind, argv in [('containers', ['ps', '-aq', '--no-trunc']), ('networks', ['network', 'ls', '-q', '--no-trunc'])]}
            before = docker_ids()
            events = (root / 'logs/events.jsonl').open('w')
            event_errors = (root / 'logs/events.stderr').open('w')
            watcher = subprocess.Popen(['docker', 'events', '--filter', 'event=create', '--format', '{{json .}}'], env=env, stdout=events, stderr=event_errors)
        log = root / 'logs' / (name + '.log')
        try:
            with log.open('wb') as output:
                result = subprocess.run(command, cwd=snapshot, env=env, stdout=output, stderr=subprocess.STDOUT)
        finally:
            if watcher is not None:
                watcher.terminate(); watcher.wait(timeout=10); events.close(); event_errors.close()
        entry = {'argv': command, 'cwd': str(snapshot), 'exit': result.returncode, 'log': str(log)}
        if before is not None:
            after = docker_ids()
            inspected = []
            for line in (root / 'logs/events.jsonl').read_text().splitlines():
                event = json.loads(line); kind = event.get('Type')
                if kind not in ['container', 'network']:
                    continue
                ident = event['Actor']['ID']
                argv = ['inspect', '--format', '{{.Id}}', ident] if kind == 'container' else ['network', 'inspect', '--format', '{{.Id}}', ident]
                check = subprocess.run(['docker', *argv], env=env, text=True, capture_output=True)
                inspected.append({'kind': kind, 'id': ident, 'absent': check.returncode == 1 and not check.stdout.strip()})
            entry.update({'before': before, 'after': after, 'baseline_preserved': before == after, 'created_exact_ids': inspected})
        executed.append(entry)
        (root / 'commands.json').write_text(json.dumps(executed, indent=2) + '\n')
        print(name + ': exit=' + str(result.returncode) + ' log=' + str(log), flush=True)
        if result.returncode:
            raise SystemExit(result.returncode)
        if before is not None and (before != after or not all(item['absent'] for item in inspected)):
            raise RuntimeError('Fixture resources differ from initial baseline; retain evidence and stop')


if __name__ == '__main__':
    main()
