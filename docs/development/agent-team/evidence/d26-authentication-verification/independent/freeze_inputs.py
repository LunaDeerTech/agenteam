from pathlib import Path
import hashlib, json, os, subprocess, time

r = Path(__file__).resolve().parent
cwd = r / 'input'
base = '457b1979c9d6563740543b2011eedc06cce34c71'
upstream = json.loads((r / 'upstream.json').read_text())
probe_paths = ['tests/account/authentication_independent_test.go', 'tests/account-captcha-web/independent.config.js', 'tests/account-captcha-web/e2e/independent.spec.ts']
explicit = json.loads((r / 'logs/compile-01.json').read_text())['env']
argv = [explicit['AGENTEAM_GO'], 'list', '-tags=integration', '-deps', '-test', '-json', './tests/account']
start = time.monotonic()
p = subprocess.run(argv, cwd=cwd, env=dict(os.environ, **explicit), capture_output=True)
(r / 'logs/list-inputs-01.log').write_bytes(p.stderr)
(r / 'logs/list-inputs-01.json').write_text(json.dumps({'argv': argv, 'cwd': str(cwd), 'env': explicit, 'exit': p.returncode, 'seconds': time.monotonic()-start, 'stdout_sha256': hashlib.sha256(p.stdout).hexdigest(), 'stdout_bytes': len(p.stdout), 'note': 'Only selected local file inventory retained; dependency JSON not archived.'}, indent=2)+'\n')
assert p.returncode == 0
decoder = json.JSONDecoder()
text, pos, packages = p.stdout.decode(), 0, []
while pos < len(text):
    while pos < len(text) and text[pos].isspace(): pos += 1
    if pos == len(text): break
    value, pos = decoder.raw_decode(text, pos)
    packages.append(value)
selected = set(upstream['sources']) | set(upstream['dependencies_and_dist']) | set(probe_paths)
selected.add('tests/account-captcha-web/e2e/public-solver.ts')
for package in packages:
    if package.get('Module', {}).get('Path') != 'github.com/LunaDeerTech/agenteam': continue
    directory = Path(package.get('Dir', ''))
    for key in ['GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles', 'EmbedFiles', 'TestEmbedFiles', 'XTestEmbedFiles', 'SFiles', 'HFiles']:
        for name in package.get(key, []):
            path = Path(name) if Path(name).is_absolute() else directory / name
            try: relative = path.relative_to(cwd)
            except ValueError: continue
            if path.is_file(): selected.add(str(relative))
# Original driver/cmd entry points are runtime build inputs, not account imports.
tracked = subprocess.run(['git', 'ls-tree', '-r', '-z', base], cwd='/workspace/agenteam', capture_output=True, check=True).stdout
blobs = {}
for record in tracked.split(b'\0'):
    if not record: continue
    meta, path = record.split(b'\t', 1)
    blobs[path.decode()] = meta.split()[2].decode()
for name in blobs:
    if name.startswith('scripts/') or (name.startswith('tests/testsupport/') and '/cmd/' in name and name.endswith('.go')):
        if (cwd/name).is_file(): selected.add(name)
files, origin = {}, {}
overrides = {**upstream['sources'], **upstream['dependencies_and_dist']}
for name in sorted(selected):
    raw = (cwd/name).read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    if name in overrides:
        assert digest == overrides[name], name
        provenance = 'final input05 exact SHA'
    elif name in probe_paths:
        provenance = 'new independent probe'
    else:
        git_blob = hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest()
        assert blobs.get(name) == git_blob, name
        provenance = 'fixed baseline git blob'
    files[name] = digest
    origin[name] = provenance
manifest = {'baseline': base, 'upstream': upstream['manifest'], 'upstream_sha256': upstream['manifest_sha256'], 'files': files, 'origin': origin, 'probe_paths': probe_paths, 'boundary': 'Compile-derived local Go closure plus original driver entrypoints, final21 and exact fixed locks/dist. Original code reused through read-only sparse links, no full source/cache copy.'}
(r/'input.json').write_text(json.dumps(manifest, indent=2)+'\n')
print(json.dumps({'files':len(files),'probe':{name:files[name] for name in probe_paths},'input_sha256':hashlib.sha256((r/'input.json').read_bytes()).hexdigest()},indent=2))
