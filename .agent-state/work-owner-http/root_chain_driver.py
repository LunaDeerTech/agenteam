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
    '^TestModelTextRuntimePersistentWire$': 'tests/model',
    '^TestKnowledgePlainTextParserIntegration$': 'tests/knowledge',
    '^TestProjectSecretVariablesDefaultRoot$': 'internal/central/app',
    '^TestKnowledgeOwnerReadWeb$': 'internal/central/app',
    '^TestKnowledgeSkillsDefaultRootComposition$': 'internal/central/app',
    '^TestKnowledgeOwnerContentHTTP(CurrentBytes|CurrentAuthority|ReaderOwnership|ReadTransactions)$': 'tests/knowledge',
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
    '^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$': 'tests/skills',
    '^TestSkillLifecycleCleanupHistoricalAttempts$': 'tests/skills',
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
    # The cleanup fixture invokes these exact shared helpers and the original
    # COMMIT-frame proxy; they are inputs even though the tests are precompiled.
    for name in ('fixture_test.go', 'object_publication_test.go',
                 'lifecycle_stop_test.go', 'owner_read_test.go',
                 'commit_recovery_test.go', 'lifecycle_cleanup_fixture_test.go',
                 'lifecycle_cleanup_test.go', 'lifecycle_cleanup_unknown_test.go',
                 'lifecycle_cleanup_history_test.go',
                 'lifecycle_cleanup_history_proxy_test.go'):
        paths.add(REPOSITORY / 'tests/skills' / name)
    paths.add(REPOSITORY / '.agent-state/project-variables-independent/commitproxy/proxy.go')
    return sorted(paths)


def model_runtime_inputs(binary):
    # The fixed candidate compiles every same-package Model fixture. The two
    # real wire probes and their declared method remain mandatory inputs.
    paths = set(input_paths(binary)) | set((REPOSITORY / 'tests/model').glob('*.go'))
    paths.update(REPOSITORY / name for name in (
        'tests/model/runtime_persistence_test.go', 'tests/model/runtime_native_test.go',
        '.agent-state/model-text-runtime/first-wire-method.md',
        '.agent-state/model-text-runtime/entry-controls.py'))
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p for p in paths):
        raise ValueError('regular complete Model Runtime inputs required')
    return sorted(paths)


def parser_inputs(binary):
    # The fixed integration binary uses every same-package Knowledge helper.
    paths = set(input_paths(binary)) | set((REPOSITORY / 'tests/knowledge').glob('*.go'))
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p for p in paths):
        raise ValueError('regular original parser inputs required')
    return sorted(paths)


def root_composition_inputs():
    # Freeze the precompiled app package's same-package fixtures as provenance.
    return sorted((REPOSITORY / 'internal/central/app').glob('*.go'))


KNOWLEDGE_UI = '^TestKnowledgeOwnerReadWeb$'
KNOWLEDGE_NODE = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node')
KNOWLEDGE_PYTHON = Path('/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3')
KNOWLEDGE_ENV = ('AGENTEAM_KNOWLEDGE_OWNER_WEB_DIST',
                 'AGENTEAM_KNOWLEDGE_OWNER_WEB_EVIDENCE',
                 'AGENTEAM_KNOWLEDGE_OWNER_WEB_SCHEMA_PYTHON',
                 'AGENTEAM_KNOWLEDGE_OWNER_WEB_CASE')


def knowledge_ui_assets():
    owned = (REPOSITORY / 'output/ai/knowledge-owner-ui').resolve()
    dist = Path(os.environ.get(KNOWLEDGE_ENV[0], ''))
    if (not dist.is_absolute() or dist != dist.resolve() or not dist.is_relative_to(owned)
            or not (dist / 'index.html').is_file()):
        raise ValueError('owned frozen Knowledge dist required')
    assets = list(dist.rglob('*'))
    if any(p.is_symlink() for p in assets):
        raise ValueError('Knowledge assets must not alias another source')
    return sorted(p for p in assets if p.is_file())


def knowledge_ui_environment():
    values = tuple(os.environ.get(key, '') for key in KNOWLEDGE_ENV)
    if (values[2] != str(KNOWLEDGE_PYTHON) or values[3] != 'read'
            or not KNOWLEDGE_PYTHON.is_file() or not os.access(KNOWLEDGE_PYTHON, os.X_OK)
            or not KNOWLEDGE_NODE.is_file() or not os.access(KNOWLEDGE_NODE, os.X_OK)):
        raise ValueError('exact Knowledge case and fixed local interpreters required')
    return values


def knowledge_ui_configuration(directory):
    values = knowledge_ui_environment()
    knowledge_ui_assets()
    evidence = Path(values[1])
    owned = (REPOSITORY / 'output/ai/knowledge-owner-ui').resolve()
    if (len(str(directory / 'runtime')) > 45 or not evidence.is_absolute()
            or evidence != evidence.resolve() or evidence.exists() or evidence.is_symlink()
            or not evidence.parent.is_dir() or not evidence.parent.is_relative_to(owned)
            or evidence.is_relative_to(Path(values[0]))):
        raise ValueError('fresh owned Knowledge evidence and short runtime required')
    return dict(zip(KNOWLEDGE_ENV, values))


