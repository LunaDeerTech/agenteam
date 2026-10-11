#!/usr/bin/env python3
"""One exact D11 PG top, actual child wait, then bounded host TCP tail.

Example: python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
 --driver /absolute/pg-only-driver --binary /absolute/work.test \
 --run '^TestTaskPlanningMigration$' --output /absolute/existing/output-dir
The output directory is reusable; each run/nonce directory must be new.
"""
import argparse
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import uuid


SECRET_STORAGE_CORE = '^TestSecretVariableStorageSQL(ReplayAndEffects|AtomicAuditAndOwnerRollback|ClosedConstraints)$'
SECRET_STORAGE_MAINTENANCE = '^TestSecretVariableStorageSQLRotationDeletedOwnerAndCleanup$'
SECRET_STORAGE_RECOVERY_WRITE = '^TestSecretVariableStorageSQL(CommitUnknown|NonceUnknown)$'
SECRET_STORAGE_RECOVERY_STATE = '^TestSecretVariableStorageSQL(MaintenanceUnknown|Concurrency)$'
SECRET_STORAGE_RECOVERY_CASES = {
    SECRET_STORAGE_RECOVERY_WRITE: {
        'TestSecretVariableStorageSQLCommitUnknown',
        *('TestSecretVariableStorageSQLCommitUnknown/' + name for name in ('before', 'after', 'pending')),
        'TestSecretVariableStorageSQLNonceUnknown',
    },
    SECRET_STORAGE_RECOVERY_STATE: {
        'TestSecretVariableStorageSQLMaintenanceUnknown',
        *('TestSecretVariableStorageSQLMaintenanceUnknown/' + name for name in
          ('rotation-refresh', 'rotation-no-refresh', 'cleanup')),
        'TestSecretVariableStorageSQLConcurrency',
        *('TestSecretVariableStorageSQLConcurrency/' + name for name in
          ('same-intent', 'changed-value', 'stale-credential-version')),
    },
}
SECRET_STORAGE_CASES = {
    SECRET_STORAGE_CORE: {
        'TestSecretVariableStorageSQLReplayAndEffects',
        'TestSecretVariableStorageSQLAtomicAuditAndOwnerRollback',
        'TestSecretVariableStorageSQLClosedConstraints',
        *('TestSecretVariableStorageSQLAtomicAuditAndOwnerRollback/' + name for name in
          ('owner-tail', 'audit-without-witness', 'audit-wrong-payload-kind',
           'audit-wrong-payload-owner', 'missing-lock')),
        *('TestSecretVariableStorageSQLClosedConstraints/' + name for name in
          ('v4-variable', 'effect-create-mismatch', 'create-expected-present',
           'command-digest', 'duplicate-command', 'duplicate-payload',
           'kind3-system', 'kind3-length', 'purpose-system')),
    },
    SECRET_STORAGE_MAINTENANCE: {'TestSecretVariableStorageSQLRotationDeletedOwnerAndCleanup'},
}


def secret_storage_inputs(driver, binary, recovery=False):
    # Precompiled test and driver; no TestMain rebuild or Go metadata process.
    # Freeze the selected storage/fixture/migration sources as well as the two
    # executable artifacts. This is scoped input evidence, not a whole-repo hash.
    root = Path(__file__).resolve().parents[2]
    output = root / 'output/ai/secret-variable-storage'
    expected_driver = output / ('pg-only-recovery-driver' if recovery else 'pg-only-driver')
    expected_binary = output / ('secret-variable-storage-recovery.test' if recovery else 'secret-variable-storage-sql-reviewed.test')
    if driver != expected_driver or binary != expected_binary:
        raise ValueError('exact Secret storage artifacts required')
    paths = {driver, binary, Path(__file__).resolve(), root / 'go.mod', root / 'go.sum',
             root / '.agent-state/task-planning-recovery/pg_only_driver.go'}
    for name in ('secret_variable_storage_fixture_test.go', 'secret_variable_storage_test.go',
                 'secret_variable_storage_maintenance_test.go', 'audit_common_test.go',
                 'secret_common_test.go', 'secret_project_audit_fixture_test.go', 'secret_rotation_test.go'):
        paths.add(root / 'tests/security' / name)
    if recovery:
        for name in ('secret_variable_storage_recovery_fixture_test.go',
                     'secret_variable_storage_recovery_test.go',
                     'secret_variable_storage_recovery_nonce_test.go',
                     'secret_variable_storage_recovery_concurrency_test.go',
                     'secret_variable_storage_recovery_maintenance_test.go',
                     'audit_proxy_test.go', 'outbound_reload_test.go'):
            paths.add(root / 'tests/security' / name)
    for name in ('secret', 'audit', 'foundation', 'postgres', 'cursor',
                 'identity/contract', 'projectvariable/contract'):
        paths.update(p for p in (root / 'internal/central' / name).rglob('*.go')
                     if not p.name.endswith('_test.go'))
    paths.update((root / 'tests/testsupport/postgres').glob('*.go'))
    paths.update((root / 'db/migrations').glob('*.go'))
    paths.update((root / 'db/migrations').glob('*.sql'))
    for path in paths:
        if path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('non-regular Secret storage input')
    return tuple(sorted(paths))


