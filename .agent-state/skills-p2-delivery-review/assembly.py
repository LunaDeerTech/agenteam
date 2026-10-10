#!/usr/bin/env python3
"""Read-only P2 assembly/source check; no build, process fixture or database."""
import difflib
from pathlib import Path
import re
import subprocess

TARGET = Path('/workspace/agenteam-skills-p2-delivery')
BASE = 'fb6ab7f492850bf1d3c025a59acbff381312a989'
AUTHOR = 'eaad50fdb08e248b850a65c74f49d3aabf73b682'
INDEPENDENT = '5bb671868ce5b4f9f0ff18fde0a692d432392911'
MANIFEST = 'fb321664'


def git(*args):
    return subprocess.check_output(['git', *args], cwd=TARGET)


def blob(revision, path):
    return git('show', revision + ':' + path)


assert git('rev-parse', 'HEAD').decode().strip() == BASE
manifest = blob(MANIFEST, '.agent-state/skills-cleanup/p2-delivery.md').decode()
blocks = re.findall(r'```text\n(.*?)\n```', manifest, re.S)
new = blocks[0].splitlines() + blocks[1].splitlines()
assert len(new) == len(set(new)) == 40
for path in new:
    assert (TARGET / path).read_bytes() == blob(AUTHOR, path), path
independent = {
    'tests/skills/p2_independent_test.go': 'tests/skills/p2_independent_test.go',
    'internal/central/skill/p2_independent_test.go': '.agent-state/skills-p2-independent/pure_test.go',
}
for target, source in independent.items():
    assert (TARGET / target).read_bytes() == blob(INDEPENDENT, source), target
shared = [
    'internal/central/object/contract/authority.go',
    'internal/central/object/transfer_upload.go',
    'internal/central/object/contract/access.go',
    'internal/central/object/contract/reference_cleanup.go',
    'internal/central/object/contract/knowledge_cleanup_test.go',
]
for path in shared:
    before = blob(BASE, path)
    expected = blob(AUTHOR, path)
    actual = (TARGET / path).read_bytes()
    assert actual == expected, path
    # Apply the inverse of only the approved same-file hunks. Any untouched
    # Avatar/Knowledge/Human/Agent/SQL byte must be retained from new main.
    old, newlines = before.splitlines(keepends=True), expected.splitlines(keepends=True)
    lines = actual.splitlines(keepends=True)
    ops = difflib.SequenceMatcher(None, old, newlines, autojunk=False).get_opcodes()
    for kind, i, j, x, y in reversed(ops):
        if kind != 'equal':
            assert lines[x:y] == newlines[x:y]
            lines[x:y] = old[i:j]
    assert b''.join(lines) == before, path
technical = set(new) | set(independent) | set(shared)
assert len(technical) == 47
allowed_documents = {
    'docs/development/work-items/d10-skills-initialization.md',
    'docs/development/work-items/d10-skills-initialization-design.md',
    'docs/development/backend/README.md',
    'docs/development/agent-team/tasks.md',
    '.agent-state/current.md',
}
changed = set(git('diff', '--name-only', BASE).decode().splitlines())
assert changed - allowed_documents == technical, sorted(changed - allowed_documents - technical)
assert not git('ls-files', '--others', '--exclude-standard').strip(), 'unexpected untracked input'
prior_migrations = set(git('ls-tree', '-r', '--name-only', BASE, 'db/migrations').decode().splitlines())
prior_sql = {p for p in prior_migrations if p.endswith('.sql')}
actual_sql = {str(p.relative_to(TARGET)) for p in (TARGET / 'db/migrations').glob('*.sql')}
assert actual_sql == prior_sql | {'db/migrations/00027_skills.sql'}
assert sorted(int(Path(p).name.split('_', 1)[0]) for p in actual_sql) == list(range(1, 28))
for path in prior_migrations:
    assert (TARGET / path).read_bytes() == blob(BASE, path), path
assert blob(AUTHOR, 'db/migrations/00026_runner_control.sql') == blob(BASE, 'db/migrations/00026_runner_control.sql')
for path in ('.agent-state/project-variables-independent/commitproxy/proxy.go',
             '.agent-state/project-variables-independent/commitproxy/proxy_test.go'):
    assert (TARGET / path).read_bytes() == blob(BASE, path) == blob(AUTHOR, path), path
subprocess.run(['git', 'diff', '--check', BASE], cwd=TARGET, check=True)
print('PASS 42 original blobs including moved pure; 5 shared inverse hunks; exactly47 technical paths; unchanged main/commitproxy/1..26 and continuous1..27; no build or SQL execution')