def knowledge_ui_inputs(binary):
    knowledge_ui_environment()
    paths = set(input_paths(binary)) | set(root_composition_inputs()) | set(knowledge_ui_assets())
    harness = REPOSITORY / 'tests/account-captcha-web'
    paths.update(harness / name for name in ('knowledge-owner-read.config.js', 'package.json', 'package-lock.json',
        'e2e/knowledge-owner-read.spec.ts', 'e2e/knowledge-owner-read.native.ts'))
    for name in ('@playwright/test', 'playwright', 'playwright-core'):
        package = harness / 'node_modules' / name
        paths.add(package / 'package.json')
        paths.update(p for p in package.rglob('*') if p.is_file() or p.is_symlink())
    paths.add(harness / 'node_modules/@playwright/test/cli.js')
    paths.update(p for p in (REPOSITORY / 'web/src').rglob('*')
                 if p.is_file() and '.spec.' not in p.name and '.test.' not in p.name)
    paths.update(REPOSITORY / 'web' / name for name in ('package.json', 'package-lock.json',
        'node_modules/typescript/package.json', 'node_modules/typescript/lib/typescript.js'))
    paths.update(REPOSITORY / 'api/openapi' / name for name in ('common.json', 'knowledge-owner.json', 'knowledge-content.json'))
    # Freeze the actual launcher and browser it execs, without enumerating the OS.
    paths.update({KNOWLEDGE_NODE, KNOWLEDGE_PYTHON.resolve(), Path('/usr/bin/chromium'), Path('/usr/lib/chromium/chromium')})
    paths.update(p for p in Path('/etc/chromium.d').glob('*') if p.is_file() or p.is_symlink())
    if any(not p.is_file() or p.is_symlink() for p in paths):
        raise ValueError('regular complete Knowledge inputs required')
    for name in ('@playwright/test', 'playwright', 'playwright-core'):
        if json.loads((harness / 'node_modules' / name / 'package.json').read_text())['version'] != '1.56.1':
            raise ValueError('locked Playwright 1.56.1 required')
    return sorted(paths)


def knowledge_ui_input_hash(binary):
    digest = hashlib.sha256()
    for path in knowledge_ui_inputs(binary):
        digest.update(str(path).encode() + b'\0' + sha(path).encode() + b'\n')
    return digest.hexdigest()


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
    plan = {'binary': str(binary.resolve()), 'selector': selector,
            'cwd': str(REPOSITORY / TARGETS[selector]),
            'directory': str(directory), 'runtime': str(directory / 'runtime'),
            'test_timeout': '6m', 'resources': 7}
    if selector == KNOWLEDGE_UI:
        plan['knowledge_ui'] = knowledge_ui_configuration(directory)
    return plan


def prepare_history_go_environment(directory, env):
    # GOTELEMETRY is a read-only go env value. The fixed Go toolchain reads
    # this mode file before starting its optional telemetry child. Establish
    # task-owned configuration before the shell's first go env/build call.
    config = directory / 'go-config'
    telemetry = config / 'go' / 'telemetry'
    for path in (config, config / 'go', telemetry):
        path.mkdir(mode=0o700)
    with (telemetry / 'mode').open('x') as stream:
        os.chmod(stream.name, 0o600)
        stream.write('off\n')
    env['XDG_CONFIG_HOME'] = str(config)
    for name in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD'):
        env.pop(name, None)


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
    if args.run == '^TestSkillLifecycleCleanupHistoricalAttempts$':
        prepare_history_go_environment(directory, env)
    if args.run == '^TestModelTextRuntimePersistentWire$':
        prepare_history_go_environment(directory, env)
    if args.run == KNOWLEDGE_UI:
        prepare_history_go_environment(directory, env)
        ui = plan['knowledge_ui']
        Path(ui['AGENTEAM_KNOWLEDGE_OWNER_WEB_EVIDENCE']).mkdir(mode=0o700)
        env.update(ui)
        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),
                    'AGENTEAM_KNOWLEDGE_OWNER_WEB_INPUT_HASH': knowledge_ui_input_hash(args.test_binary),
                    'PATH': str(KNOWLEDGE_NODE.parent) + os.pathsep + env.get('PATH', '')})
    os.chdir(REPOSITORY)
    # No child is started here: the original shell chain replaces this PID.
    # Its nested Go Cmd.Run and shell wait remain the actual child owners.
    os.execve('/bin/sh', ['/bin/sh', 'scripts/test-objects.sh', '--run', args.run], env)


if __name__ == '__main__':
    sys.exit(main())
