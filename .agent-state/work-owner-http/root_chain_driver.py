#!/usr/bin/env python3
"""Task adapter only: exec the original seven-resource fixture chain.

The shared PG supervisor owns the actual wait and final observations. This
adapter neither supervises a second process tree nor removes Docker resources.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys

REPOSITORY = Path(__file__).resolve().parents[2]
GO = Path('/workspace/toolchains/go1.27.1/bin/go')
MINIO = REPOSITORY / 'output/ai/deps-minio/bin/minio'
MINIO_SHA = 'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
TARGETS = {
    '^TestWorkOwnerRootActual(Command|Reader)Join$': 'internal/central/app',
    '^TestWorkOwnerHTTPProcessRoutingAndPersistence$': 'tests/process',
    '^TestIndependentWorkOwnerRootConfirmationJoin$': 'internal/central/app',
}
UI_CASES = {
    '^TestAccountProjectWorkPlanningWebReadAndNavigation$': 'read',
    '^TestAccountProjectWorkPlanningWebStructureAndTasks$': 'planning',
    '^TestAccountProjectWorkPlanningWebBlockers$': 'blockers',
    '^TestAccountProjectWorkPlanningWebOriginalRecovery$': 'recovery',
    '^TestAccountProjectWorkPlanningWebIdentityAndOwnership$': 'identity',
    '^TestAccountProjectWorkPlanningWebLayouts$': 'layouts',
    '^TestIndependentProjectWorkPlanningWebRecovery$': 'independent-recovery',
    '^TestIndependentProjectWorkPlanningWebAuthority$': 'independent-authority',
}
TARGETS.update({selector: 'tests/account' for selector in UI_CASES})


def ui_assets():
    owned = REPOSITORY / 'output/ai/work-owner-planning-ui'
    dist = Path(os.environ.get('AGENTEAM_PROJECT_OWNER_WEB_DIST', ''))
    if (not dist.is_absolute() or dist.is_symlink()
            or not dist.resolve().is_relative_to(owned.resolve())
            or not (dist / 'index.html').is_file()):
        raise ValueError('owned frozen Work UI dist required')
    assets = list(dist.rglob('*'))
    if any(p.is_symlink() for p in assets):
        raise ValueError('Work UI assets must not alias another tree')
    return sorted(p for p in assets if p.is_file())


def ui_configuration(selector, directory):
    case = UI_CASES[selector]
    if os.environ.get('AGENTEAM_WORK_PLANNING_WEB_CASE') != case:
        raise ValueError('exact Work UI case binding required')
    runtime = directory / 'runtime'
    if len(str(runtime)) > 45:
        raise ValueError('owned browser runtime must meet original short-path bound')
    ui_assets()
    owned = (REPOSITORY / 'output/ai/work-owner-planning-ui').resolve()
    values = {}
    for key in ('AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE', 'AGENTEAM_AUTH_WEB_IMAGES'):
        path = Path(os.environ.get(key, ''))
        if (not path.is_absolute() or path.exists() or path.is_symlink()
                or not path.parent.is_dir() or not path.parent.resolve().is_relative_to(owned)):
            raise ValueError('fresh absolute owned Work UI evidence/image paths required')
        values[key] = str(path)
    paths = list(values.values())
    if paths[0] == paths[1] or any(Path(a).is_relative_to(Path(b)) for a, b in ((paths[0], paths[1]), (paths[1], paths[0]))):
        raise ValueError('separate owned evidence and image directories required')
    harness = REPOSITORY / 'tests/account-captcha-web'
    spec = 'project-work-planning-independent.spec.ts' if case.startswith('independent-') else 'project-work-planning.spec.ts'
    if not (harness / 'e2e' / spec).is_file():
        raise ValueError('the selected real browser spec must exist')
    return values


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def input_paths(binary):
    # The process TestMain rebuilds cmd binaries, so freezing only the test
    # executable would be insufficient. Include production sources and all
    # three actual go:embed inputs; never consume Model's private harness.
    paths = {Path(binary).resolve(), Path(__file__).resolve(), GO, MINIO,
             REPOSITORY / '.agent-state/task-planning-recovery/pg_only_supervisor.py',
             REPOSITORY / 'go.mod', REPOSITORY / 'go.sum',
             REPOSITORY / 'internal/central/account/assets/weak-passwords.json',
             REPOSITORY / 'internal/central/skill/builtin/add-skills/v1/SKILL.md'}
    for name in ('test-objects.sh', 'test-security.sh', 'test-postgres.sh'):
        paths.add(REPOSITORY / 'scripts' / name)
    for directory in ('cmd', 'internal', 'tests/testsupport/postgres',
                      'tests/testsupport/objectstore', 'tests/testsupport/outbound'):
        paths.update(p for p in (REPOSITORY / directory).rglob('*.go')
                     if not p.name.endswith('_test.go'))
    paths.update((REPOSITORY / 'db/migrations').glob('*.go'))
    paths.update((REPOSITORY / 'db/migrations').glob('*.sql'))
    if os.environ.get('AGENTEAM_WORK_PLANNING_WEB_CASE') in UI_CASES.values():
        paths.update(ui_assets())
        paths.update((REPOSITORY / 'tests/account').glob('*.go'))
        harness = REPOSITORY / 'tests/account-captcha-web'
        paths.update((harness / 'e2e').glob('project-work-planning*.ts'))
        paths.update(harness / name for name in ('project-work-planning.config.js', 'package.json', 'package-lock.json'))
        paths.update(REPOSITORY / 'web/src/api' / name for name in ('work-planning.ts', 'client.ts', 'account.ts', 'system-account.ts'))
        paths.update(REPOSITORY / 'api/openapi' / name for name in ('common.json', 'work-planning.json'))
    return sorted(paths)


def configuration(binary, selector, directory):
    binary, directory = Path(binary), Path(directory)
    if (selector not in TARGETS or not binary.is_absolute() or not binary.is_file()
            or not os.access(binary, os.X_OK) or not directory.is_absolute()
            or directory.exists() or directory.is_symlink()):
        raise ValueError('exact root target and fresh absolute directory required')
    if not GO.is_file() or sha(MINIO) != MINIO_SHA:
        raise ValueError('fixed Go and verified cached MinIO required')
    plan = {'binary': str(binary.resolve()), 'selector': selector,
            'cwd': str(REPOSITORY / TARGETS[selector]),
            'directory': str(directory), 'runtime': str(directory / 'runtime'),
            'test_timeout': '6m', 'resources': 7}
    if selector in UI_CASES:
        plan['ui'] = ui_configuration(selector, directory)
    return plan


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--test-binary', required=True)
    parser.add_argument('--run', required=True)
    parser.add_argument('--directory', required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    try:
        plan = configuration(args.test_binary, args.run, args.directory)
    except (ValueError, OSError):
        print('STOP invalid root-chain inputs', file=sys.stderr)
        return 1
    if args.check:
        print(json.dumps(plan, sort_keys=True))
        return 0
    directory = Path(plan['directory'])
    directory.mkdir(mode=0o700)
    runtime = Path(plan['runtime'])
    runtime.mkdir(mode=0o700)
    with (directory / 'request.json').open('x') as stream:
        os.chmod(stream.name, 0o600)
        json.dump(plan, stream, sort_keys=True)
    env = os.environ.copy()
    for name in ('AGENTEAM_OBJECT_FIXTURE', 'AGENTEAM_OUTBOUND_FIXTURE',
                 'AGENTEAM_PG_FIXTURE', 'AGENTEAM_PG_UNSUPPORTED_FIXTURE'):
        env.pop(name, None)
    env.update({'AGENTEAM_GO': str(GO), 'GOTOOLCHAIN': 'local',
                'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTELEMETRY': 'off',
                'GOFLAGS': '-mod=readonly -p=2',
                'AGENTEAM_MINIO_BINARY': str(MINIO),
                'AGENTEAM_FIXTURE_TEST_BINARY': plan['binary'],
                'AGENTEAM_FIXTURE_TEST_CWD': plan['cwd'],
                'AGENTEAM_FIXTURE_OWNED_RECORD': str(directory / 'owned.json'),
                'TMPDIR': str(runtime), 'GOTMPDIR': str(runtime)})
    if 'ui' in plan:
        for path in plan['ui'].values():
            Path(path).mkdir(mode=0o700)
        # One input identity binds the fixture's safe-body sidecars to the same
        # sources, binary and private assets already frozen by the supervisor.
        digest = hashlib.sha256()
        for path in input_paths(args.test_binary):
            digest.update(str(path).encode() + b'\0' + sha(path).encode() + b'\n')
        env.update(plan['ui'])
        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),
                    'AGENTEAM_PROJECT_OWNER_WEB_INPUT_HASH': digest.hexdigest()})
    os.chdir(REPOSITORY)
    # No child is started here: the original shell chain replaces this PID.
    # Its nested Go Cmd.Run and shell wait remain the actual child owners.
    os.execve('/bin/sh', ['/bin/sh', 'scripts/test-objects.sh', '--run', args.run], env)


if __name__ == '__main__':
    sys.exit(main())
