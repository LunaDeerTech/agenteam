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
    '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|PendingHistoryAndCausePlans)$': 'tests/objects',
    '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans)$': 'tests/objects',
    '^TestObjectMetadataCleanup(ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans)$': 'tests/objects',
    '^TestObjectMetadataCleanupOldAttemptsAndStopHistory$': 'tests/objects',
    '^TestObjectMetadataCleanupIndexMigration$': 'tests/objects',
    '^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$': 'tests/objects',
    '^TestWorkOwnerRootActual(Command|Reader)Join$': 'internal/central/app',
    '^TestWorkOwnerHTTPProcessRoutingAndPersistence$': 'tests/process',
    '^TestIndependentWorkOwnerRootConfirmationJoin$': 'internal/central/app',
    '^TestProjectVariablesRootActualCallJoin$': 'internal/central/app',
    '^TestProjectVariablesHTTPProcessRoutingAndPersistence$': 'tests/process',
    '^TestIndependentProjectVariablesProcessConfirmationExit$': 'tests/process',
    '^TestIndependentProjectVariablesRootConfirmationForce$': 'internal/central/app',
}


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
    return sorted(paths)


def metadata_cost_inputs():
    # The selected cost cases embed these SQL seeds in the fixed candidate.
    # Include their package helpers as source provenance; old selectors keep
    # the original input_paths closure unchanged.
    paths = set((REPOSITORY / 'tests/objects').glob('*.go'))
    for name in ('metadata_cleanup_project_cost.sql',
                 'metadata_cleanup_skill_cost.sql',
                 'metadata_cleanup_transfer_cost.sql'):
        paths.add(REPOSITORY / 'tests/objects/testdata' / name)
    return sorted(paths)


def metadata_remaining_cost_inputs():
    # The later cost candidate also embeds these three new scenarios. Keep
    # the original three-cost closure and all prior selector inputs intact.
    paths = set(metadata_cost_inputs())
    for name in ('metadata_cleanup_live_cost.sql',
                 'metadata_cleanup_anchor_cost.sql',
                 'metadata_cleanup_pending_cost.sql'):
        paths.add(REPOSITORY / 'tests/objects/testdata' / name)
    return sorted(paths)


def configuration(binary, selector, directory):
    binary, directory = Path(binary), Path(directory)
    if (selector not in TARGETS or not binary.is_absolute() or not binary.is_file()
            or not os.access(binary, os.X_OK) or not directory.is_absolute()
            or directory.exists() or directory.is_symlink()):
        raise ValueError('exact root target and fresh absolute directory required')
    if not GO.is_file() or sha(MINIO) != MINIO_SHA:
        raise ValueError('fixed Go and verified cached MinIO required')
    return {'binary': str(binary.resolve()), 'selector': selector,
            'cwd': str(REPOSITORY / TARGETS[selector]),
            'directory': str(directory), 'runtime': str(directory / 'runtime'),
            'test_timeout': '6m', 'resources': 7}


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
    os.chdir(REPOSITORY)
    # No child is started here: the original shell chain replaces this PID.
    # Its nested Go Cmd.Run and shell wait remain the actual child owners.
    os.execve('/bin/sh', ['/bin/sh', 'scripts/test-objects.sh', '--run', args.run], env)


if __name__ == '__main__':
    sys.exit(main())
