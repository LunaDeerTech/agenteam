import datetime, hashlib, json, os, pathlib, subprocess, sys, time

root = pathlib.Path(__file__).resolve().parent
ev = root / 'evidence'
name, tree_name, overlay_name, selector = sys.argv[1:]
snapshot = root / tree_name
overrides = {
    'AGENTEAM_GO': '/workspace/toolchains/go1.27.1/bin/go',
    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off', 'GOPROXY': 'off',
    'GOMODCACHE': '/workspace/agenteam-dependency-cache/modcache',
    'GOCACHE': '/tmp/agenteam-d08-b02-baseline-2h_8imsq/cache',
    'GOTMPDIR': str(root / 'runtime'), 'TMPDIR': str(root / 'runtime'),
    'GOFLAGS': '-mod=readonly -overlay=' + str(root / overlay_name) + ' -v',
    'DOCKER_CONFIG': str(root / 'docker-config'),
    'DOCKER_HOST': 'unix:///var/run/docker.sock',
}
unset = ['DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH', 'DOCKER_API_VERSION',
         'AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_POSTGRES_FIXTURE', 'AGENTEAM_NETWORK_FIXTURE']
env = os.environ.copy()
env.update(overrides)
for key in unset:
    env.pop(key, None)

def call(args):
    return subprocess.run(args, env=env, capture_output=True, text=True)

def ids(kind):
    args = ['ps', '-aq', '--no-trunc'] if kind == 'containers' else ['network', 'ls', '-q', '--no-trunc']
    p = call(['docker', *args]); p.check_returncode()
    return sorted(p.stdout.split())

inputs = json.loads((root / 'inputs.json').read_text())
if tree_name == 'snapshot':
    for path, want in inputs['sources'].items():
        assert hashlib.sha256((snapshot / path).read_bytes()).hexdigest() == want, path
assert hashlib.sha256((snapshot / 'go.mod').read_bytes()).hexdigest() == inputs['go_mod_sha256']
assert hashlib.sha256((snapshot / 'go.sum').read_bytes()).hexdigest() == inputs['go_sum_sha256']
before = {kind: ids(kind) for kind in ['containers', 'networks']}
author_base = json.loads((ev / 'author-confirmed-commit-command.json').read_text())['before']
author_after = {key: author_base[key] for key in before}
assert before == author_after, 'Docker baseline changed since handoff; stop before fixture'
command = [overrides['AGENTEAM_GO'], 'run', './tests/testsupport/postgres/cmd/fixture', '-run', selector]
record = {
    'command': command, 'cwd': str(snapshot), 'base': inputs['baseline'], 'input_kind': tree_name,
    'environment': overrides, 'unset': unset, 'before': before,
    'start': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'source_hashes': {path: hashlib.sha256((snapshot / path).read_bytes()).hexdigest() if (snapshot / path).is_file() else None for path in inputs['sources']},
    'probe_hashes': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in (root / 'probe').glob('*.go')},
    'tools': {key: {'path': overrides[key], 'sha256': hashlib.sha256(pathlib.Path(overrides[key]).read_bytes()).hexdigest()} for key in ['AGENTEAM_GO']},
}
(ev / (name + '-command.json')).write_text(json.dumps(record, indent=2) + '\n')
print('fixture starting ' + record['start'], flush=True)
with (ev / (name + '-events.jsonl')).open('w') as events, (ev / (name + '-events.stderr')).open('w') as errors, (ev / (name + '.log')).open('w') as log:
    watcher = subprocess.Popen(['docker', 'events', '--filter', 'event=create', '--format', '{{json .}}'], env=env, stdout=events, stderr=errors)
    started = time.monotonic()
    try:
        result = subprocess.run(command, cwd=snapshot, env=env, stdout=log, stderr=subprocess.STDOUT)
    finally:
        watcher.terminate(); watcher.wait(timeout=10)
elapsed = time.monotonic() - started
after = {kind: ids(kind) for kind in before}
owned = []
for line in (ev / (name + '-events.jsonl')).read_text().splitlines():
    item = json.loads(line); kind = item.get('Type'); attrs = item.get('Actor', {}).get('Attributes', {})
    ident = item.get('Actor', {}).get('ID', '')
    if kind not in ['container', 'network']:
        continue
    inspected = call(['docker', *(['inspect', '--format', '{{.Id}}', ident] if kind == 'container' else ['network', 'inspect', '--format', '{{.Id}}', ident])])
    owned.append({'kind': kind, 'id': ident, 'name': attrs.get('name'), 'labels': {k: v for k, v in attrs.items() if 'agenteam' in k}, 'inspect_exit': inspected.returncode, 'inspect_stdout': inspected.stdout.strip(), 'inspect_stderr': inspected.stderr.strip()})
summary = {'exit': result.returncode, 'seconds': elapsed, 'end': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'after': after, 'baseline_preserved': before == after, 'owned_create_events': owned}
(ev / (name + '-result.json')).write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps({'exit': result.returncode, 'seconds': elapsed, 'baseline_preserved': before == after, 'resources_created': len(owned)}, indent=2), flush=True)
sys.exit(result.returncode)
