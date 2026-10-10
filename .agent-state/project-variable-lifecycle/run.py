#!/usr/bin/env python3
"""One author lifecycle PG probe; original supervisor owns every real resource tail."""
import argparse
import hashlib
from pathlib import Path
import stat
import sys

ROOT = Path(__file__).resolve().parents[2]
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestProjectVariableLocalLifecycleStop$'
CASES = {'TestProjectVariableLocalLifecycleStop': (
    'archive-calls-and-project-isolation', 'delete-read-calls',
    'confirmed-phase-and-rejection')}
DRIVER = 'output/ai/project-variable-lifecycle/candidate-01/pg-only-driver'
BINARY = 'output/ai/project-variable-lifecycle/candidate-01/project-variable-lifecycle.test'
PROBE = 'tests/projectvariable/project_lifecycle_test.go'
OWN = '.agent-state/project-variable-lifecycle/'

# The accepted author PG entry's concrete dependency closure. No root/MinIO or
# native listener is introduced. Re-enumeration detects new and removed inputs.
CENTRAL = ('account', 'account/contract', 'accountmail', 'audit', 'audit/contract',
           'cursor', 'event/contract', 'foundation', 'httpapi', 'identity/contract',
           'object', 'object/contract', 'outbound', 'outbox', 'outbox/contract',
           'postgres', 'project', 'project/contract', 'projectvariable',
           'projectvariable/contract', 'projectvariable/http', 'recoverylog',
           'secret', 'secret/contract', 'work', 'work/contract')
GO_TREES = ('tests/projectvariable', 'tests/testsupport/postgres', 'db/migrations',
            '.agent-state/project-variables-independent/commitproxy')
REQUIRED = (SUPERVISOR, '.agent-state/task-planning-recovery/pg_only_driver.go',
            'go.mod', 'go.sum', 'internal/central/account/assets/weak-passwords.json',
            'tests/projectvariable/project_lifecycle_fixture_test.go', PROBE,
            OWN + 'run.py', OWN + 'entry-controls.py', OWN + 'README.md')


def inputs(driver, binary, selector):
    if selector != SELECTOR or driver != ROOT / DRIVER or binary != ROOT / BINARY:
        raise ValueError('exact lifecycle selector and artifacts required')
    paths = {ROOT / name for name in REQUIRED} | {driver, binary}
    for name in CENTRAL:
        files = list((ROOT / 'internal/central' / name).glob('*.go'))
        if not files:
            raise ValueError('missing lifecycle production input directory')
        paths.update(p for p in files if not p.name.endswith('_test.go'))
    paths.update((ROOT / 'internal/central/projectvariable/http').glob('*.go'))
    for name in GO_TREES:
        files = list((ROOT / name).glob('*.go'))
        if not files:
            raise ValueError('missing lifecycle fixture input directory')
        paths.update(files)
    ddl = list((ROOT / 'db/migrations').glob('*.sql'))
    if not ddl:
        raise ValueError('missing lifecycle migrations')
    paths.update(ddl)
    for path in paths:
        if path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('lifecycle input must be regular and not symlinked')
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def arguments(argv):
    parser = argparse.ArgumentParser(allow_abbrev=False)
    for name in ('driver', 'binary', 'run', 'output'):
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args(argv)
    if len(argv) != 8 or any(argv.count('--' + name) != 1 for name in ('driver', 'binary', 'run', 'output')):
        parser.error('each explicit lifecycle argument is required exactly once')
    if args.run != SELECTOR:
        parser.error('one exact lifecycle selector required')
    if Path(args.driver) != ROOT / DRIVER or Path(args.binary) != ROOT / BINARY:
        parser.error('fixed lifecycle artifacts required')
    output = Path(args.output)
    parent = ROOT / 'output/ai/project-variable-lifecycle'
    if not output.is_absolute() or output.parent != parent or not output.name.startswith('pg-author-'):
        parser.error('lifecycle output must be a named task-owned round')
    if output.exists() or output.is_symlink():
        parser.error('lifecycle output must be fresh')
    return args


def load_supervisor():
    path = ROOT / SUPERVISOR
    namespace = {'__file__': str(path), '__name__': 'project_variable_lifecycle_supervisor'}
    exec(compile(path.read_text(), str(path), 'exec'), namespace)
    namespace['SECRET_HTTP_PG'] = SELECTOR
    namespace['SECRET_HTTP_CASES'] = {SELECTOR: CASES}
    namespace['secret_http_inputs'] = inputs
    return namespace


def main():
    # This selector does not match the shared supervisor's historical namespace
    # guards. Reject all aliases/root/native/unknown options BEFORE loading it.
    arguments(sys.argv[1:])
    namespace = load_supervisor()
    return namespace['main']()


if __name__ == '__main__':
    sys.exit(main())
