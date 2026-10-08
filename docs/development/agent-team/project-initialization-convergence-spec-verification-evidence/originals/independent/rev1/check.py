import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path('/workspace/scratch/project-create-frontier4089')
OUT = Path('/workspace/scratch/project-initialization-convergence-verification/rev1')
expected = {
    'draft-rev1.md': '34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b',
    'inputs01.json': '057d9f57e0e7f2786444fcc685c11cda23557587c7bb9dab7bd45f478176367e',
    'freeze-rev1.json': '1edf2e50ddb0e90ceca74e9ff2e21767329d54cb1f8e1f37784ef23c7e288d48',
}

def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()

failures = []
top = []
for name, value in expected.items():
    actual = sha(ROOT / name)
    top.append({'path': str(ROOT / name), 'sha256': actual})
    if actual != value:
        failures.append('top hash: ' + name)

freeze = json.loads((ROOT / 'freeze-rev1.json').read_text())
for x in freeze['files']:
    p = Path(x['path'])
    if sha(p) != x['sha256'] or len(p.read_bytes()) != x['bytes']:
        failures.append('freeze file: ' + x['path'])

inputs = json.loads((ROOT / 'inputs01.json').read_text())
if inputs['commit'] != '4089d13128da8680955005d9507c8a7da74af1f1':
    failures.append('wrong declared baseline')
seen = set()
for x in inputs['sources']:
    p = Path(x['snapshot'])
    raw = p.read_bytes()
    blob = hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()
    if hashlib.sha256(raw).hexdigest() != x['sha256'] or blob != x['git_blob']:
        failures.append('source: ' + x['path'])
    if x['path'] in seen:
        failures.append('duplicate source: ' + x['path'])
    seen.add(x['path'])

# Reuse the previously independently bound technical fingerprints, not a new tree copy.
prep = json.loads((OUT.parent / 'prep01/sources.json').read_text())
by_path = {x['path']: x['sha256'] for x in inputs['sources']}
prep_matches = 0
for x in prep['files']:
    rel = x['path'].removeprefix('/workspace/agenteam/')
    if rel in by_path:
        if by_path[rel] != x['sha256']:
            failures.append('prep mismatch: ' + rel)
        elif x['matches_accepted_audit_runtime_input']:
            prep_matches += 1

raw = (ROOT / 'draft-rev1.md').read_bytes()
text = raw.decode('utf-8')
links = []
for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)', text):
    path, _, fragment = target.partition('#')
    p = (ROOT / path).resolve()
    valid = p.is_file()
    if fragment:
        valid = False  # No fragment links exist in this frozen draft.
    links.append({'target': target, 'exists': valid})
    if not valid:
        failures.append('link: ' + target)
if b'\r' in raw or not raw.endswith(b'\n') or any(x.rstrip() != x for x in text.splitlines()):
    failures.append('format')

scope = re.findall(r'^\| [1-6] \| ([^|]+) \|', text, re.M)
scope = [x.strip() for x in scope]
if len(scope) != 6 or len(set(scope)) != 6:
    failures.append('scope table')
for path in scope[:5]:
    if Path('/workspace/agenteam', path).exists():
        failures.append('new path collision: ' + path)

old_names = [
    'TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts',
    'TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock',
]
old = (ROOT / 'fixed/tests/project/b02_owner_commands_test.go').read_text()
for name in old_names:
    if not re.search(r'^func ' + name + r'\(t \*testing.T\)', old, re.M):
        failures.append('old selector: ' + name)

result = {
    'kind': 'independent-static-doc-input-check',
    'declared_baseline': inputs['commit'],
    'top_inputs': top,
    'freeze_files_checked': len(freeze['files']),
    'fixed_sources_sha256_and_blob_checked': len(inputs['sources']),
    'matches_previously_bound_accepted_technical_sources': prep_matches,
    'links': links,
    'scope': scope,
    'old_selectors_found': old_names,
    'failures': failures,
    'pass': not failures,
    'limits': 'Hash/blob consistency with frozen index; no Git object lookup, Go, DB, socket, container or stopped probe executed.',
}
(OUT / 'checks.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(result, ensure_ascii=False, indent=2))
sys.exit(bool(failures))