def observe_secret_storage(log_path, log, selector):
    expected = (SECRET_STORAGE_CASES | SECRET_STORAGE_RECOVERY_CASES)[selector]
    try:
        raw = log_path.read_text()
    except (OSError, UnicodeDecodeError):
        log.write('SECRET_STORAGE exact_cases=False log_unreadable=True\n')
        return False
    runs = re.findall(r'^=== RUN   (\S+)$', raw, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', raw, re.M)
    passed = [name for state, name in results if state == 'PASS']
    good = (len(runs) == len(expected) and set(runs) == expected
            and len(results) == len(expected) and len(passed) == len(expected)
            and set(passed) == expected)
    log.write(f'SECRET_STORAGE exact_cases={good} run_count={len(runs)} result_count={len(results)}\n')
    return good


SECRET_OWNER_CASES = {
    '^TestSecretVariableOwner(Persistence|CurrentAuthority)$': {
        'TestSecretVariableOwnerPersistence', 'TestSecretVariableOwnerCurrentAuthority',
        *('TestSecretVariableOwnerCurrentAuthority/' + name for name in
          ('current-owner-and-cross-project', 'current-owner-loss-hides-original-history',
           'real-archive-after-prepare-rechecks-final-gate', 'revoked-current-session-before-safe-history')),
    },
    '^TestSecretVariableOwner(AtomicFacts|Concurrency)$': {
        'TestSecretVariableOwnerAtomicFacts', 'TestSecretVariableOwnerConcurrency',
        *('TestSecretVariableOwnerAtomicFacts/' + name for name in
          ('after-d10-audit', 'after-outbox', 'after-activity', 'owner-tail')),
        *('TestSecretVariableOwnerConcurrency/' + name for name in
          ('same-key-original-intent', 'same-key-other-value', 'two-keys-update-delete-version', 'ordinary-secret-name')),
    },
    '^TestSecretVariableOwnerCommitRecovery$': {
        'TestSecretVariableOwnerCommitRecovery',
        *('TestSecretVariableOwnerCommitRecovery/' + name for name in
          ('before-forward', 'after-forward', 'pending-outlives-confirmation', 'stop-confirms-actual-join')),
    },
    '^TestSecretVariableOwnerMigration$': {
        'TestSecretVariableOwnerMigration',
        *('TestSecretVariableOwnerMigration/' + name for name in
          ('empty-repeat-and-exact-new-schema', 'ordinary-stored-facts-survive-upgrade',
           'actual-closed-checks-and-deferred-history')),
        *('TestSecretVariableOwnerMigration/actual-closed-checks-and-deferred-history/' + name for name in
          ('secret-plaintext', 'secret-null-internal-version', 'ordinary-null-value',
           'completed-without-audit', 'audit-extra-material-field')),
    },
}


def secret_owner_inputs(driver, binary):
    # Fixed compiled artifacts plus their local source/embedded inputs. No
    # rebuild, Go metadata process, package import or resource setup here.
    root = Path(__file__).resolve().parents[2]
    output = root / 'output/ai/secret-variable-owner-service'
    if driver != output / 'pg-only-owner-driver' or binary != output / 'secret-variable-owner.test':
        raise ValueError('exact Secret Owner artifacts required')
    paths = {driver, binary, Path(__file__).resolve(), root / 'go.mod', root / 'go.sum',
             root / '.agent-state/task-planning-recovery/pg_only_driver.go',
             root / 'internal/central/account/assets/weak-passwords.json'}
    # The actual package also compiles ordinary fixtures/proxy/helper files.
    # Include those helpers and all local production dependencies; not a repo hash.
    for name in ('account', 'account/contract', 'accountmail', 'audit', 'audit/contract',
                 'cursor', 'event/contract', 'foundation', 'httpapi', 'identity/contract',
                 'object', 'object/contract', 'outbound', 'outbox', 'outbox/contract',
                 'postgres', 'project', 'project/contract', 'projectvariable',
                 'projectvariable/contract', 'projectvariable/http', 'recoverylog',
                 'secret', 'secret/contract', 'work', 'work/contract'):
        paths.update(p for p in (root / 'internal/central' / name).glob('*.go')
                     if not p.name.endswith('_test.go'))
    for name in ('tests/projectvariable', 'tests/testsupport/postgres', 'db/migrations'):
        paths.update((root / name).glob('*.go'))
    paths.update((root / 'db/migrations').glob('*.sql'))
    for path in paths:
        if path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('non-regular Secret Owner input')
    return tuple(sorted(paths))


def observe_secret_owner(log_path, log, selector):
    expected = SECRET_OWNER_CASES[selector]
    try:
        raw = log_path.read_text()
    except (OSError, UnicodeDecodeError):
        log.write('SECRET_OWNER exact_cases=False log_unreadable=True\n')
        return False
    runs = re.findall(r'^=== RUN   (\S+)$', raw, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', raw, re.M)
    passed = [name for state, name in results if state == 'PASS']
    good = (len(runs) == len(expected) and set(runs) == expected
            and len(results) == len(expected) and len(passed) == len(expected)
            and set(passed) == expected)
    log.write(f'SECRET_OWNER exact_cases={good} run_count={len(runs)} result_count={len(results)}\n')
    return good


METADATA_ROOT = '^TestAgentConfigurationMetadata$'
METADATA_CASES = frozenset({
    'TestAgentConfigurationMetadata',
    'TestAgentConfigurationMetadata/normal-metadata',
    'TestAgentConfigurationMetadata/current-and-stale',
    'TestAgentConfigurationMetadata/caller-rollback',
})


METADATA_GROUPS = {
    '^TestSchedulerReviewDispatch$': frozenset({
        'TestSchedulerReviewDispatch',
        'TestSchedulerReviewDispatch/phase-isolated-review-execution',
        'TestSchedulerReviewDispatch/review-failure-atomic-block-and-replay',
    }),
    '^TestTaskHumanReview$': frozenset({
        'TestTaskHumanReview',
        'TestTaskHumanReview/completed-work-review-and-done',
        'TestTaskHumanReview/review-rework-late-transaction-rollback-and-replay',
    }),
    '^TestSchedulerRelaunch$': frozenset({
        'TestSchedulerRelaunch',
        'TestSchedulerRelaunch/cooldown-restart-and-new-execution',
        'TestSchedulerRelaunch/relaunch-failure-atomic-block-and-replay',
    }),
    '^TestSchedulerExecution$': frozenset({
        'TestSchedulerExecution',
        'TestSchedulerExecution/historical-association-terminal-and-dedup',
        'TestSchedulerExecution/asynchronous-todo-and-cancel-join',
    }),
    '^TestExecutionFirstRound$': frozenset({
        'TestExecutionFirstRound',
        'TestExecutionFirstRound/completed-one-turn',
        'TestExecutionFirstRound/start-receipt-loss-recovery',
        'TestExecutionFirstRound/cancel-joins-current-call',
    }),
    '^TestModelAgentRetryRuntime$': frozenset({
        'TestModelAgentRetryRuntime',
        'TestModelAgentRetryRuntime/retry-success-and-execution-lease-reuse',
        'TestModelAgentRetryRuntime/cancel-prevents-next-attempt',
        'TestModelAgentRetryRuntime/nonretryable-single-failure',
    }),
    '^TestExecutionTaskContext$': frozenset({
        'TestExecutionTaskContext',
        'TestExecutionTaskContext/frozen-input-after-owner-updates',
    }),
    '^TestExecutionModelEnvironmentCapture$': frozenset({
        'TestExecutionModelEnvironmentCapture',
        'TestExecutionModelEnvironmentCapture/complete-input-unknown-recovery',
        'TestExecutionModelEnvironmentCapture/missing-provider-rolls-back',
    }),
    '^TestExecutionCaptureProviders$': frozenset({
        'TestExecutionCaptureProviders',
        'TestExecutionCaptureProviders/real-providers-roll-back-with-unbound-snapshot',
    }),
    '^TestSchedulerProjectRunner$': frozenset({
        'TestSchedulerProjectRunner',
        'TestSchedulerProjectRunner/ordered-todo-and-serial-launch',
        'TestSchedulerProjectRunner/paused-pending-recovery-and-join',
    }),
    '^TestSchedulerBoundedRetry$': frozenset({
        'TestSchedulerBoundedRetry',
        'TestSchedulerBoundedRetry/temporary-due-original-key-created',
        'TestSchedulerBoundedRetry/temporary-exhaustion-technical-blocker',
    }),
    '^TestSchedulerRetryBinding$': frozenset({
        'TestSchedulerRetryBinding',
        'TestSchedulerRetryBinding/config-bound-claim-and-real-lock-timeout',
        'TestSchedulerRetryBinding/legacy-null-policy-stays-unbound',
    }),
    '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': frozenset({
        'TestTaskTechnicalResolutionHTTP',
        'TestTaskTechnicalResolutionHTTP/resolve-to-todo-lookup-replay',
        'TestTaskTechnicalResolutionAtomic',
        'TestTaskTechnicalResolutionAtomic/late-transaction-rollback-and-replay',
    }),
    '^TestSchedulerLaunchFinalFailure$': frozenset({
        'TestSchedulerLaunchFinalFailure',
        'TestSchedulerLaunchFinalFailure/title-preserved-technical-blocker-and-replay',
        'TestSchedulerLaunchFinalFailure/late-transaction-rollback-and-settlement',
    }),
    '^TestAgentConfigurationSchema$': frozenset({
        'TestAgentConfigurationSchema',
        'TestAgentConfigurationSchema/fresh-prefix-and-repeat',
        'TestAgentConfigurationSchema/schema-invariants-and-unbound-dependencies',
        'TestAgentConfigurationSchema/upgrade-preserves-facts-and-audit-checks',
    }),
    '^TestAgentRuntimeSchema$': frozenset({
        'TestAgentRuntimeSchema',
        'TestAgentRuntimeSchema/execution-slot-and-unbound-launch',
        'TestAgentRuntimeSchema/human-compatibility-and-agent-origin',
        'TestAgentRuntimeSchema/prefix36-upgrade-and-repeat',
        'TestAgentRuntimeSchema/runtime-attempt-and-terminal',
    }),
    '^TestExecutionPreparation$': frozenset({
        'TestExecutionPreparation',
        'TestExecutionPreparation/current-owner-task-input',
        'TestExecutionPreparation/prefix39-upgrade-and-repeat',
        'TestExecutionPreparation/preparation-claim-and-attempt',
        'TestExecutionPreparation/project-preparation-gate',
    }),
    '^TestAgentConfigurationCreate$': frozenset({
        'TestAgentConfigurationCreate',
        'TestAgentConfigurationCreate/default-create-and-replay',
        'TestAgentConfigurationCreate/final-transaction-rollback',
    }),
    '^TestTaskTransitionHuman$': frozenset({
        'TestTaskTransitionHuman',
        'TestTaskTransitionHuman/assignment-config-and-replay',
        'TestTaskTransitionHuman/final-transaction-rollback',
    }),
    '^TestSchedulerClaim$': frozenset({
        'TestSchedulerClaim',
        'TestSchedulerClaim/final-transaction-rollback',
        'TestSchedulerClaim/start-sprint-claim-and-replay',
    }),
    '^TestSchedulerLaunch$': frozenset({
        'TestSchedulerLaunch',
        'TestSchedulerLaunch/association-failure-lookup-recovery',
        'TestSchedulerLaunch/created-association-and-replay',
    }),
    '^TestSchedulerBusyCompensation$': frozenset({
        'TestSchedulerBusyCompensation',
        'TestSchedulerBusyCompensation/preserve-user-update',
        'TestSchedulerBusyCompensation/rollback-restore-and-replay',
    }),
    '^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': frozenset({
        'TestSchedulerPendingVisit',
        'TestSchedulerPendingVisit/association-rollback-original-lookup',
        'TestSchedulerPendingVisit/paused-enumeration-and-resume',
        'TestTaskHumanHTTP',
        'TestTaskHumanHTTP/owner-csrf-and-new-session-lookup',
        'TestTaskHumanHTTP/transfer-lookup-replay-and-get',
    }),
    '^TestSprintStartHTTP$': frozenset({
        'TestSprintStartHTTP',
        'TestSprintStartHTTP/paused-start-get-lookup-replay',
    }),
    METADATA_ROOT: METADATA_CASES,
    '^TestSkillInstallationPersistentObject$': frozenset({
        'TestSkillInstallationPersistentObject',
        'TestSkillInstallationPersistentObject/install-read-replay',
        'TestSkillInstallationPersistentObject/ordinary-cleanup',
    }),
    '^TestSkillInstallationOwnerHTTP$': frozenset({
        'TestSkillInstallationOwnerHTTP',
        'TestSkillInstallationOwnerHTTP/install-lookup-catalog-and-read',
        'TestSkillInstallationOwnerHTTP/current-owner-and-csrf',
    }),
    '^TestSkillInstallation(PersistentObject|OwnerHTTP)$': frozenset({
        'TestSkillInstallationPersistentObject',
        'TestSkillInstallationPersistentObject/install-read-replay',
        'TestSkillInstallationPersistentObject/ordinary-cleanup',
        'TestSkillInstallationOwnerHTTP',
        'TestSkillInstallationOwnerHTTP/install-lookup-catalog-and-read',
        'TestSkillInstallationOwnerHTTP/current-owner-and-csrf',
    }),
}


def metadata_results(output, selector=METADATA_ROOT):
    cases = METADATA_GROUPS.get(selector)
    if cases is None:
        return False
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    return (len(runs) == len(cases) and set(runs) == cases
            and len(results) == len(cases)
            and all(state == 'PASS' for state, _ in results)
            and {name for _, name in results} == cases
            and len(waits) == 1 and waits[0][1:] == ('0', selector)
            and sum(line.startswith('D03 explicit test actual_wait') for line in output.splitlines()) == 1
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def metadata_same(inputs, args, adapter, selector=METADATA_ROOT):
    try:
        return {str(p): adapter.sha(p) for p in adapter.metadata_inputs(args.binary, selector)} == inputs
    except (OSError, ValueError, TypeError):
        return False


GUARD_ROOT = '^TestProjectLifecycleStopBatchRealGuard$'
GUARD_CASES = frozenset({'TestProjectLifecycleStopBatchRealGuard'})


def guard_results(output):
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    child_lines = [line for line in output.splitlines() if 'ProjectStopBatch child actual_wait' in line]
    child = (re.fullmatch(r'[ \t]+project_phase_recovery_guard_test\.go:[1-9][0-9]*: '
                         r'ProjectStopBatch child actual_wait pid=([1-9][0-9]*) '
                         r'signal=SIGKILL stdout_joined=true', child_lines[0])
             if len(child_lines) == 1 else None)
    return (len(runs) == 1 and set(runs) == GUARD_CASES
            and len(results) == 1 and results[0] == ('PASS', 'TestProjectLifecycleStopBatchRealGuard')
            and len(waits) == 1 and waits[0][1:] == ('0', GUARD_ROOT)
            and sum(line.startswith('D03 explicit test actual_wait') for line in output.splitlines()) == 1
            and child is not None and child[1] != waits[0][0]
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def guard_same(inputs, args, adapter):
    try:
        return {str(p): adapter.sha(p) for p in adapter.guard_inputs(args.binary)} == inputs
    except (OSError, ValueError, TypeError):
        return False


MODEL_RUNTIME = '^TestModelTextRuntimePersistentWire$'
MODEL_RUNTIME_CASES = {'TestModelTextRuntimePersistentWire',
                       'TestModelTextRuntimePersistentWire/json_success',
                       'TestModelTextRuntimePersistentWire/policy_deny'}


def model_runtime_results(output):
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    return (len(runs) == len(MODEL_RUNTIME_CASES) and set(runs) == MODEL_RUNTIME_CASES
            and len(results) == len(MODEL_RUNTIME_CASES)
            and all(state == 'PASS' for state, _ in results)
            and {name for _, name in results} == MODEL_RUNTIME_CASES
            and len(waits) == 1 and waits[0][1:] == ('0', MODEL_RUNTIME)
            and len(re.findall(r'^D03 explicit test actual_wait ', output, re.M)) == 1
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def model_runtime_same(inputs, args, adapter):
    try:
        return {str(p): adapter.sha(p) for p in adapter.model_runtime_inputs(args.binary)} == inputs
    except (OSError, ValueError, TypeError):
        return False


PARSER_PG = '^TestKnowledgePlainTextParserIntegration$'
PARSER_CASES = frozenset({
    'TestKnowledgePlainTextParserIntegration',
    'TestKnowledgePlainTextParserIntegration/full_current_bytes_after_actual_close',
    'TestKnowledgePlainTextParserIntegration/partial_and_nonplain_rejected',
    'TestKnowledgePlainTextParserIntegration/foreign_owner_produces_no_parser_input',
})


def parser_results(output):
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    return (len(runs) == len(PARSER_CASES) and set(runs) == PARSER_CASES
            and len(results) == len(PARSER_CASES)
            and all(state == 'PASS' for state, _ in results)
            and {name for _, name in results} == PARSER_CASES
            and len(waits) == 1 and waits[0][1:] == ('0', PARSER_PG)
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def parser_same(inputs, args, adapter):
    try:
        paths = set(adapter.parser_inputs(args.binary))
        return (set(inputs) == {str(p) for p in paths}
                and all(p.is_file() and not p.is_symlink()
                        and p.resolve(strict=True) == p
                        and adapter.sha(p) == inputs[str(p)] for p in paths))
    except (OSError, ValueError, TypeError):
        return False


SECRET_ROOT = '^TestProjectSecretVariablesDefaultRoot$'


def secret_root_results(output):
    top = 'TestProjectSecretVariablesDefaultRoot'
    expected = {top, top + '/existing-project-protocol-and-routing',
                top + '/stop-drain-original-calls'}
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    return (len(runs) == len(expected) and set(runs) == expected
            and len(results) == len(expected)
            and all(state == 'PASS' for state, _ in results)
            and {name for _, name in results} == expected
            and len(waits) == 1 and waits[0][1:] == ('0', SECRET_ROOT)
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


KNOWLEDGE_UI = '^TestKnowledgeOwnerReadWeb$'


def knowledge_ui_results(output):
    top = 'TestKnowledgeOwnerReadWeb'
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=(\S+)$', output, re.M)
    browser = re.findall(r'^\s+\S+\.go:[0-9]+: Knowledge Node actual_wait pid=([1-9][0-9]*) success=(true|false)$', output, re.M)
    return (runs == [top] and results == [('PASS', top)]
            and len(waits) == 1 and waits[0][1:] == ('0', KNOWLEDGE_UI)
            and len(browser) == 1 and browser[0][1] == 'true'
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def knowledge_ui_same(inputs, args, adapter):
    try:
        paths = adapter.knowledge_ui_inputs(args.binary)
        return (adapter.knowledge_ui_environment() == args.knowledge_ui_environment
                and {str(p): adapter.sha(p) for p in paths} == inputs)
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        return False


def knowledge_ui_reap_exited(log):
    # Accepted Work UI pre-reap: one finite snapshot, actual WNOHANG waits.
    # Live or not-yet-waitable children still reach the original survivor FAIL.
    success = True
    for _ in range(len(descendants(os.getpid()))):
        try:
            pid, status = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:
            return success
        if pid == 0:
            return success
        log.write(f'SUPERVISOR knowledge_ui_adopted_actual_wait pid={pid} status={status}\n')
        success = success and status == 0
    return success


def root_composition_results(output):
    selector = '^TestKnowledgeSkillsDefaultRootComposition$'
    wanted = 'TestKnowledgeSkillsDefaultRootComposition'
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    results = re.findall(r'^[ \t]*--- (PASS|FAIL|SKIP): (\S+) \([^()\r\n]*\)$', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector='
                       + re.escape(selector) + r'$', output, re.M)
    return (runs == [wanted] and results == [('PASS', wanted)]
            and len(waits) == 1 and waits[0][1] == '0'
            and re.search(r'^FAIL(?:\s|$)', output, re.M) is None)


def root_composition_same(inputs, args, adapter):
    try:
        paths = set(adapter.input_paths(args.binary)) | set(adapter.root_composition_inputs())
        return (set(inputs) == {str(p) for p in paths}
                and all(p.is_file() and not p.is_symlink() and adapter.sha(p) == inputs[str(p)] for p in paths))
    except (OSError, ValueError, TypeError):
        return False


def budgets(root_chain):
    # Root: original Go test 360s + readiness 75s + fixture cleanup 55s +
    # build/scheduling allowance 50s. The separate 60s TERM grace allows the
    # original three owners' bounded cleanup; it does not extend a Go test.
    return (540, 60) if root_chain else (123, 3)


CONTENT_PG = '^TestKnowledgeOwnerContentHTTP(CurrentBytes|CurrentAuthority|ReaderOwnership|ReadTransactions)$'
CONTENT_NATIVE = '^TestContentHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$'
CONTENT_GROUPS = {
    CONTENT_PG: {
        'TestKnowledgeOwnerContentHTTPCurrentBytes': ('utf8_slices_default_head_and_zero_business_facts', 'default_and_maximum_are_utf8_byte_limits', 'real_pdf_docx_have_no_readable_provider', 'deleted_410_missing_foreign_404_and_bodyless_head'),
        'TestKnowledgeOwnerContentHTTPCurrentAuthority': ('real_account_before_query_and_safe_rejection', 'new_actual_login_then_formal_logout', 'uninitialized_archived_and_deleting_read_gate', 'owner_changed_sql_fact_requires_new_authority'),
        'TestKnowledgeOwnerContentHTTPReaderOwnership': ('current_owner_after_real_object_open', 'current_deleted_after_real_object_open', 'real_eof_and_d05_close_do_not_finish_held_consumer'),
        'TestKnowledgeOwnerContentHTTPReadTransactions': ('commit_not_forwarded', 'commit_applied_ack_lost', 'original_select_cancelled_and_transaction_retired'),
    },
    CONTENT_NATIVE: {
        'TestContentHTTPNativeDeadlines': ('read-natural', 'earlier-parent'),
        'TestContentHTTPNativeKeepAliveAndClose': ('cleared-deadline-keeps-real-connection', 'real-body-close-error-aborts-before-response'),
        'TestContentHTTPNativeBackpressureAndDisconnect': ('content-write-natural-deadline', 'disconnect-cancels-actual-library-tail'),
    },
}


def content_schema_python():
    raw = os.environ.get('AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON', '')
    path = Path(raw)
    if not raw or not path.is_absolute() or not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError('explicit local Schema interpreter required')
    return path.resolve(strict=True)


def content_inputs(selector=None):
    root = Path(__file__).resolve().parents[2]
    paths = {Path(__file__).resolve(), root / '.agent-state/work-owner-http/root_chain_driver.py',
             root / '.agent-state/work-owner-http/native_driver.go',
             root / '.agent-state/knowledge-content-http/schema-controls.py',
             root / 'api/openapi/knowledge-content.json', root / 'api/openapi/common.json',
             root / 'go.mod', root / 'go.sum', Path('/workspace/toolchains/go1.27.1/bin/go'),
             root / 'internal/central/account/assets/weak-passwords.json',
             root / 'internal/central/skill/builtin/add-skills/v1/SKILL.md'}
    for directory in ('internal', 'cmd', 'tests/testsupport'):
        paths.update(p for p in (root / directory).rglob('*.go') if not p.name.endswith('_test.go'))
    paths.update((root / 'internal/central/knowledge/contenthttp').glob('*.go'))
    paths.update((root / 'tests/knowledge').glob('*.go'))
    paths.update((root / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
    paths.update((root / 'db/migrations').glob('*.go'))
    paths.update((root / 'db/migrations').glob('*.sql'))
    if selector == CONTENT_PG:
        paths.add(content_schema_python())
    return sorted(paths)


def content_exact(path, selector):
    try:
        output = path.read_text()
    except (OSError, UnicodeError):
        return False
    groups = CONTENT_GROUPS[selector]
    expected = set(groups) | {parent + '/' + child for parent, children in groups.items() for child in children}
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    passes = re.findall(r'^\s*--- PASS: (\S+) \(', output, re.M)
    return (len(runs) == len(expected) and set(runs) == expected
            and len(passes) == len(expected) and set(passes) == expected
            and re.search(r'^(?:FAIL(?:\s|$)|\s*--- (?:FAIL|SKIP):)', output, re.M) is None)


def content_root(directory, log, path, selector):
    try:
        observed = observe_root_chain(directory, log, path, selector)
        output = path.read_text()
        waits = re.findall(r'^D03 explicit test actual_wait pid=([1-9][0-9]*) code=(-?[0-9]+) selector=' + re.escape(selector) + r'$', output, re.M)
        observed = observed and len(waits) == 1 and waits[0][1] == '0'
    except (OSError, UnicodeError):
        observed = False
    exact = content_exact(path, selector)
    log.write(f'ROOT content_exact={exact} original_wait={observed}\n')
    return observed and exact


def content_native(directory, log, path, selector):
    good = False
    try:
        output = path.read_text()
        manifest = directory / 'owned.json'
        if manifest.is_symlink() or manifest.stat().st_mode & 0o777 != 0o600 or manifest.stat().st_size > 4096:
            raise ValueError('invalid native owned record')
        record = json.loads(manifest.read_text())
        pid = record.get('child_pid')
        if set(record) != {'kind', 'child_pid'} or record['kind'] != 'work-http-native' or type(pid) is not int or pid <= 0:
            raise ValueError('invalid native child identity')
        starts = re.findall(r'^CHILD pid=([1-9][0-9]*) selector=' + re.escape(selector) + r' kind=native-http$', output, re.M)
        waits = re.findall(r'^CHILD actual_wait pid=([1-9][0-9]*) state=exit status (\d+)$', output, re.M)
        terminals = re.findall(r'^DRIVER terminal exit=0 elapsed=\d+\.\d+s child_started=true actual_child_wait=true private_removed=true$', output, re.M)
        runtime = re.findall(r'^NATIVE runtime_empty=true actual_child_wait=true$', output, re.M)
        tmp = directory / 'tmp'
        good = (starts == [str(pid)] and waits == [(str(pid), '0')] and len(terminals) == 1
                and len(runtime) == 1 and not tmp.exists() and not tmp.is_symlink())
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        pass
    exact = content_exact(path, selector)
    log.write(f'NATIVE content_exact={exact} original_wait_private={good}\n')
    return good and exact


def content_same(inputs, args, adapter):
    # Re-enumerate this exact branch's closure: a newly added or removed source
    # is drift even if every originally hashed file is otherwise unchanged.
    try:
        if args.run == CONTENT_PG and os.environ.get('AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON') != args.content_schema_python:
            return False
        paths = {p.resolve() for p in content_inputs(args.run)}
        paths.update(p.resolve() for p in (adapter.input_paths(args.binary) if adapter is not None else (args.driver, args.binary)))
        return (paths == {Path(p) for p in inputs}
                and all(p.is_file() and not p.is_symlink() and hashlib.sha256(p.read_bytes()).hexdigest() == inputs[str(p)] for p in paths))
    except (OSError, ValueError, TypeError):
        return False


def root_adapter(driver):
    expected = Path(__file__).resolve().parents[2] / '.agent-state/work-owner-http/root_chain_driver.py'
    if driver.resolve() != expected:
        raise ValueError('root mode requires the exact task adapter')
    spec = importlib.util.spec_from_file_location('work_owner_root_adapter', expected)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def root_record(directory):
    path = directory / 'owned.json'
    if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600:
        raise ValueError('invalid owned root manifest permissions')
    raw = path.read_bytes()
    if len(raw) > 16384:
        raise ValueError('owned root manifest too large')
    record = json.loads(raw)
    if (set(record) != {'kind', 'resources', 'directories'}
            or record['kind'] != 'work-owner-root-chain'
            or len(record['resources']) != 7 or len(record['directories']) != 3):
        raise ValueError('incomplete seven-resource manifest')
    wanted = {'agenteam.d05.objectfixture': ['container', 'network'],
              'agenteam.d04.networkfixture': ['container', 'network'],
              'agenteam.d03.fixture': ['container', 'container', 'network']}
    seen, groups = set(), {}
    for item in record['resources']:
        if (set(item) != {'kind', 'id', 'label', 'nonce'}
                or item['label'] not in wanted or item['kind'] not in ('container', 'network')
                or re.fullmatch('[0-9a-f]{64}', item['id']) is None
                or re.fullmatch('[0-9a-f]{32}', item['nonce']) is None or item['id'] in seen):
            raise ValueError('invalid owned root resource identity')
        seen.add(item['id'])
        groups.setdefault(item['label'], []).append(item)
    for label, kinds in wanted.items():
        items = groups.get(label, [])
        if sorted(v['kind'] for v in items) != kinds or len({v['nonce'] for v in items}) != 1:
            raise ValueError('root resource nonce/group mismatch')
    runtime = (directory / 'runtime').resolve()
    for name in record['directories']:
        path = Path(name)
        if not path.is_absolute() or not path.resolve().is_relative_to(runtime) or path.resolve() == runtime:
            raise ValueError('private directory is outside owned runtime')
    return record


def exact_absent(item, timeout):
    # An unavailable daemon is not absence. Require Docker's exact missing-ID
    # diagnostic; never remove, prune or infer ownership from a name prefix.
    args = ['docker', item['kind'], 'inspect', item['id']]
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, timeout=timeout)
    missing = re.compile(r'(?:No such (?:object|container|network):\s*' + re.escape(item['id'])
                         + r'\b|network\s+' + re.escape(item['id']) + r'\s+not found)', re.I)
    return result.returncode != 0 and missing.search(result.stderr) is not None


def skill_cleanup_results(output, selector):
    # Both exact literals have closed bodies. Parent-only success, a skipped
    # child, and a duplicate successful run all fail closed.
    expected = {
        '^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$': {
            'TestSkillLifecycleCleanupPersistence',
            'TestSkillLifecycleCleanupPersistence/current_gate_before_irreversible_release',
            'TestSkillLifecycleCleanupPersistence/actual_physical_audit_and_bounded_history',
            'TestSkillLifecycleCleanupPersistence/last_object_and_skill_anchors_share_original_transaction',
            'TestSkillLifecycleCleanupCommitRecovery',
            'TestSkillLifecycleCleanupCommitRecovery/gate',
            'TestSkillLifecycleCleanupCommitRecovery/last_two_domain_anchors',
        },
        '^TestSkillLifecycleCleanupHistoricalAttempts$': {
            'TestSkillLifecycleCleanupHistoricalAttempts',
            'TestSkillLifecycleCleanupHistoricalAttempts/native_retry_preserves_abandoned_cause',
            'TestSkillLifecycleCleanupHistoricalAttempts/seeded_retained_mapping_history_batches_and_fk_rollback',
        },
    }.get(selector)
    if expected is None:
        return False
    runs = re.findall(r'^=== RUN   (\S+)$', output, re.M)
    passed = re.findall(r'^\s*--- PASS: (\S+) \(', output, re.M)
    waits = re.findall(r'^D03 explicit test actual_wait pid=[1-9][0-9]* code=0 selector='
                       + re.escape(selector) + r'$', output, re.M)
    return (len(runs) == len(passed) == len(expected)
            and set(runs) == set(passed) == expected and len(waits) == 1
            and re.search(r'^\s*--- (?:FAIL|SKIP):', output, re.M) is None)


def observe_root_chain(directory, log, log_path, selector):
    good = True
    try:
        record = root_record(directory)
    except (OSError, ValueError, TypeError, KeyError):
        record = None
        good = False
        log.write('STOP missing or invalid seven-resource ownership record\n')
    # A failed setup never manufactures seven resource retirements. It still
    # reports the actual runtime state and retains the original failure.
    deadline = time.monotonic() + 20
    for round in (1, 2):
        if record is not None:
            for item in record['resources']:
                absent = False
                try:
                    remaining = deadline - time.monotonic()
                    if remaining > 0:
                        absent = exact_absent(item, min(3, remaining))
                except (OSError, subprocess.TimeoutExpired):
                    pass
                log.write(f"ROOT resource_observation={round} kind={item['kind']} id={item['id']} nonce={item['nonce']} absent={absent}\n")
                if not absent: good = False
            private_absent = all(not Path(p).exists() and not Path(p).is_symlink()
                                 for p in record['directories'])
            log.write(f'ROOT private_observation={round} absent={private_absent}\n')
            if not private_absent: good = False
        runtime = directory / 'runtime'
        try:
            empty = runtime.is_dir() and not runtime.is_symlink() and not any(runtime.iterdir())
        except OSError:
            empty = False
        log.write(f'ROOT runtime_observation={round} empty={empty}\n')
        if not empty: good = False
        if round == 1: time.sleep(.1)
    log.flush()
    if selector in (*METADATA_GROUPS, GUARD_ROOT, MODEL_RUNTIME, PARSER_PG, '^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',
                    '^TestSkillLifecycleCleanupHistoricalAttempts$'):
        try:
            output = log_path.read_text()
        except (OSError, UnicodeDecodeError):
            log.write('STOP cleanup result log unavailable or invalid UTF-8\n')
            return False
    else:
        output = log_path.read_text()
    expected = {
        **{key: {name for name in cases if '/' not in name}
           for key, cases in METADATA_GROUPS.items()},
        GUARD_ROOT: {'TestProjectLifecycleStopBatchRealGuard'},
        MODEL_RUNTIME: {'TestModelTextRuntimePersistentWire'},
        PARSER_PG: {'TestKnowledgePlainTextParserIntegration'},
        SECRET_ROOT: {'TestProjectSecretVariablesDefaultRoot'},
        KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'},
        '^TestKnowledgeSkillsDefaultRootComposition$': {'TestKnowledgeSkillsDefaultRootComposition'},
        CONTENT_PG: set(CONTENT_GROUPS[CONTENT_PG]),
        '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|PendingHistoryAndCausePlans)$': {'TestObjectMetadataCleanupLiveTransferAndDownloadPlans', 'TestObjectMetadataCleanupPendingHistoryAndCausePlans'},
        '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans)$': {'TestObjectMetadataCleanupLiveTransferAndDownloadPlans', 'TestObjectMetadataCleanupFinalAnchorForeignKeyPlans', 'TestObjectMetadataCleanupPendingHistoryAndCausePlans'},
        '^TestObjectMetadataCleanup(ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans)$': {'TestObjectMetadataCleanupProjectHistoryPlans', 'TestObjectMetadataCleanupSkillsIndexPlans', 'TestObjectMetadataCleanupTransferAndForeignKeyPlans'},
        '^TestObjectMetadataCleanupOldAttemptsAndStopHistory$': {'TestObjectMetadataCleanupOldAttemptsAndStopHistory'},
        '^TestObjectMetadataCleanupIndexMigration$': {'TestObjectMetadataCleanupIndexMigration'},
        '^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$': {'TestObjectMetadataCleanupBoundedHistoryAndFinalTransaction', 'TestObjectMetadataCleanupFinalCommitUnknown'},
        '^TestWorkOwnerRootActual(Command|Reader)Join$': {'TestWorkOwnerRootActualCommandJoin', 'TestWorkOwnerRootActualReaderJoin'},
        '^TestWorkOwnerHTTPProcessRoutingAndPersistence$': {'TestWorkOwnerHTTPProcessRoutingAndPersistence'},
        '^TestIndependentWorkOwnerRootConfirmationJoin$': {'TestIndependentWorkOwnerRootConfirmationJoin'},
        '^TestProjectVariablesRootActualCallJoin$': {'TestProjectVariablesRootActualCallJoin'},
        '^TestProjectVariablesHTTPProcessRoutingAndPersistence$': {'TestProjectVariablesHTTPProcessRoutingAndPersistence'},
        '^TestIndependentProjectVariablesProcessConfirmationExit$': {'TestIndependentProjectVariablesProcessConfirmationExit'},
        '^TestIndependentProjectVariablesRootConfirmationForce$': {'TestIndependentProjectVariablesRootConfirmationForce'},
        '^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$': {'TestSkillLifecycleCleanupPersistence', 'TestSkillLifecycleCleanupCommitRecovery'},
        '^TestSkillLifecycleCleanupHistoricalAttempts$': {'TestSkillLifecycleCleanupHistoricalAttempts'},
    }.get(selector, set())
    actual = set(re.findall(r'^=== RUN   (Test\w+)$', output, re.M))
    waited = re.search(r'^D03 explicit test actual_wait pid=[1-9][0-9]* code=-?[0-9]+ selector='
                       + re.escape(selector) + r'$', output, re.M) is not None
    log.write(f'ROOT exact_tops={actual == expected} actual_test_wait={waited}\n')
    if selector in METADATA_GROUPS:
        complete = metadata_results(output, selector)
        log.write(f'ROOT metadata_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector == GUARD_ROOT:
        complete = guard_results(output)
        log.write(f'ROOT guard_exact_run_pass_child_wait={complete}\n')
        good = good and complete
    if selector == PARSER_PG:
        complete = parser_results(output)
        log.write(f'ROOT parser_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector == MODEL_RUNTIME:
        complete = model_runtime_results(output)
        log.write(f'ROOT model_runtime_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector == KNOWLEDGE_UI:
        complete = knowledge_ui_results(output)
        log.write(f'ROOT knowledge_ui_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector == SECRET_ROOT:
        complete = secret_root_results(output)
        log.write(f'ROOT secret_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector == '^TestKnowledgeSkillsDefaultRootComposition$':
        complete = root_composition_results(output)
        log.write(f'ROOT composition_exact_run_pass_wait={complete}\n')
        good = good and complete
    if selector in ('^TestSkillLifecycleCleanup(Persistence|CommitRecovery)$',
                    '^TestSkillLifecycleCleanupHistoricalAttempts$'):
        complete = skill_cleanup_results(output, selector)
        log.write(f'ROOT cleanup_exact_run_pass={complete}\n')
        good = good and complete
    return good and actual == expected and waited


def tcp():
    rows = set()
    for name in ('tcp', 'tcp6'):
        for line in Path('/proc/net/' + name).read_text().splitlines()[1:]:
            fields = line.split()
            rows.add((name, fields[1], fields[2], fields[3], fields[9]))
    return rows


def tcp_failure_sample(delta, phase):
    """Project only the last existing observation; never poll or infer ownership."""
    if phase not in ('supervisor', 'outer'):
        raise ValueError('exact TCP failure phase required')
    rows = sorted(delta)
    return {'phase': phase, 'utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
            'total': len(rows), 'truncated': len(rows) > 32,
            'rows': [dict(zip(('family', 'localhex', 'remotehex', 'state', 'inode'), row))
                     for row in rows[:32]]}


def descendants(root):
    parents = {}
    for stat in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = stat.read_text().rsplit(')', 1)[1].split()
            parents[int(stat.parent.name)] = int(fields[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    result = {root}
    changed = True
    while changed:
        added = {pid for pid, parent in parents.items() if parent in result} - result
        changed = bool(added)
        result |= added
    return result - {root}



SKILL_HTTP_PG = '^TestSkillOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$'
SKILL_HTTP_NATIVE = '^TestSkillOwnerHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$'
SKILL_HTTP_CASES = {
    SKILL_HTTP_PG: {
        'TestSkillOwnerReadHTTPMetadata': ('same_current_directory_and_detail_get_head', 'strict_request_and_real_browser_boundary'),
        'TestSkillOwnerReadHTTPCurrentAuthority': ('foreign_owner_and_admin_no_bypass', 'project_gate_and_missing_publication_are_distinct', 'real_new_session_and_logout', 'current_owner_mapping_rechecked', 'archived_read_then_deleting_gate'),
        'TestSkillOwnerReadHTTPTransactions': ('reader_first', 'writer_first', 'actual_query_cancel_and_original_tx_join'),
        'TestSkillOwnerReadHTTPCommitUnknown': ('not_forwarded', 'committed_ack_lost'),
    },
    SKILL_HTTP_NATIVE: {
        'TestSkillOwnerHTTPNativeDeadlines': ('read-natural', 'earlier-parent'),
        'TestSkillOwnerHTTPNativeKeepAliveAndClose': ('cleared-deadline-keeps-real-connection', 'real-body-close-error-aborts-before-response'),
        'TestSkillOwnerHTTPNativeBackpressureAndDisconnect': ('summary-write-natural-deadline', 'disconnect-cancels-actual-library-tail'),
    },
}


def skill_http_inputs(driver, binary, selector):
    # Exact local test inputs plus the runtime Schema producer/JSON/interpreter.
    # The immutable precompiled binary represents its other build dependencies;
    # this is not a repository-wide hash or another build during supervision.
    root = Path(__file__).resolve().parents[2]
    paths = {driver.resolve(), binary.resolve(), Path(__file__).resolve(),
             root / 'go.mod', root / 'go.sum'}
    paths.update(root / 'internal/central/skill/http' / name for name in ('handler.go', 'wire.go', 'io.go', 'native_test.go'))
    paths.update((root / 'internal/central/skill/http').glob('*.go'))
    if selector == SKILL_HTTP_PG:
        paths.add(root / '.agent-state/task-planning-recovery/pg_only_driver.go')
        paths.update(root / 'tests/skills' / name for name in ('owner_http_fixture_test.go', 'owner_http_test.go', 'owner_http_transactions_test.go'))
        paths.update((root / 'tests/skills').glob('*.go'))
        paths.update((root / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
        paths.update((root / 'tests/testsupport/postgres').glob('*.go'))
        paths.update({root / 'internal/central/skill/http/testdata/schema.py',
                      root / 'api/openapi/skill-owner.json', root / 'api/openapi/common.json',
                      Path(sys.executable).resolve()})
    elif selector == SKILL_HTTP_NATIVE:
        paths.add(root / '.agent-state/work-owner-http/native_driver.go')
    else:
        raise ValueError('unknown Skill HTTP selector')
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def skill_http_spawn(driver, binary, selector, directory, log):
    environment = dict(os.environ)
    if selector == SKILL_HTTP_PG:
        environment['AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON'] = str(Path(sys.executable).resolve())
    return subprocess.Popen([str(driver.resolve()), '--test-binary', str(binary.resolve()),
                             '--run', selector, '--directory', str(directory)],
                            stdout=log, stderr=subprocess.STDOUT, env=environment)


def observe_skill_http(directory, log, log_path, selector):
    try:
        output = log_path.read_text()
        expected = {top for top in SKILL_HTTP_CASES[selector]}
        expected.update(top + '/' + sub for top, subs in SKILL_HTTP_CASES[selector].items() for sub in subs)
        runs = re.findall(r'^=== RUN   (Test[^\s]+)$', output, re.M)
        passes = re.findall(r'^\s*--- PASS: (Test[^\s]+) \([^\r\n]*\)$', output, re.M)
        good = (set(runs) == expected and len(runs) == len(expected)
                and set(passes) == expected and len(passes) == len(expected))
        started = re.findall(r'^CHILD pid=([1-9][0-9]*) selector=' + re.escape(selector)
                             + (r' kind=native-http' if selector == SKILL_HTTP_NATIVE else '') + r'$', output, re.M)
        waited = re.findall(r'^CHILD actual_wait pid=([1-9][0-9]*) state=exit status 0$', output, re.M)
        good = (good and len(started) == 1 and waited == started
                and len(re.findall(r'^CHILD pid=', output, re.M)) == 1
                and len(re.findall(r'^CHILD actual_wait ', output, re.M)) == 1
                and len(re.findall(r'^DRIVER terminal ', output, re.M)) == 1
                and re.search(r'^\s*--- (?:FAIL|SKIP): ', output, re.M) is None)
        manifest = directory / 'owned.json'
        if manifest.is_symlink() or manifest.stat().st_mode & 0o777 != 0o600 or manifest.stat().st_size > 16384:
            raise ValueError('invalid Skill HTTP ownership record')
        record = json.loads(manifest.read_bytes())
        if selector == SKILL_HTTP_PG:
            if (set(record) != {'nonce', 'network_id', 'container_id'}
                    or re.fullmatch('[0-9a-f]{32}', record['nonce']) is None
                    or any(re.fullmatch('[0-9a-f]{64}', record[k]) is None for k in ('network_id', 'container_id'))):
                raise ValueError('incomplete Skill HTTP PG identities')
            expected_retire = [(str(n), record['container_id'], record['network_id']) for n in (1, 2)]
            retired = re.findall(r'^RETIRE observation=([12]) exact_container=([0-9a-f]{64}) exact_network=([0-9a-f]{64}) clean=true$', output, re.M)
            owned = re.findall(r'^OWNED nonce=([0-9a-f]{32}) container=([0-9a-f]{64}) network=([0-9a-f]{64}) port=[0-9]+ PostgreSQL=[0-9]+ vector=0\.8\.1$', output, re.M)
            terminal = re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true cleanup=true$', output, re.M)
            good = good and len(re.findall(r'^RETIRE observation=', output, re.M)) == 2 and len(re.findall(r'^OWNED nonce=', output, re.M)) == 1 and retired == expected_retire and owned == [(record['nonce'], record['container_id'], record['network_id'])] and len(terminal) == 1
        else:
            if set(record) != {'kind', 'child_pid'} or record['kind'] != 'work-http-native' or type(record['child_pid']) is not int:
                raise ValueError('invalid Skill HTTP native identity')
            good = (good and started == [str(record['child_pid'])]
                    and len(re.findall(r'^NATIVE runtime_empty=true actual_child_wait=true$', output, re.M)) == 1
                    and len(re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true private_removed=true$', output, re.M)) == 1)
        good = good and not directory.is_symlink() and {p.name for p in directory.iterdir()} == {'owned.json'}
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        good = False
    log.write(f'SKILL_HTTP exact_cases_wait_private={good}\n')
    return good

SECRET_HTTP_PG = '^TestSecretVariableHTTPBoundary$'
SECRET_HTTP_NATIVE = '^TestSecretHTTPNativeTransport$'
SECRET_HTTP_CASES = {
    SECRET_HTTP_PG: {'TestSecretVariableHTTPBoundary': (
        'real-owner-protocol-and-safe-history', 'authenticated-session-revoked-before-domain')},
    SECRET_HTTP_NATIVE: {'TestSecretHTTPNativeTransport': (
        'deadlines-and-keepalive', 'backpressure-and-disconnect-join')},
}


def secret_http_inputs(driver, binary, selector):
    root = Path(__file__).resolve().parents[2]
    output = root / 'output/ai/secret-owner-http/candidate-01'
    if selector == SECRET_HTTP_PG:
        expected_driver, expected_binary = 'pg-only-driver', 'secret-http-pg.test'
    elif selector == SECRET_HTTP_NATIVE:
        expected_driver, expected_binary = 'native-driver', 'secret-http-native.test'
    else:
        raise ValueError('unknown Secret HTTP selector')
    if driver != output / expected_driver or binary != output / expected_binary:
        raise ValueError('exact Secret HTTP artifacts required')
    paths = {driver, binary, Path(__file__).resolve(), root / 'go.mod', root / 'go.sum'}
    paths.update((root / 'internal/central/projectvariable/http').glob('*.go'))
    paths.add(root / 'internal/central/projectvariable/http/secret_native_test.go')
    if selector == SECRET_HTTP_PG:
        paths.update({root / '.agent-state/task-planning-recovery/pg_only_driver.go',
                      root / 'internal/central/account/assets/weak-passwords.json',
                      root / 'tests/projectvariable/secret_http_fixture_test.go',
                      root / 'tests/projectvariable/secret_http_test.go'})
        # Same concrete Owner dependencies and ordinary fixture helpers as its
        # accepted PG entry, plus the actual new HTTP fixture. No root/MinIO.
        for name in ('account', 'account/contract', 'accountmail', 'audit', 'audit/contract',
                     'cursor', 'event/contract', 'foundation', 'httpapi', 'identity/contract',
                     'object', 'object/contract', 'outbound', 'outbox', 'outbox/contract',
                     'postgres', 'project', 'project/contract', 'projectvariable',
                     'projectvariable/contract', 'projectvariable/http', 'recoverylog',
                     'secret', 'secret/contract', 'work', 'work/contract'):
            paths.update(p for p in (root / 'internal/central' / name).glob('*.go')
                         if not p.name.endswith('_test.go'))
        for name in ('tests/projectvariable', 'tests/testsupport/postgres', 'db/migrations',
                     '.agent-state/project-variables-independent/commitproxy'):
            paths.update((root / name).glob('*.go'))
        paths.update((root / 'db/migrations').glob('*.sql'))
    else:
        paths.add(root / '.agent-state/work-owner-http/native_driver.go')
    for path in paths:
        if path.resolve(strict=True) != path or not stat.S_ISREG(path.stat().st_mode):
            raise ValueError('non-regular Secret HTTP input')
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def observe_secret_http(directory, log, log_path, selector):
    try:
        output = log_path.read_text()
        expected = {top for top in SECRET_HTTP_CASES[selector]}
        expected.update(top + '/' + sub for top, subs in SECRET_HTTP_CASES[selector].items() for sub in subs)
        runs = re.findall(r'^=== RUN   (Test[^\s]+)$', output, re.M)
        passes = re.findall(r'^\s*--- PASS: (Test[^\s]+) \([^\r\n]*\)$', output, re.M)
        good = (set(runs) == expected and len(runs) == len(expected)
                and set(passes) == expected and len(passes) == len(expected))
        started = re.findall(r'^CHILD pid=([1-9][0-9]*) selector=' + re.escape(selector)
                             + (r' kind=native-http' if selector == SECRET_HTTP_NATIVE else '') + r'$', output, re.M)
        waited = re.findall(r'^CHILD actual_wait pid=([1-9][0-9]*) state=exit status 0$', output, re.M)
        good = (good and len(started) == 1 and waited == started
                and len(re.findall(r'^CHILD pid=', output, re.M)) == 1
                and len(re.findall(r'^CHILD actual_wait ', output, re.M)) == 1
                and len(re.findall(r'^DRIVER terminal ', output, re.M)) == 1
                and re.search(r'^\s*--- (?:FAIL|SKIP): ', output, re.M) is None)
        manifest = directory / 'owned.json'
        if manifest.is_symlink() or manifest.stat().st_mode & 0o777 != 0o600 or manifest.stat().st_size > 16384:
            raise ValueError('invalid Secret HTTP ownership record')
        record = json.loads(manifest.read_bytes())
        if selector == SECRET_HTTP_PG:
            if (set(record) != {'nonce', 'network_id', 'container_id'}
                    or re.fullmatch('[0-9a-f]{32}', record['nonce']) is None
                    or any(re.fullmatch('[0-9a-f]{64}', record[k]) is None for k in ('network_id', 'container_id'))):
                raise ValueError('incomplete Secret HTTP PG identities')
            expected_retire = [(str(n), record['container_id'], record['network_id']) for n in (1, 2)]
            retired = re.findall(r'^RETIRE observation=([12]) exact_container=([0-9a-f]{64}) exact_network=([0-9a-f]{64}) clean=true$', output, re.M)
            owned = re.findall(r'^OWNED nonce=([0-9a-f]{32}) container=([0-9a-f]{64}) network=([0-9a-f]{64}) port=[0-9]+ PostgreSQL=[0-9]+ vector=0\.8\.1$', output, re.M)
            terminal = re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true cleanup=true$', output, re.M)
            good = good and len(re.findall(r'^RETIRE observation=', output, re.M)) == 2 and len(re.findall(r'^OWNED nonce=', output, re.M)) == 1 and retired == expected_retire and owned == [(record['nonce'], record['container_id'], record['network_id'])] and len(terminal) == 1
        else:
            if set(record) != {'kind', 'child_pid'} or record['kind'] != 'work-http-native' or type(record['child_pid']) is not int:
                raise ValueError('invalid Secret HTTP native identity')
            good = (good and started == [str(record['child_pid'])]
                    and len(re.findall(r'^NATIVE runtime_empty=true actual_child_wait=true$', output, re.M)) == 1
                    and len(re.findall(r'^DRIVER terminal exit=0 elapsed=\S+ child_started=true actual_child_wait=true private_removed=true$', output, re.M)) == 1)
        good = good and not directory.is_symlink() and {p.name for p in directory.iterdir()} == {'owned.json'}
    except (OSError, UnicodeError, ValueError, TypeError, KeyError):
        good = False
    log.write(f'SECRET_HTTP exact_cases_wait_private={good}\n')
    return good


def survivor_identity(pid):
    # Failure-only evidence for an already discovered owned descendant. Never
    # collect argv, environment or full executable paths, or change retirement.
    result = {'pid': pid, 'comm': None, 'state': None, 'ppid': None,
              'starttime': None, 'exe_name': None}
    try:
        raw = Path(f'/proc/{pid}/stat').read_text()
        prefix, fields = raw.rsplit(')', 1)
        tail = fields.split()
        if int(prefix.split('(', 1)[0]) == pid:
            result.update(comm=prefix.split('(', 1)[1], state=tail[0],
                          ppid=int(tail[1]), starttime=int(tail[19]))
    except (OSError, UnicodeError, ValueError, IndexError):
        pass
    try:
        result['exe_name'] = Path(os.readlink(f'/proc/{pid}/exe')).name
    except OSError:
        pass
    return json.dumps(result, sort_keys=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--driver', required=True, type=Path)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--run', required=True)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--root-chain', action='store_true',
                        help='exact Work root adapter; 540s chain budget and seven-resource observations')
    args = parser.parse_args()
    if any(name in args.run for name in ('AgentConfigurationMetadata', 'AgentConfigurationSchema', 'AgentRuntimeSchema', 'ExecutionPreparation', 'AgentConfigurationCreate', 'TaskTransitionHuman', 'SchedulerClaim', 'SchedulerLaunch', 'SchedulerBusyCompensation', 'SchedulerPendingVisit', 'TaskHumanHTTP', 'SprintStartHTTP', 'TaskTechnicalResolutionHTTP', 'TaskTechnicalResolutionAtomic', 'SchedulerRetryBinding', 'SchedulerBoundedRetry', 'SchedulerProjectRunner', 'ExecutionCaptureProviders', 'ExecutionModelEnvironmentCapture', 'ExecutionTaskContext')) and (args.run not in METADATA_GROUPS or not args.root_chain):
        parser.error('System configuration requires one exact original root-chain profile')
    if 'TestSkillInstallation' in args.run and (args.run not in METADATA_GROUPS or not args.root_chain):
        parser.error('Skill installation requires one exact original root-chain profile')
    if any(selector[1:-1] in args.run for selector in METADATA_GROUPS) and (args.run not in METADATA_GROUPS or not args.root_chain):
        parser.error('configuration metadata requires one exact original root-chain entry')
    if 'ProjectLifecycleStopBatchRealGuard' in args.run and (args.run != GUARD_ROOT or not args.root_chain):
        parser.error('lifecycle guard requires one exact original root-chain entry')
    if 'ModelTextRuntimePersistentWire' in args.run and (args.run != MODEL_RUNTIME or not args.root_chain):
        parser.error('Model Runtime requires one exact original root-chain entry')
    if 'KnowledgePlainTextParser' in args.run and (args.run != PARSER_PG or not args.root_chain):
        parser.error('plain text parser requires its exact original root-chain entry')
    if 'KnowledgeOwnerReadWeb' in args.run and (args.run != KNOWLEDGE_UI or not args.root_chain):
        parser.error('Knowledge UI requires its exact original root-chain entry')
    if 'ProjectSecretVariablesDefaultRoot' in args.run and (args.run != SECRET_ROOT or not args.root_chain):
        parser.error('Secret default root requires its exact original root-chain entry')
    if args.run == '^TestKnowledgeSkillsDefaultRootComposition$' and not args.root_chain:
        parser.error('default root composition requires the original root chain')
    if any(name in args.run for name in ('SecretVariableHTTP', 'SecretHTTPNative')) and (args.root_chain or args.run not in SECRET_HTTP_CASES):
        parser.error('Secret HTTP requires one exact PG/native top without root mode')
    secret_http_selected = not args.root_chain and args.run in SECRET_HTTP_CASES
    if 'SecretVariableOwner' in args.run and (args.root_chain or args.run not in SECRET_OWNER_CASES):
        parser.error('Secret Owner requires one exact PG-only group')
    secret_owner = not args.root_chain and args.run in SECRET_OWNER_CASES
    if 'SecretVariableStorage' in args.run and (args.root_chain or args.run not in (SECRET_STORAGE_CASES | SECRET_STORAGE_RECOVERY_CASES)):
        parser.error('Secret storage requires one exact PG-only core or maintenance group')
    driver_timeout, term_grace = budgets(args.root_chain)
    adapter = root_adapter(args.driver) if args.root_chain else None
    secret_recovery = not args.root_chain and args.run in SECRET_STORAGE_RECOVERY_CASES
    secret_storage = not args.root_chain and (args.run in SECRET_STORAGE_CASES or secret_recovery)
    if adapter is not None and args.run not in adapter.TARGETS:
        parser.error('root mode requires one exact Work root selector')
    if args.run in CONTENT_GROUPS and args.root_chain != (args.run == CONTENT_PG):
        parser.error('exact content selector requires its declared mode')
    if args.run == CONTENT_PG:
        try:
            content_schema_python()
            args.content_schema_python = os.environ['AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON']
        except (OSError, ValueError):
            parser.error('explicit local content Schema interpreter required')
    stem = ('ui-' + uuid.uuid4().hex[:16]) if args.run == KNOWLEDGE_UI else ('pg-' + uuid.uuid4().hex)
    directory = args.output.resolve() / stem
    if args.run == KNOWLEDGE_UI:
        try:
            adapter.knowledge_ui_configuration(directory)
            args.knowledge_ui_environment = adapter.knowledge_ui_environment()
        except (OSError, ValueError):
            parser.error('exact frozen Knowledge assets, interpreters and fresh evidence required')
    args.output.mkdir(parents=True, exist_ok=True)
    log_path = args.output / (stem + '.log')
    # Adopt only this supervisor's own descendants, so any unexpected survivor
    # can be actually waited and reported rather than inferred dead from ps.
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), 'PR_SET_CHILD_SUBREAPER')
    inputs = {str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest()
              for p in (args.driver, args.binary)}
    if adapter is not None:
        inputs = {str(p): adapter.sha(p) for p in adapter.input_paths(args.binary)}
        if args.run == '^TestObjectMetadataCleanup(ProjectHistoryPlans|SkillsIndexPlans|TransferAndForeignKeyPlans)$':
            inputs.update({str(p): adapter.sha(p) for p in adapter.metadata_cost_inputs()})
        if args.run in ('^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|FinalAnchorForeignKeyPlans|PendingHistoryAndCausePlans)$', '^TestObjectMetadataCleanup(LiveTransferAndDownloadPlans|PendingHistoryAndCausePlans)$'):
            inputs.update({str(p): adapter.sha(p) for p in adapter.metadata_remaining_cost_inputs()})
    if args.run in CONTENT_GROUPS:
        inputs.update({str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest() for p in content_inputs(args.run)})
    skill_selected = not args.root_chain and args.run in SKILL_HTTP_CASES
    if skill_selected:
        inputs = skill_http_inputs(args.driver, args.binary, args.run)
    if secret_storage:
        inputs = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in secret_storage_inputs(args.driver, args.binary, secret_recovery)}
    if secret_owner:
        inputs = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in secret_owner_inputs(args.driver, args.binary)}
    if secret_http_selected:
        inputs = secret_http_inputs(args.driver, args.binary, args.run)
    if args.run in ('^TestKnowledgeSkillsDefaultRootComposition$', SECRET_ROOT):
        inputs.update({str(p): adapter.sha(p) for p in adapter.root_composition_inputs()})
    if args.run in METADATA_GROUPS:
        inputs = {str(p): adapter.sha(p) for p in adapter.metadata_inputs(args.binary, args.run)}
    if args.run == GUARD_ROOT:
        inputs = {str(p): adapter.sha(p) for p in adapter.guard_inputs(args.binary)}
    if args.run == MODEL_RUNTIME:
        inputs = {str(p): adapter.sha(p) for p in adapter.model_runtime_inputs(args.binary)}
    if args.run == PARSER_PG:
        inputs = {str(p): adapter.sha(p) for p in adapter.parser_inputs(args.binary)}
    if args.run == KNOWLEDGE_UI:
        inputs = {str(p): adapter.sha(p) for p in adapter.knowledge_ui_inputs(args.binary)}
    baseline = tcp()
    started = time.monotonic()
    child = None
    code = 1
    interrupted = False
    def stop(signum, frame):
        nonlocal interrupted
        interrupted = True
        if child is not None and child.poll() is None:
            child.send_signal(signal.SIGTERM)
    old = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
    with log_path.open('w', buffering=1) as log:
        try:
            if skill_selected:
                child = skill_http_spawn(args.driver, args.binary, args.run, directory, log)
            else:
                child = subprocess.Popen([str(args.driver.resolve()), '--test-binary',
                    str(args.binary.resolve()), '--run', args.run, '--directory', str(directory)],
                    stdout=log, stderr=subprocess.STDOUT)
            nonroot_reap_deadline = None
            try:
                code = child.wait(timeout=driver_timeout)
            except subprocess.TimeoutExpired:
                if not args.root_chain:
                    # Share the existing three-second retirement allowance
                    # between TERM, direct SIGKILL wait and adopted waits.
                    nonroot_reap_deadline = time.monotonic() + term_grace
                child.terminate()
                try:
                    code = child.wait(timeout=term_grace if args.root_chain else min(1, term_grace / 3))
                except subprocess.TimeoutExpired:
                    child.kill()
                    if args.root_chain:
                        try:
                            code = child.wait(timeout=3)
                        except subprocess.TimeoutExpired:
                            log.write('STOP root driver still not waited after bounded SIGKILL tail\n')
                    else:
                        try:
                            code = child.wait(timeout=max(0, nonroot_reap_deadline - time.monotonic()))
                        except subprocess.TimeoutExpired:
                            log.write('STOP driver still not waited within retirement deadline\n')
                code = 1
                log.write('STOP driver exceeded runtime budget\n')
            if args.root_chain:
                log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} actual={child.returncode is not None} code={code}\n')
            else:
                log.write(f'SUPERVISOR actual_driver_wait pid={child.pid} actual={child.returncode is not None} actual_exit={child.returncode} code={code}\n')
            if args.run == KNOWLEDGE_UI and child.returncode is not None:
                if not knowledge_ui_reap_exited(log):
                    code = 1
            survivors = descendants(os.getpid())
            if survivors:
                code = 1
                log.write(f'STOP owned descendants survived driver: {sorted(survivors)}\n')
                for pid in survivors:
                    if args.run == '^TestSkillLifecycleCleanupHistoricalAttempts$':
                        log.write('OWNED survivor_identity=' + survivor_identity(pid) + '\n')
                    try: os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError: pass
            reap_deadline = (time.monotonic() + 5 if args.root_chain else
                             nonroot_reap_deadline if nonroot_reap_deadline is not None else
                             time.monotonic() + term_grace)
            while True:
                if not args.root_chain and child.returncode is None:
                    # Popen still owns the direct child. Do not steal its
                    # eventual status with generic waitpid and claim a join.
                    code = 1
                    log.write('STOP direct wait incomplete; adopted wait not claimed\n')
                    break
                try:
                    pid, status = os.waitpid(-1, os.WNOHANG)
                    if pid == 0:
                        if time.monotonic() >= reap_deadline:
                            code = 1
                            kind = 'root ' if args.root_chain else ''
                            log.write(f'STOP owned {kind}descendants not joined within bounded reap tail\n')
                            break
                        time.sleep(.02)
                        continue
                    if args.root_chain and pid == child.pid:
                        child.returncode = os.waitstatus_to_exitcode(status)
                    log.write(f'SUPERVISOR adopted_actual_wait pid={pid} status={status}\n')
                except ChildProcessError:
                    break
            for round in (1, 2):
                remaining = descendants(os.getpid())
                log.write(f'OWNED runtime_observation={round} descendants={sorted(remaining)}\n')
                if remaining: code = 1
            if args.root_chain and not (content_root(directory, log, log_path, args.run)
                    if args.run == CONTENT_PG else observe_root_chain(directory, log, log_path, args.run)):
                code = 1
            if not args.root_chain and args.run == CONTENT_NATIVE and not content_native(directory, log, log_path, args.run):
                code = 1
            if skill_selected and not observe_skill_http(directory, log, log_path, args.run):
                code = 1
            if secret_storage and not observe_secret_storage(log_path, log, args.run):
                code = 1
            if secret_owner and not observe_secret_owner(log_path, log, args.run):
                code = 1
            if secret_http_selected and not observe_secret_http(directory, log, log_path, args.run):
                code = 1
            # The tail is a host delta, not an assertion that every short
            # connection in this shared host was owned by this invocation.
            tail_deadline = time.monotonic() + 75
            empty, delta = 0, set()
            while time.monotonic() < tail_deadline and empty < 2:
                delta = tcp() - baseline
                if not delta:
                    empty += 1
                    log.write(f'HOST_TCP delta_empty_observation={empty}\n')
                else:
                    empty = 0
                if empty < 2: time.sleep(.1)
            if empty != 2:
                code = 1
                log.write(f'STOP host TCP delta tail not empty: {len(delta)} rows\n')
                sample = tcp_failure_sample(delta, 'supervisor')
                log.write('HOST_TCP failure_sample=' + json.dumps(sample, sort_keys=True) + '\n')
                # stdout is retained in the task's existing private supervisor.log.
                print(json.dumps({'host_tcp_failure': sample}, sort_keys=True), flush=True)
            if secret_http_selected:
                try:
                    same = secret_http_inputs(args.driver, args.binary, args.run) == inputs
                except (OSError, ValueError):
                    same = False
            elif secret_owner:
                try:
                    same = (all(hashlib.sha256(Path(p).read_bytes()).hexdigest() == digest
                                for p, digest in inputs.items())
                            and set(inputs) == {str(p) for p in secret_owner_inputs(args.driver, args.binary)})
                except (OSError, ValueError):
                    same = False
            else:
                if secret_storage:
                    try:
                        same = (all(hashlib.sha256(Path(p).read_bytes()).hexdigest() == digest
                                    for p, digest in inputs.items())
                                and set(inputs) == {str(p) for p in secret_storage_inputs(args.driver, args.binary, secret_recovery)})
                    except (OSError, ValueError):
                        same = False
                else:
                    if skill_selected:
                        try:
                            same = skill_http_inputs(args.driver, args.binary, args.run) == inputs
                        except (OSError, ValueError):
                            same = False
                    else:
                        same = (content_same(inputs, args, adapter) if args.run in CONTENT_GROUPS else
                                all((adapter.sha(p) if adapter is not None else hashlib.sha256(Path(p).read_bytes()).hexdigest()) == digest
                                    for p, digest in inputs.items()))
            if args.run in ('^TestKnowledgeSkillsDefaultRootComposition$', SECRET_ROOT):
                same = same and root_composition_same(inputs, args, adapter)
            if args.run in METADATA_GROUPS:
                same = same and metadata_same(inputs, args, adapter, args.run)
            if args.run == GUARD_ROOT:
                same = same and guard_same(inputs, args, adapter)
            if args.run == MODEL_RUNTIME:
                same = same and model_runtime_same(inputs, args, adapter)
            if args.run == PARSER_PG:
                same = same and parser_same(inputs, args, adapter)
            if args.run == KNOWLEDGE_UI:
                same = same and knowledge_ui_same(inputs, args, adapter)
            if not same: code = 1
            if interrupted: code = 1
            log.write(f'SUPERVISOR inputs_unchanged={same} terminal={code} elapsed={time.monotonic()-started:.3f}s\n')
        finally:
            for s, handler in old.items(): signal.signal(s, handler)
    print(json.dumps({'selector': args.run, 'exit': code, 'log': str(log_path),
                      'owned_directory': str(directory), 'actual_driver_wait': child is not None and child.returncode is not None}))
    return code


if __name__ == '__main__':
    sys.exit(main())
