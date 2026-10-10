#!/usr/bin/env python3
"""One independent PG probe; original supervisor owns every real resource tail."""
import argparse
import hashlib
from pathlib import Path
import stat
import sys

ROOT = Path(__file__).resolve().parents[2]
SUPERVISOR = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestIndependentSecretHTTPCurrentSessionAndSafeErrors$'
CASES = {'TestIndependentSecretHTTPCurrentSessionAndSafeErrors': (
    'current-session-after-domain-begin', 'hostile-member-errors-are-safe')}
DRIVER = 'output/ai/secret-owner-http/candidate-01/pg-only-driver'
BINARY = 'output/ai/secret-owner-http/candidate-independent-01/secret-http-independent.test'
PROBE = 'tests/projectvariable/secret_http_independent_test.go'
OWN = '.agent-state/secret-owner-http/'

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
            'tests/projectvariable/secret_http_fixture_test.go', PROBE,
            'api/openapi/secret-variables.json', 'api/openapi/common.json',
            OWN + 'independent.py', OWN + 'independent-controls.py',
            OWN + 'independent-README.md')


def inputs(driver, binary, selector):
    if selector != SELECTOR or driver != ROOT / DRIVER or binary != ROOT / BINARY:
        raise ValueError('exact independent selector and artifacts required')
    paths = {ROOT / name for name in REQUIRED} | {driver, binary}
    for name in CENTRAL:
        files = list((ROOT / 'internal/central' / name).glob('*.go'))
        if not files:
            raise ValueError('missing independent production input directory')
        paths.update(p for p in files if not p.name.endswith('_test.go'))
    paths.update((ROOT / 'internal/central/projectvariable/http').glob('*.go'))
    for name in GO_TREES:
        files = list((ROOT / name).glob('*.go'))
        if not files:
            raise ValueError('missing independent fixture input directory')
        paths.update(files)
    ddl = list((ROOT / 'db/migrations').glob('*.sql'))
    if not ddl:
        raise ValueError('missing independent migrations')
    paths.update(ddl)
    for path in paths:
        if path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('independent input must be regular and not symlinked')
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def arguments(argv):
    parser = argparse.ArgumentParser(allow_abbrev=False)
    for name in ('driver', 'binary', 'run', 'output'):
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args(argv)
    if len(argv) != 8 or any(argv.count('--' + name) != 1 for name in ('driver', 'binary', 'run', 'output')):
        parser.error('each explicit independent argument is required exactly once')
    if args.run != SELECTOR:
        parser.error('one exact independent selector required')
    if Path(args.driver) != ROOT / DRIVER or Path(args.binary) != ROOT / BINARY:
        parser.error('fixed independent artifacts required')
    output = Path(args.output)
    parent = ROOT / 'output/ai/secret-owner-http'
    if not output.is_absolute() or output.parent != parent or not output.name.startswith('independent-pg-'):
        parser.error('independent output must be a named task-owned round')
    if output.exists() or output.is_symlink():
        parser.error('independent output must be fresh')
    return args


def load_supervisor():
    path = ROOT / SUPERVISOR
    namespace = {'__file__': str(path), '__name__': 'secret_http_independent_supervisor'}
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
