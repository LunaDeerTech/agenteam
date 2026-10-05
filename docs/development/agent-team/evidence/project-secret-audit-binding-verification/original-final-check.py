import datetime, hashlib, json, os, pathlib, subprocess

r = pathlib.Path(__file__).resolve().parent
ev = r / 'evidence'
env = os.environ.copy()
env.update({'DOCKER_CONFIG': str(r / 'docker-config'), 'DOCKER_HOST': 'unix:///var/run/docker.sock'})
for key in ['DOCKER_CONTEXT', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH', 'DOCKER_API_VERSION']:
    env.pop(key, None)
def call(args):
    return subprocess.run(args, env=env, text=True, capture_output=True)
def ids(kind):
    p = call(['docker', *(['ps', '-aq', '--no-trunc'] if kind == 'containers' else ['network', 'ls', '-q', '--no-trunc'])])
    p.check_returncode()
    return sorted(p.stdout.split())
owned = []
for name in ['baseline-terminal-time', 'candidate-terminal-time', 'binding-independent', 'original-outbox-top-observed']:
    result = json.loads((ev / (name + '-result.json')).read_text())
    assert result['exit'] == 0 and result['baseline_preserved']
    for item in result['owned_create_events']:
        p = call(['docker', *(['inspect', '--format', '{{.Id}}', item['id']] if item['kind'] == 'container' else ['network', 'inspect', '--format', '{{.Id}}', item['id']])])
        assert p.returncode == 1 and not p.stdout.strip()
        owned.append({'run': name, 'kind': item['kind'], 'id': item['id'], 'second_inspect_exit': p.returncode})
after = {kind: ids(kind) for kind in ['containers', 'networks']}
before = json.loads((ev / 'baseline-terminal-time-command.json').read_text())['before']
assert after == before
metadata = {}
for kind in after:
    records = []
    for ident in after[kind]:
        args = ['inspect', '--format', '{{json .Id}} {{json .Name}} {{json .Config.Labels}}', ident] if kind == 'containers' else ['network', 'inspect', '--format', '{{json .Id}} {{json .Name}} {{json .Labels}}', ident]
        p = call(['docker', *args]); p.check_returncode(); records.append(p.stdout.strip())
    metadata[kind] = records
inputs = json.loads((r / 'inputs.json').read_text())
checks = {}
for rel, want in inputs['sources'].items():
    checks[rel] = {'expected': want}
    for name, tree in [('snapshot', r / 'snapshot'), ('repository', pathlib.Path('/workspace/agenteam')), ('author_frozen', pathlib.Path('/tmp/agenteam-project-secret-binding-ljibt5s3/candidate-freeze'))]:
        path = tree / rel
        if not path.is_file() and name == 'author_frozen':
            continue
        got = hashlib.sha256(path.read_bytes()).hexdigest()
        checks[rel][name] = got
        assert got == want, (name, rel)
assert list((r / 'runtime').iterdir()) == []
procs = []
for item in pathlib.Path('/proc').iterdir():
    if not item.name.isdigit() or int(item.name) == os.getpid():
        continue
    try:
        argv = (item / 'cmdline').read_bytes().split(b'\0')
        cwd = (item / 'cwd').resolve()
    except (OSError, PermissionError):
        continue
    if not argv or not str(cwd).startswith(str(r)):
        continue
    exe = pathlib.Path(argv[0].decode(errors='replace')).name
    if exe in ['go', 'fixture', 'project.test', 'docker']:
        procs.append({'pid': int(item.name), 'executable': exe})
assert procs == [], procs
out = {'at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'baseline_id_sets_unchanged': True, 'baseline': after, 'current_baseline_identity_metadata': metadata, 'owned_exact_ids_second_absent': owned, 'runtime_empty': True, 'runtime_processes': procs, 'source_checks': checks, 'all_stop': True, 'observer_note': 'Initial inline process enumeration matched its own bash command text; no fixture process was present. This persisted checker uses executable basename.'}
(ev / 'final-input-resources.json').write_text(json.dumps(out, indent=2) + '\n')
print(json.dumps({'baseline_unchanged': True, 'owned_containers_absent': sum(x['kind'] == 'container' for x in owned), 'owned_networks_absent': sum(x['kind'] == 'network' for x in owned), 'source_matches': len(checks), 'runtime_processes': len(procs), 'runtime_empty': True}, indent=2))
