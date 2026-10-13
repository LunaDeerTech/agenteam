#!/usr/bin/env python3
"""Run a reviewed UI scope through the repository's real fixture chain.

No new services, full-source hashing, or global network/cleanup policy. Original
Go and Playwright budgets stay in their fixtures. Record actual child waits and
check only resources this invocation owns. Save under .agent-state before use.
"""
import argparse
import ctypes
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

NATIVE_PG_FTS_SELECTOR = '^TestNativePGFTSBenchmark$'
NATIVE_PG_FTS_EXPECTED = {'TestNativePGFTSBenchmark'}
TASK_STATE_FILTER_SELECTOR = '^TestTaskStateFilterWeb$'
TASK_STATE_FILTER_EXPECTED = {'TestTaskStateFilterWeb',
    'TestTaskStateFilterWeb/default-app-state-pagination'}
TASK_BLOCKER_HTTP_SELECTOR = '^TestTaskBlockerManagementAssignedBlockedHTTP$'
TASK_BLOCKER_HTTP_EXPECTED = {'TestTaskBlockerManagementAssignedBlockedHTTP',
    'TestTaskBlockerManagementAssignedBlockedHTTP/original-failure-human-cause-and-technical-boundary'}
TASK_BLOCKER_WEB_SELECTOR = '^TestTaskBlockerManagementWeb$'
TASK_BLOCKER_WEB_EXPECTED = {'TestTaskBlockerManagementWeb',
    'TestTaskBlockerManagementWeb/assigned-todo-human-and-dependency'}
TASK_CANCEL_SELECTOR = '^TestTaskCancelWeb$'
TASK_CANCEL_EXPECTED = {'TestTaskCancelWeb',
    'TestTaskCancelWeb/explicit-cancel-preserves-relations-and-completes-sprint'}
SPRINT_COMPLETE_HTTP_SELECTOR = '^TestSprintCompleteHTTP$'
SPRINT_COMPLETE_HTTP_EXPECTED = {'TestSprintCompleteHTTP',
    'TestSprintCompleteHTTP/empty-original-recovery-and-unfinished-refusal',
    'TestSprintCompleteHTTP/final-completion-lock-excludes-new-member'}
SPRINT_COMPLETE_CLEANUP_SELECTOR = '^TestProjectWorkCleanup$/^real-relations-upgrade-and-original-return$'
SPRINT_COMPLETE_CLEANUP_EXPECTED = {'TestProjectWorkCleanup',
    'TestProjectWorkCleanup/real-relations-upgrade-and-original-return'}
SPRINT_COMPLETE_WEB_SELECTOR = '^TestSprintCompleteWeb$'
SPRINT_COMPLETE_WEB_EXPECTED = {'TestSprintCompleteWeb',
    'TestSprintCompleteWeb/complete-explicit-next-and-original-lookup'}
SELECTOR = '^(TestTaskSearchWeb|TestProjectVariablesWeb)$'
EXPECTED = {'TestTaskSearchWeb', 'TestTaskSearchWeb/server-search-beyond-first-page',
            'TestProjectVariablesWeb', 'TestProjectVariablesWeb/create-update-clear-and-delete'}
CREATE_SELECTOR = '^(TestProjectCreationApp|TestProjectCreationRecoveryApp)$'
CREATE_EXPECTED = {'TestProjectCreationApp',
                   'TestProjectCreationApp/default-create-and-configure',
                   'TestProjectCreationApp/create-through-browser',
                   'TestProjectCreationRecoveryApp',
                   'TestProjectCreationRecoveryApp/cancelled-create-restarts-original-intent'}
SECRET_SELECTOR = '^TestProjectSecretDeleteWeb$'
SECRET_EXPECTED = {'TestProjectSecretDeleteWeb',
                   'TestProjectSecretDeleteWeb/busy-reference-then-delete'}
USAGE_SELECTOR = '^TestProjectUsageSummaryWeb$'
USAGE_EXPECTED = {'TestProjectUsageSummaryWeb',
                  'TestProjectUsageSummaryWeb/daily-server-aggregate'}
SKILL_SELECTOR = '^TestSkillPackageImportWeb$'
SKILL_EXPECTED = {'TestSkillPackageImportWeb',
                  'TestSkillPackageImportWeb/existing-file-import-and-fixed-revision-download'}
TASK_SELECTOR = '^TestTaskSearchWeb$'
TASK_EXPECTED = {'TestTaskSearchWeb', 'TestTaskSearchWeb/server-search-beyond-first-page'}
RUNNER_COMMAND_SELECTOR = '^TestRunnerCommandApp$'
RUNNER_COMMAND_EXPECTED = {'TestRunnerCommandApp',
                           'TestRunnerCommandApp/default-enrollment-does-not-start-command',
                           'TestRunnerCommandApp/explicit-argv-report-and-cancelled-child-join',
                           'TestRunnerCommandApp/same-identity-restart-command-and-read'}
RUNNER_WRITE_SELECTOR = '^TestRunnerWriteFileApp$'
RUNNER_WRITE_EXPECTED = {'TestRunnerWriteFileApp',
                         'TestRunnerWriteFileApp/enrollment-write-and-no-overwrite-conflict',
                         'TestRunnerWriteFileApp/stored-identity-combined-read-write',
                         'TestRunnerWriteFileApp/omitted-write-is-not-dispatched'}
RUNNER_SELECTOR = '^TestRunnerDirectoryApp$'
RUNNER_EXPECTED = {'TestRunnerDirectoryApp',
                   'TestRunnerDirectoryApp/selected-directory-reads-and-identity-restart',
                   'TestRunnerDirectoryApp/selection-isolation-and-before-device-validation'}
EXECUTION_SELECTOR = '^TestExecutionProjectStop$'
EXECUTION_EXPECTED = {'TestExecutionProjectStop',
                      'TestExecutionProjectStop/project-isolation-and-original-model-return',
                      'TestExecutionProjectStop/original-preparation-unknown-retained'}
OUTBOX_SELECTOR = '^TestProjectOutboxCleanup$'
OUTBOX_EXPECTED = {'TestProjectOutboxCleanup',
                   'TestProjectOutboxCleanup/original-d06-return-before-project-progress',
                   'TestProjectOutboxCleanup/unknown-preflight-and-expired-owner'}
AUDIT_SELECTOR = '^TestProjectAuditCleanup$'
AUDIT_EXPECTED = {'TestProjectAuditCleanup',
                  'TestProjectAuditCleanup/outbox-to-audit-500-plus-one-original-return',
                  'TestProjectAuditCleanup/original-unknown-confirmation-and-expired-scope'}
SKILLS_CLEANUP_SELECTOR = '^TestProjectSkillsCleanup$'
SKILLS_CLEANUP_EXPECTED = {'TestProjectSkillsCleanup',
                           'TestProjectSkillsCleanup/real-packages-and-original-return',
                           'TestProjectSkillsCleanup/disabled-agent-retains-initialization'}
AGENT_RETIREMENT_SELECTOR = '^TestProjectAgentRetirement$'
AGENT_RETIREMENT_EXPECTED = {'TestProjectAgentRetirement',
                            'TestProjectAgentRetirement/canonical-references-and-original-return',
                            'TestProjectAgentRetirement/original-unknown-and-applied-anchor',
                            'TestProjectAgentRetirement/enabled-assignment-rolls-back-retirement'}
AGENT_INITIAL_GRANT_EXPECTED = {'TestProjectAgentRetirement',
                               'TestProjectAgentRetirement/canonical-references-and-original-return',
                               'TestProjectAgentRetirement/original-unknown-and-applied-anchor',
                               'TestProjectAgentRetirement/assignment-history-rolls-back-retirement'}
AGENT_ASSIGNMENT_HISTORY_EXPECTED = {'TestProjectAgentRetirement',
                                    'TestProjectAgentRetirement/canonical-references-and-original-return',
                                    'TestProjectAgentRetirement/original-unknown-and-applied-anchor',
                                    'TestProjectAgentRetirement/planned-assignment-rolls-back-retirement'}
VARIABLES_CLEANUP_SELECTOR = '^TestProjectVariablesCleanup$'
VARIABLES_CLEANUP_EXPECTED = {'TestProjectVariablesCleanup',
                              'TestProjectVariablesCleanup/owned-lanes-batches-and-original-return',
                              'TestProjectVariablesCleanup/original-unknown-and-metadata-only-recovery'}
AGENT_VARIABLES_FINALIZATION_SELECTOR = '^TestProjectAgentVariablesFinalization$'
AGENT_VARIABLES_FINALIZATION_EXPECTED = {'TestProjectAgentVariablesFinalization',
                                        'TestProjectAgentVariablesFinalization/terminal-receipt-and-original-return',
                                        'TestProjectAgentVariablesFinalization/original-unknown-and-receipt-confirmation',
                                        'TestProjectAgentVariablesFinalization/foreign-live-instance-retains-terminal-data',
                                        'TestProjectAgentVariablesFinalization/empty-not-applied-original-return-fences-new-pairs'}
SECRET_CLEANUP_SELECTOR = '^TestProjectSecretCleanup$'
SECRET_CLEANUP_EXPECTED = {'TestProjectSecretCleanup',
                           'TestProjectSecretCleanup/head-batches-and-original-return',
                           'TestProjectSecretCleanup/original-unknown-readonly-confirmation',
                           'TestProjectSecretCleanup/formal-provider-reference-remains-pending'}
ARTIFACT_CLEANUP_SELECTOR = '^TestProjectArtifactCleanup$'
ARTIFACT_CLEANUP_EXPECTED = {'TestProjectArtifactCleanup',
                             'TestProjectArtifactCleanup/inline-upload-and-original-checkpoint-return',
                             'TestProjectArtifactCleanup/artifact-original-unknown-readonly-confirmation',
                             'TestProjectArtifactCleanup/object-original-unknown-readonly-confirmation'}

ARTIFACT_STOP_SELECTOR = '^TestProjectArtifactStop$'
ARTIFACT_STOP_EXPECTED = {'TestProjectArtifactStop',
                          'TestProjectArtifactStop/migration78-to79-and-original-public-return',
                          'TestProjectArtifactStop/original-stop-unknown-readonly-confirmation'}

ARTIFACT_SOURCE_STOP_SELECTOR = '^TestProjectArtifactSourceStop$'
ARTIFACT_SOURCE_STOP_EXPECTED = {'TestProjectArtifactSourceStop',
                                 'TestProjectArtifactSourceStop/original-source-acquisition-and-release-return',
                                 'TestProjectArtifactSourceStop/original-source-unknown-readonly-confirmation'}

ARTIFACT_SOURCE_CLEANUP_SELECTOR = '^TestProjectArtifactSourceCleanup$'
ARTIFACT_SOURCE_CLEANUP_EXPECTED = {'TestProjectArtifactSourceCleanup'}

PROJECT_FINALIZE_SELECTOR = '^TestProjectFinalize$'
PROJECT_FINALIZE_EXPECTED = {'TestProjectFinalize',
                             'TestProjectFinalize/original-final-return-and-atomic-receipt',
                             'TestProjectFinalize/committed-unknown-receipt-first-confirmation',
                             'TestProjectFinalize/registration-rollback-unknown-explicit-resume'}

WORK_STOP_SELECTOR = '^TestProjectWorkStop$'
WORK_STOP_EXPECTED = {'TestProjectWorkStop',
                      'TestProjectWorkStop/five-lanes-upgrade-and-original-return',
                      'TestProjectWorkStop/technical-unknown-readonly-confirmation',
                      'TestProjectWorkStop/archive-keeps-original-read-and-replay'}

SCHEDULER_STOP_SELECTOR = '^TestProjectSchedulerStop$'
SCHEDULER_STOP_EXPECTED = {'TestProjectSchedulerStop',
                           'TestProjectSchedulerStop/seven-entry-owners-upgrade-and-original-return',
                           'TestProjectSchedulerStop/technical-unknown-readonly-confirmation',
                           'TestProjectSchedulerStop/known-not-created-original-acknowledgement'}

SCHEDULER_CLEANUP_SELECTOR = '^TestProjectSchedulerCleanup$'
SCHEDULER_CLEANUP_EXPECTED = {'TestProjectSchedulerCleanup',
                              'TestProjectSchedulerCleanup/real-references-upgrade-and-original-returns',
                              'TestProjectSchedulerCleanup/original-unknown-readonly-confirmation'}

EXECUTION_CALL_STOP_SELECTOR = '^TestProjectExecutionCallStop$'
EXECUTION_CALL_STOP_EXPECTED = {'TestProjectExecutionCallStop',
                                'TestProjectExecutionCallStop/service-return-upgrade-and-instance-isolation',
                                'TestProjectExecutionCallStop/technical-unknown-readonly-confirmation',
                                'TestProjectExecutionCallStop/associated-preparation-original-failure-return'}

WORK_READER_STOP_SELECTOR = '^TestProjectWorkReaderStop$'
WORK_READER_STOP_EXPECTED = {'TestProjectWorkReaderStop',
                             'TestProjectWorkReaderStop/eight-nonempty-reads-and-original-store-return',
                             'TestProjectWorkReaderStop/reader-technical-unknown-readonly-confirmation',
                             'TestProjectWorkReaderStop/archive-preserves-original-and-new-reads'}

EXECUTION_RETIREMENT_SELECTOR = '^TestProjectExecutionRetirement$'
EXECUTION_RETIREMENT_EXPECTED = {'TestProjectExecutionRetirement',
                                 'TestProjectExecutionRetirement/queued-proof-upgrade-and-original-return',
                                 'TestProjectExecutionRetirement/original-committed-unknown-readonly-confirmation',
                                 'TestProjectExecutionRetirement/registration-rollback-unknown-and-next-explicit-run'}

EXECUTION_RETIREMENT_SEED_DIAGNOSTIC_SELECTOR = '^TestProjectExecutionRetirement$/^queued-proof-upgrade-and-original-return$'
EXECUTION_RETIREMENT_SEED_DIAGNOSTIC_EXPECTED = {
    'TestProjectExecutionRetirement',
    'TestProjectExecutionRetirement/queued-proof-upgrade-and-original-return'}

WORK_CLEANUP_SELECTOR = '^TestProjectWorkCleanup$'
WORK_CLEANUP_EXPECTED = {'TestProjectWorkCleanup',
                         'TestProjectWorkCleanup/real-relations-upgrade-and-original-return',
                         'TestProjectWorkCleanup/provider-and-checkpoint-unknown-readonly-confirmations',
                         'TestProjectWorkCleanup/registration-rollback-unknown-and-next-run'}

APP_WORK_OWNERSHIP_SELECTOR = '^(TestWorkProjectCallDefaultApp|TestWorkOwnerRootActualReaderJoin|TestWorkOwnerRootActualCommandJoin)$'
APP_WORK_OWNERSHIP_EXPECTED = {'TestWorkProjectCallDefaultApp',
                               'TestWorkProjectCallDefaultApp/default-http-original-owner',
                               'TestWorkProjectCallDefaultApp/committed-return-before-root-drain',
                               'TestWorkOwnerRootActualReaderJoin',
                               'TestWorkOwnerRootActualReaderJoin/graceful-three-http-readers',
                               'TestWorkOwnerRootActualReaderJoin/force-keeps-original-deadline',
                               'TestWorkOwnerRootActualCommandJoin',
                               'TestWorkOwnerRootActualCommandJoin/graceful-three-calls',
                               'TestWorkOwnerRootActualCommandJoin/force-does-not-invent-join'}

RUNTIME_FINAL_TEXT_SELECTOR = '^TestRuntimeFinalTextDefaultApp$'
RUNTIME_FINAL_TEXT_EXPECTED = {'TestRuntimeFinalTextDefaultApp', 'TestRuntimeFinalTextDefaultApp/original-producer-and-restarted-history'}
RUNTIME_FINAL_TEXT_WEB_SELECTOR = '^TestRuntimeFinalTextWeb$'
RUNTIME_FINAL_TEXT_WEB_EXPECTED = {'TestRuntimeFinalTextWeb', 'TestRuntimeFinalTextWeb/final-text-load-refresh-and-safe-render'}
TASK_EXECUTION_READ_SELECTOR = '^TestTaskExecutionReadDefaultApp$'
TASK_EXECUTION_READ_EXPECTED = {'TestTaskExecutionReadDefaultApp', 'TestTaskExecutionReadDefaultApp/enabled-owner-and-disabled-history'}
TASK_EXECUTION_READ_WEB_SELECTOR = '^TestTaskExecutionReadWeb$'
TASK_EXECUTION_READ_WEB_EXPECTED = {'TestTaskExecutionReadWeb', 'TestTaskExecutionReadWeb/history-detail-refresh-and-owner'}

TASK_REORDER_SELECTOR = '^TestTaskReorderWeb$'
TASK_REORDER_EXPECTED = {'TestTaskReorderWeb', 'TestTaskReorderWeb/task-reorder-normal',
                         'TestTaskReorderWeb/task-reorder-recovery'}

APP_ARTIFACT_CONTENT_SELECTOR = '^TestArtifactContentDefaultApp$'
APP_ARTIFACT_CONTENT_EXPECTED = {
    'TestArtifactContentDefaultApp',
    'TestArtifactContentDefaultApp/create-original-lookup-retry-cancel-and-owner'}
APP_ARTIFACT_CONTENT_WEB_SELECTOR = '^TestArtifactContentWeb$'
APP_ARTIFACT_CONTENT_WEB_EXPECTED = {
    'TestArtifactContentWeb', 'TestArtifactContentWeb/create-and-recover-original'}

APP_ARTIFACT_OWNER_HTTP_SELECTOR = '^TestArtifactOwnerHTTPDefaultApp$'
APP_ARTIFACT_OWNER_HTTP_EXPECTED = {
    'TestArtifactOwnerHTTPDefaultApp',
    'TestArtifactOwnerHTTPDefaultApp/pagination-current-owner-and-utf8-head',
    'TestArtifactOwnerHTTPDefaultApp/original-reader-handler-and-owner-return'}
APP_ARTIFACT_OWNER_WEB_SELECTOR = '^TestArtifactOwnerWeb$'
APP_ARTIFACT_OWNER_WEB_EXPECTED = {
    'TestArtifactOwnerWeb', 'TestArtifactOwnerWeb/catalog-and-bounded-read'}

APP_HUMAN_ARTIFACT_SELECTOR = '^TestHumanArtifactDefaultApp$'
APP_HUMAN_ARTIFACT_EXPECTED = {
    'TestHumanArtifactDefaultApp',
    'TestHumanArtifactDefaultApp/inline-replay-audit-and-original-close',
    'TestHumanArtifactDefaultApp/published-ack-unknown-and-readonly-replay',
    'TestHumanArtifactDefaultApp/pending-creation-explicit-cancellation'}

RUNNER_MANAGED_CLEANUP_SELECTOR = '^TestProjectRunnerManagedCleanup$'
RUNNER_MANAGED_CLEANUP_EXPECTED = {
    'TestProjectRunnerManagedCleanup',
    'TestProjectRunnerManagedCleanup/nonempty-configuration-and-original-return',
    'TestProjectRunnerManagedCleanup/original-unknown-readonly-confirmation'}

APP_KNOWLEDGE_LIFECYCLE_SELECTOR = '^TestKnowledgeProjectLifecycleDefaultApp$'
APP_KNOWLEDGE_LIFECYCLE_EXPECTED = {
    'TestKnowledgeProjectLifecycleDefaultApp',
    'TestKnowledgeProjectLifecycleDefaultApp/http-source-and-original-store-close',
    'TestKnowledgeProjectLifecycleDefaultApp/original-technical-unknown-readonly-confirmation',
    'TestKnowledgeProjectLifecycleDefaultApp/six-business-tables-and-object-retirement'}

APP_USAGE_LIFECYCLE_SELECTOR = '^TestUsageProjectLifecycleDefaultApp$'
APP_USAGE_LIFECYCLE_EXPECTED = {
    'TestUsageProjectLifecycleDefaultApp',
    'TestUsageProjectLifecycleDefaultApp/policy-deny-and-original-wholecall-return',
    'TestUsageProjectLifecycleDefaultApp/technical-and-rebuild-original-unknown',
    'TestUsageProjectLifecycleDefaultApp/three-tables-and-original-cleanup-confirmation'}
APP_EXECUTION_SCHEDULER_OWNERSHIP_SELECTOR = '^TestExecutionSchedulerProjectCallDefaultApp$'
APP_EXECUTION_SCHEDULER_OWNERSHIP_EXPECTED = {
    'TestExecutionSchedulerProjectCallDefaultApp',
    'TestExecutionSchedulerProjectCallDefaultApp/default-owned-graph-and-original-launch',
    'TestExecutionSchedulerProjectCallDefaultApp/committed-lookup-return-before-root-drain'}

def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')

def children():
    # Same parent-map walk as the existing PG supervisor. Some kernels omit
    # /proc/<pid>/task/<tid>/children; /proc/*/stat remains the supported source.
    parents = {}
    for stat in Path('/proc').glob('[0-9]*/stat'):
        try:
            fields = stat.read_text().rsplit(')', 1)[1].split()
            parents[int(stat.parent.name)] = int(fields[1])
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            pass
    result = {os.getpid()}
    changed = True
    while changed:
        added = {pid for pid, parent in parents.items() if parent in result} - result
        changed = bool(added)
        result |= added
    return sorted(result - {os.getpid()})

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--binary', type=Path, required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--runtime', type=Path, required=True)
    p.add_argument('--dist', type=Path)
    p.add_argument('--artifact-web-harness', type=Path)
    p.add_argument('--runner-binary', type=Path)
    p.add_argument('--native-fts-export', type=Path)
    p.add_argument('--native-fts-output', type=Path)
    p.add_argument('--input-hash', required=True)
    p.add_argument('--pg-current-only', action='store_true',
                   help='explicitly omit the unused PG16 negative-control fixture')
    scope = p.add_mutually_exclusive_group()
    scope.add_argument('--native-pg-fts', action='store_true',
                       help='run only the fixed native FTS candidate through the original current-only fixture chain')
    scope.add_argument('--task-state-filter', action='store_true',
                       help='run the default App state filter and original pagination browser scope')
    scope.add_argument('--task-blocker-http', action='store_true',
                       help='run the reviewed assigned/blocked Human Owner HTTP scope')
    scope.add_argument('--task-blocker-web', action='store_true',
                       help='run the reviewed assigned Todo blocker browser scope')
    scope.add_argument('--task-cancel', action='store_true',
                       help='run the reviewed explicit Task cancellation browser scope')
    scope.add_argument('--sprint-complete-http', action='store_true',
                       help='run the reviewed Sprint completion HTTP scope')
    scope.add_argument('--sprint-complete-cleanup', action='store_true',
                       help='run only the changed Work cleanup migration and original return subcase')
    scope.add_argument('--sprint-complete-web', action='store_true',
                       help='run the reviewed Sprint completion browser in the same package binary')
    scope.add_argument('--project-creation', action='store_true',
                       help='run the reviewed default-App creation, browser and recovery scope')
    scope.add_argument('--secret-delete', action='store_true',
                       help='run the reviewed referenced-Secret deletion scope')
    scope.add_argument('--project-usage-summary', action='store_true',
                       help='run the reviewed daily Usage summary scope')
    scope.add_argument('--skill-package-import', action='store_true',
                       help='run the reviewed existing-file Skill package import scope')
    scope.add_argument('--task-search', action='store_true',
                       help='run the reviewed Task search and enum filters scope')
    scope.add_argument('--runner-command', action='store_true',
                       help='run the reviewed explicit Runner run-command CLI scope')
    scope.add_argument('--runner-write-file', action='store_true',
                       help='run the reviewed explicit Runner write-file CLI scope')
    scope.add_argument('--runner-directory', action='store_true',
                       help='run the reviewed real Runner CLI with its separate prebuilt command')
    scope.add_argument('--execution-project-stop', action='store_true',
                       help='run the reviewed Project-scoped Execution stop integration scope')
    scope.add_argument('--outbox-cleanup', action='store_true',
                       help='run the reviewed Project Outbox cleanup integration scope')
    scope.add_argument('--audit-cleanup', action='store_true',
                       help='run the reviewed Project Audit cleanup integration scope')
    scope.add_argument('--skills-cleanup', action='store_true',
                       help='run the reviewed Project Skills cleanup integration scope')
    scope.add_argument('--agent-retirement', action='store_true',
                       help='run the reviewed Project Agent retirement integration scope')
    scope.add_argument('--agent-initial-grant', action='store_true',
                       help='run the reviewed Agent initial-grant retirement and migration scope')
    scope.add_argument('--agent-assignment-history', action='store_true',
                       help='run the reviewed Agent assignment-history retirement and migration scope')
    scope.add_argument('--variables-cleanup', action='store_true',
                       help='run the reviewed Project Variables cleanup integration scope')
    scope.add_argument('--agent-variables-finalization', action='store_true',
                       help='run the reviewed Agent and Variables terminal owner cleanup scope')
    scope.add_argument('--secret-cleanup', action='store_true',
                       help='run the reviewed Project Secret cleanup integration scope')
    scope.add_argument('--artifact-object-cleanup', action='store_true',
                       help='run the reviewed Project Artifact and Object cleanup scope')
    scope.add_argument('--artifact-stop', action='store_true',
                       help='run the reviewed real Artifact Stop and technical cleanup scope')
    scope.add_argument('--artifact-source-stop', action='store_true',
                       help='run the reviewed typed Artifact Source Stop integration scope')
    scope.add_argument('--artifact-source-cleanup', action='store_true',
                       help='run the reviewed bounded Artifact Source history cleanup scope')
    scope.add_argument('--project-finalize', action='store_true',
                       help='run the reviewed Project finalizer original return and recovery scope')
    scope.add_argument('--work-stop', action='store_true',
                       help='run the reviewed owned Work Stop integration scope')
    scope.add_argument('--scheduler-stop', action='store_true',
                       help='run the reviewed owned Scheduler Stop integration scope')
    scope.add_argument('--scheduler-cleanup', action='store_true',
                       help='run the reviewed bounded Scheduler cleanup integration scope')
    scope.add_argument('--execution-call-stop', action='store_true',
                       help='run the reviewed owned Execution call Stop integration scope')
    scope.add_argument('--work-reader-stop', action='store_true',
                       help='run the reviewed owned Work Reader Stop integration scope')
    scope.add_argument('--execution-retirement', action='store_true',
                       help='run the reviewed Execution retirement and reference cleanup scope')
    scope.add_argument('--execution-retirement-seed-diagnostic', action='store_true',
                       help='diagnose only the first Execution retirement seed; not full scope acceptance')
    scope.add_argument('--work-cleanup', action='store_true',
                       help='run the reviewed bounded Work history cleanup integration scope')
    scope.add_argument('--app-work-ownership', action='store_true',
                       help='run the reviewed default App Work ownership and original root return scope')
    scope.add_argument('--app-execution-scheduler-ownership', action='store_true',
                       help='run the reviewed default App Execution and Scheduler ownership scope')
    scope.add_argument('--app-usage-lifecycle', action='store_true',
                       help='run only the original default Usage App lifecycle scope')
    scope.add_argument('--app-knowledge-lifecycle', action='store_true',
                       help='run only the original default Knowledge App lifecycle scope')
    scope.add_argument('--app-human-artifact', action='store_true',
                       help='run only the reviewed default App Human Artifact ownership scope')
    scope.add_argument('--runner-managed-cleanup', action='store_true',
                       help='run only the reviewed Runner managed logical configuration cleanup scope')
    scope.add_argument('--app-runtime-final-text', action='store_true',
                       help='run the reviewed Runtime final text original producer and restarted history scope')
    scope.add_argument('--app-runtime-final-text-web', action='store_true',
                       help='run the reviewed Runtime final text browser in the same App binary')
    scope.add_argument('--app-task-execution-read', action='store_true',
                       help='run the reviewed default App Task Execution read scope')
    scope.add_argument('--app-task-execution-read-web', action='store_true',
                       help='run the reviewed Task Execution read browser in the same App binary')
    scope.add_argument('--task-reorder', action='store_true',
                       help='run only the reviewed Task reorder normal and recovery browser scope')
    scope.add_argument('--app-artifact-content', action='store_true',
                       help='run the default App Artifact content original command scope')
    scope.add_argument('--app-artifact-content-web', action='store_true',
                       help='run the Artifact content browser in the same App binary')
    scope.add_argument('--app-artifact-owner-http', action='store_true',
                       help='run the reviewed default App Artifact owner HTTP scope')
    scope.add_argument('--app-artifact-owner-web', action='store_true',
                       help='run the reviewed Artifact owner browser in the same App binary')
    a = p.parse_args()
    selector = (NATIVE_PG_FTS_SELECTOR if a.native_pg_fts else
                TASK_STATE_FILTER_SELECTOR if a.task_state_filter else
                TASK_BLOCKER_HTTP_SELECTOR if a.task_blocker_http else
                TASK_BLOCKER_WEB_SELECTOR if a.task_blocker_web else
                TASK_CANCEL_SELECTOR if a.task_cancel else
                SPRINT_COMPLETE_HTTP_SELECTOR if a.sprint_complete_http else
                SPRINT_COMPLETE_CLEANUP_SELECTOR if a.sprint_complete_cleanup else
                SPRINT_COMPLETE_WEB_SELECTOR if a.sprint_complete_web else
                RUNTIME_FINAL_TEXT_SELECTOR if a.app_runtime_final_text else
                RUNTIME_FINAL_TEXT_WEB_SELECTOR if a.app_runtime_final_text_web else
                TASK_EXECUTION_READ_SELECTOR if a.app_task_execution_read else
                TASK_EXECUTION_READ_WEB_SELECTOR if a.app_task_execution_read_web else
                TASK_REORDER_SELECTOR if a.task_reorder else
                APP_ARTIFACT_CONTENT_SELECTOR if a.app_artifact_content else
                APP_ARTIFACT_CONTENT_WEB_SELECTOR if a.app_artifact_content_web else
                APP_ARTIFACT_OWNER_HTTP_SELECTOR if a.app_artifact_owner_http else
                APP_ARTIFACT_OWNER_WEB_SELECTOR if a.app_artifact_owner_web else
                APP_HUMAN_ARTIFACT_SELECTOR if a.app_human_artifact else
                RUNNER_MANAGED_CLEANUP_SELECTOR if a.runner_managed_cleanup else
                APP_KNOWLEDGE_LIFECYCLE_SELECTOR if a.app_knowledge_lifecycle else
                APP_USAGE_LIFECYCLE_SELECTOR if a.app_usage_lifecycle else
                APP_EXECUTION_SCHEDULER_OWNERSHIP_SELECTOR if a.app_execution_scheduler_ownership else
                APP_WORK_OWNERSHIP_SELECTOR if a.app_work_ownership else
                WORK_CLEANUP_SELECTOR if a.work_cleanup else
                EXECUTION_RETIREMENT_SEED_DIAGNOSTIC_SELECTOR if a.execution_retirement_seed_diagnostic else
                EXECUTION_RETIREMENT_SELECTOR if a.execution_retirement else
                WORK_READER_STOP_SELECTOR if a.work_reader_stop else
                EXECUTION_CALL_STOP_SELECTOR if a.execution_call_stop else
                SCHEDULER_CLEANUP_SELECTOR if a.scheduler_cleanup else
                SCHEDULER_STOP_SELECTOR if a.scheduler_stop else
                WORK_STOP_SELECTOR if a.work_stop else
                PROJECT_FINALIZE_SELECTOR if a.project_finalize else
                ARTIFACT_SOURCE_CLEANUP_SELECTOR if a.artifact_source_cleanup else
                ARTIFACT_SOURCE_STOP_SELECTOR if a.artifact_source_stop else
                ARTIFACT_STOP_SELECTOR if a.artifact_stop else
                ARTIFACT_CLEANUP_SELECTOR if a.artifact_object_cleanup else
                SECRET_CLEANUP_SELECTOR if a.secret_cleanup else
                AGENT_VARIABLES_FINALIZATION_SELECTOR if a.agent_variables_finalization else
                VARIABLES_CLEANUP_SELECTOR if a.variables_cleanup else
                AGENT_RETIREMENT_SELECTOR if a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history else
                SKILLS_CLEANUP_SELECTOR if a.skills_cleanup else
                AUDIT_SELECTOR if a.audit_cleanup else
                OUTBOX_SELECTOR if a.outbox_cleanup else
                EXECUTION_SELECTOR if a.execution_project_stop else
                RUNNER_COMMAND_SELECTOR if a.runner_command else
                RUNNER_WRITE_SELECTOR if a.runner_write_file else
                RUNNER_SELECTOR if a.runner_directory else
                TASK_SELECTOR if a.task_search else
                SKILL_SELECTOR if a.skill_package_import else
                USAGE_SELECTOR if a.project_usage_summary else
                SECRET_SELECTOR if a.secret_delete else
                CREATE_SELECTOR if a.project_creation else SELECTOR)
    expected = (NATIVE_PG_FTS_EXPECTED if a.native_pg_fts else
                TASK_STATE_FILTER_EXPECTED if a.task_state_filter else
                TASK_BLOCKER_HTTP_EXPECTED if a.task_blocker_http else
                TASK_BLOCKER_WEB_EXPECTED if a.task_blocker_web else
                TASK_CANCEL_EXPECTED if a.task_cancel else
                SPRINT_COMPLETE_HTTP_EXPECTED if a.sprint_complete_http else
                SPRINT_COMPLETE_CLEANUP_EXPECTED if a.sprint_complete_cleanup else
                SPRINT_COMPLETE_WEB_EXPECTED if a.sprint_complete_web else
                RUNTIME_FINAL_TEXT_EXPECTED if a.app_runtime_final_text else
                RUNTIME_FINAL_TEXT_WEB_EXPECTED if a.app_runtime_final_text_web else
                TASK_EXECUTION_READ_EXPECTED if a.app_task_execution_read else
                TASK_EXECUTION_READ_WEB_EXPECTED if a.app_task_execution_read_web else
                TASK_REORDER_EXPECTED if a.task_reorder else
                APP_ARTIFACT_CONTENT_EXPECTED if a.app_artifact_content else
                APP_ARTIFACT_CONTENT_WEB_EXPECTED if a.app_artifact_content_web else
                APP_ARTIFACT_OWNER_HTTP_EXPECTED if a.app_artifact_owner_http else
                APP_ARTIFACT_OWNER_WEB_EXPECTED if a.app_artifact_owner_web else
                APP_HUMAN_ARTIFACT_EXPECTED if a.app_human_artifact else
                RUNNER_MANAGED_CLEANUP_EXPECTED if a.runner_managed_cleanup else
                APP_KNOWLEDGE_LIFECYCLE_EXPECTED if a.app_knowledge_lifecycle else
                APP_USAGE_LIFECYCLE_EXPECTED if a.app_usage_lifecycle else
                APP_EXECUTION_SCHEDULER_OWNERSHIP_EXPECTED if a.app_execution_scheduler_ownership else
                APP_WORK_OWNERSHIP_EXPECTED if a.app_work_ownership else
                WORK_CLEANUP_EXPECTED if a.work_cleanup else
                EXECUTION_RETIREMENT_SEED_DIAGNOSTIC_EXPECTED if a.execution_retirement_seed_diagnostic else
                EXECUTION_RETIREMENT_EXPECTED if a.execution_retirement else
                WORK_READER_STOP_EXPECTED if a.work_reader_stop else
                EXECUTION_CALL_STOP_EXPECTED if a.execution_call_stop else
                SCHEDULER_CLEANUP_EXPECTED if a.scheduler_cleanup else
                SCHEDULER_STOP_EXPECTED if a.scheduler_stop else
                WORK_STOP_EXPECTED if a.work_stop else
                PROJECT_FINALIZE_EXPECTED if a.project_finalize else
                ARTIFACT_SOURCE_CLEANUP_EXPECTED if a.artifact_source_cleanup else
                ARTIFACT_SOURCE_STOP_EXPECTED if a.artifact_source_stop else
                ARTIFACT_STOP_EXPECTED if a.artifact_stop else
                ARTIFACT_CLEANUP_EXPECTED if a.artifact_object_cleanup else
                SECRET_CLEANUP_EXPECTED if a.secret_cleanup else
                AGENT_VARIABLES_FINALIZATION_EXPECTED if a.agent_variables_finalization else
                VARIABLES_CLEANUP_EXPECTED if a.variables_cleanup else
                AGENT_ASSIGNMENT_HISTORY_EXPECTED if a.agent_assignment_history else
                AGENT_INITIAL_GRANT_EXPECTED if a.agent_initial_grant else
                AGENT_RETIREMENT_EXPECTED if a.agent_retirement else
                SKILLS_CLEANUP_EXPECTED if a.skills_cleanup else
                AUDIT_EXPECTED if a.audit_cleanup else
                OUTBOX_EXPECTED if a.outbox_cleanup else
                EXECUTION_EXPECTED if a.execution_project_stop else
                RUNNER_COMMAND_EXPECTED if a.runner_command else
                RUNNER_WRITE_EXPECTED if a.runner_write_file else
                RUNNER_EXPECTED if a.runner_directory else
                TASK_EXPECTED if a.task_search else
                SKILL_EXPECTED if a.skill_package_import else
                USAGE_EXPECTED if a.project_usage_summary else
                SECRET_EXPECTED if a.secret_delete else
                CREATE_EXPECTED if a.project_creation else EXPECTED)
    node_count = (0 if a.native_pg_fts or a.task_blocker_http or a.sprint_complete_http or a.sprint_complete_cleanup or a.runner_command or a.runner_write_file or a.app_runtime_final_text or a.app_task_execution_read or a.app_artifact_content or a.app_artifact_owner_http or a.app_human_artifact or a.runner_managed_cleanup or a.runner_directory or a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup or a.project_finalize or a.work_stop or a.scheduler_stop or a.scheduler_cleanup or a.execution_call_stop or a.work_reader_stop or a.execution_retirement or a.execution_retirement_seed_diagnostic or a.work_cleanup or a.app_work_ownership or a.app_execution_scheduler_ownership or a.app_usage_lifecycle or a.app_knowledge_lifecycle else
                  1 if a.task_state_filter or a.task_blocker_web or a.task_cancel or a.sprint_complete_web or a.app_runtime_final_text_web or a.app_task_execution_read_web or a.app_artifact_content_web or a.app_artifact_owner_web or a.project_creation or a.secret_delete or a.project_usage_summary or a.skill_package_import or a.task_search else 2)
    web_scopes = ([] if a.native_pg_fts or a.task_blocker_http or a.sprint_complete_http or a.sprint_complete_cleanup or a.runner_command or a.runner_write_file or a.app_runtime_final_text or a.app_task_execution_read or a.app_artifact_content or a.app_artifact_owner_http or a.app_human_artifact or a.runner_managed_cleanup or a.runner_directory or a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup or a.project_finalize or a.work_stop or a.scheduler_stop or a.scheduler_cleanup or a.execution_call_stop or a.work_reader_stop or a.execution_retirement or a.execution_retirement_seed_diagnostic or a.work_cleanup or a.app_work_ownership or a.app_execution_scheduler_ownership or a.app_usage_lifecycle or a.app_knowledge_lifecycle else
                  [('AGENTEAM_TASK_STATE_FILTER_WEB', 'task-state-filter')]
                  if a.task_state_filter else
                  [('AGENTEAM_TASK_BLOCKER_MANAGEMENT_WEB', 'task-blocker-management')]
                  if a.task_blocker_web else
                  [('AGENTEAM_TASK_CANCEL_WEB', 'task-cancel')]
                  if a.task_cancel else
                  [('AGENTEAM_SPRINT_COMPLETE_WEB', 'sprint-complete')]
                  if a.sprint_complete_web else
                  [('AGENTEAM_RUNTIME_FINAL_TEXT_WEB', 'runtime-final-text')]
                  if a.app_runtime_final_text_web else
                  [('AGENTEAM_TASK_EXECUTION_READ_WEB', 'task-execution-read')]
                  if a.app_task_execution_read_web else
                  [('AGENTEAM_TASK_REORDER_WEB', 'task-reorder')]
                  if a.task_reorder else
                  [('AGENTEAM_ARTIFACT_CONTENT_WEB', 'artifact-content')]
                  if a.app_artifact_content_web else
                  [('AGENTEAM_ARTIFACT_OWNER_WEB', 'artifact-owner')]
                  if a.app_artifact_owner_web else
                  [('AGENTEAM_TASK_SEARCH_WEB', 'task-search')]
                  if a.task_search else
                  [('AGENTEAM_SKILL_IMPORT_WEB', 'skill-import')]
                  if a.skill_package_import else
                  [('AGENTEAM_PROJECT_USAGE_SUMMARY_WEB', 'usage-summary')]
                  if a.project_usage_summary else
                  [('AGENTEAM_PROJECT_SECRET_DELETE_WEB', 'secret-delete')]
                  if a.secret_delete else
                  [('AGENTEAM_PROJECT_CREATION_WEB', 'project-creation')]
                  if a.project_creation else
                  [('AGENTEAM_TASK_SEARCH_WEB', 'task-search'),
                   ('AGENTEAM_PROJECT_VARIABLES_WEB', 'variable-delete')])
    for path in (a.root, a.binary, a.output, a.runtime):
        assert path.is_absolute()
    assert a.binary.is_file()
    if a.native_pg_fts:
        assert a.native_fts_export is not None and a.native_fts_output is not None
        assert (a.native_fts_export.is_absolute() and a.native_fts_export.is_dir()
                and a.native_fts_export.resolve() == a.native_fts_export)
        # The unchanged candidate validates the completed answer-free export
        # before reading the owned descriptor or querying the package license.
        for name in ('complete.json', 'corpus.jsonl', 'queries.jsonl'):
            path = a.native_fts_export / name
            assert path.is_file() and 0 < path.stat().st_size <= 2 << 20
        assert (a.native_fts_output.is_absolute()
                and a.native_fts_output.resolve() == a.native_fts_output
                and not a.native_fts_output.exists() and not a.native_fts_output.is_symlink()
                and a.native_fts_output not in (a.output, a.runtime))
    else:
        assert a.native_fts_export is None and a.native_fts_output is None
    if a.runner_command or a.runner_write_file or a.runner_directory:
        assert a.dist is None and a.runner_binary is not None
        assert (a.runner_binary.is_absolute() and a.runner_binary.is_file()
                and a.runner_binary.resolve() == a.runner_binary
                and os.access(a.runner_binary, os.X_OK))
    elif a.native_pg_fts or a.task_blocker_http or a.sprint_complete_http or a.sprint_complete_cleanup or a.app_runtime_final_text or a.app_task_execution_read or a.app_artifact_content or a.app_artifact_owner_http or a.app_human_artifact or a.runner_managed_cleanup or a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup or a.project_finalize or a.work_stop or a.scheduler_stop or a.scheduler_cleanup or a.execution_call_stop or a.work_reader_stop or a.execution_retirement or a.execution_retirement_seed_diagnostic or a.work_cleanup or a.app_work_ownership or a.app_execution_scheduler_ownership or a.app_usage_lifecycle or a.app_knowledge_lifecycle:
        assert a.dist is None and a.runner_binary is None
    else:
        assert a.runner_binary is None
        assert a.dist is not None and a.dist.is_absolute() and (a.dist / 'index.html').is_file()
    if a.task_state_filter:
        assert a.artifact_web_harness is None
        harness = a.root / 'tests/account-captcha-web'
        assert harness.is_dir() and harness.resolve() == harness
        assert (harness / 'task-state-filter.config.js').is_file()
    elif a.task_blocker_web:
        assert a.artifact_web_harness is None
        harness = a.root / 'tests/account-captcha-web'
        assert harness.is_dir() and harness.resolve() == harness
        assert (harness / 'task-blocker-management.config.js').is_file()
    elif a.task_cancel:
        assert a.artifact_web_harness is None
        harness = a.root / 'tests/account-captcha-web'
        assert harness.is_dir() and harness.resolve() == harness
        assert (harness / 'task-cancel.config.js').is_file()
    elif a.sprint_complete_web:
        assert a.artifact_web_harness is None
        harness = a.root / 'tests/account-captcha-web'
        assert harness.is_dir() and harness.resolve() == harness
        assert (harness / 'sprint-complete.config.js').is_file()
    elif a.app_runtime_final_text_web:
        assert a.artifact_web_harness == a.root / 'tests/account-captcha-web'
        assert a.artifact_web_harness.is_dir() and a.artifact_web_harness.resolve() == a.artifact_web_harness
        assert (a.artifact_web_harness / 'runtime-final-text.config.js').is_file()
    elif a.app_task_execution_read_web:
        assert a.artifact_web_harness == a.root / 'tests/account-captcha-web'
        assert a.artifact_web_harness.is_dir() and a.artifact_web_harness.resolve() == a.artifact_web_harness
        assert (a.artifact_web_harness / 'task-execution-read.config.js').is_file()
    elif a.app_artifact_content_web:
        expected_harness = (a.root / 'tests/account-captcha-web'
                            if a.root == Path('/workspace/agenteam-task-backlog-reorder')
                            else Path('/workspace/agenteam-artifact-content-ui/tests/account-captcha-web'))
        assert a.artifact_web_harness == expected_harness
        assert a.artifact_web_harness.is_dir() and a.artifact_web_harness.resolve() == a.artifact_web_harness
        assert (a.artifact_web_harness / 'artifact-content.config.js').is_file()
    elif a.app_artifact_owner_web:
        assert a.artifact_web_harness == Path('/workspace/agenteam-artifact-owner-ui/tests/account-captcha-web')
        assert a.artifact_web_harness.is_dir() and a.artifact_web_harness.resolve() == a.artifact_web_harness
        assert (a.artifact_web_harness / 'artifact-owner.config.js').is_file()
    else:
        assert a.artifact_web_harness is None
    assert re.fullmatch('[0-9a-f]{64}', a.input_hash)
    a.output.mkdir(mode=0o700)
    a.runtime.mkdir(mode=0o700)
    for prefix, name in web_scopes:
        (a.output / name).mkdir(mode=0o700)
    if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0):
        raise OSError(ctypes.get_errno(), 'subreaper unavailable')
    env = dict(os.environ, TMPDIR=str(a.runtime), AGENTEAM_AUTH_WEB_RUNTIME=str(a.runtime),
               AGENTEAM_FIXTURE_TEST_BINARY=str(a.binary),
               AGENTEAM_FIXTURE_TEST_CWD=str(a.root / ('tests/search-benchmark' if a.native_pg_fts else 'internal/central/app' if a.app_runtime_final_text or a.app_runtime_final_text_web or a.app_task_execution_read or a.app_task_execution_read_web or a.app_artifact_content or a.app_artifact_content_web or a.app_artifact_owner_http or a.app_artifact_owner_web or a.app_work_ownership or a.app_execution_scheduler_ownership or a.app_usage_lifecycle or a.app_knowledge_lifecycle or a.app_human_artifact else 'tests/model' if a.project_usage_summary else 'tests/projectvariable')),
               AGENTEAM_FIXTURE_OWNED_RECORD=str(a.output / 'owned-resources.json'))
    if a.native_pg_fts or a.task_state_filter or a.task_blocker_http or a.task_blocker_web or a.task_cancel or a.sprint_complete_http or a.sprint_complete_cleanup or a.sprint_complete_web or a.runner_command or a.runner_write_file or a.app_runtime_final_text or a.app_runtime_final_text_web or a.app_task_execution_read or a.app_task_execution_read_web or a.task_reorder or a.app_artifact_content or a.app_artifact_content_web or a.app_artifact_owner_http or a.app_artifact_owner_web or a.app_human_artifact:
        # Ordinary task-owned scope: no live-provider environment or PG16.
        a.pg_current_only = True
        for name in tuple(env):
            if name.startswith('DEV_CHAT_') or name == 'AGENTEAM_LIVE_CHAT_E2E':
                env.pop(name)
    if a.native_pg_fts:
        env['AGENTEAM_NATIVE_FTS_EXPORT'] = str(a.native_fts_export)
        env['AGENTEAM_NATIVE_FTS_OUTPUT'] = str(a.native_fts_output)
    env.pop('AGENTEAM_FIXTURE_PG_CURRENT_ONLY', None)
    if a.pg_current_only:
        env['AGENTEAM_FIXTURE_PG_CURRENT_ONLY'] = '1'
    if a.runner_command or a.runner_write_file or a.runner_directory:
        env['AGENTEAM_RUNNER_BINARY'] = str(a.runner_binary)
    for prefix, name in web_scopes:
        env.update({prefix+'_DIST': str(a.dist), prefix+'_EVIDENCE': str(a.output/name)})
        if prefix not in ('AGENTEAM_SPRINT_COMPLETE_WEB', 'AGENTEAM_TASK_CANCEL_WEB', 'AGENTEAM_TASK_BLOCKER_MANAGEMENT_WEB', 'AGENTEAM_TASK_STATE_FILTER_WEB'):
            env[prefix+'_INPUT_HASH'] = a.input_hash
    if a.app_runtime_final_text_web:
        env['AGENTEAM_RUNTIME_FINAL_TEXT_WEB_HARNESS'] = str(a.artifact_web_harness)
    if a.app_task_execution_read_web:
        env['AGENTEAM_TASK_EXECUTION_READ_WEB_HARNESS'] = str(a.artifact_web_harness)
    if a.app_artifact_content_web:
        env['AGENTEAM_ARTIFACT_CONTENT_WEB_HARNESS'] = str(a.artifact_web_harness)
    if a.app_artifact_owner_web:
        env['AGENTEAM_ARTIFACT_OWNER_WEB_HARNESS'] = str(a.artifact_web_harness)
    command = ['sh', 'scripts/test-objects.sh', '-run', selector]
    save(a.output/'invocation.json', {'command':command,'cwd':str(a.root),
         'input_hash':a.input_hash,'binary':str(a.binary),'dist':str(a.dist) if a.dist else None,
         **({'native_pg_fts':True, 'native_fts_export':str(a.native_fts_export),
             'native_fts_output':str(a.native_fts_output),
             'work_budget_seconds':105, 'total_budget_seconds':120}
            if a.native_pg_fts else {}),
         **({'task_cancel':True} if a.task_cancel else {}),
         **({'sprint_complete_http':True} if a.sprint_complete_http else {}),
         **({'sprint_complete_cleanup':True} if a.sprint_complete_cleanup else {}),
         **({'sprint_complete_web':True} if a.sprint_complete_web else {}),
         **({'runner_command':True,'acceptance_method':'runner-command-owned-v1'} if a.runner_command else {}),
         **({'runner_write_file':True,'acceptance_method':'runner-write-file-owned-v1'} if a.runner_write_file else {}),
         'runner_directory':a.runner_directory,'execution_project_stop':a.execution_project_stop,
         'outbox_cleanup':a.outbox_cleanup,'audit_cleanup':a.audit_cleanup,
         'skills_cleanup':a.skills_cleanup,'agent_retirement':a.agent_retirement,
         'agent_initial_grant':a.agent_initial_grant,
         'agent_assignment_history':a.agent_assignment_history,
         'variables_cleanup':a.variables_cleanup,
         'agent_variables_finalization':a.agent_variables_finalization,
         'secret_cleanup':a.secret_cleanup,
         'artifact_object_cleanup':a.artifact_object_cleanup,'artifact_stop':a.artifact_stop,
         'artifact_source_stop':a.artifact_source_stop,
         'artifact_source_cleanup':a.artifact_source_cleanup,
         'project_finalize':a.project_finalize,
         'work_stop':a.work_stop,
         'scheduler_stop':a.scheduler_stop,
         'scheduler_cleanup':a.scheduler_cleanup,
         'execution_call_stop':a.execution_call_stop,
         'work_reader_stop':a.work_reader_stop,
         'execution_retirement':a.execution_retirement,
         'execution_retirement_seed_diagnostic':a.execution_retirement_seed_diagnostic,
         'work_cleanup':a.work_cleanup,
         'app_work_ownership':a.app_work_ownership,
         'app_execution_scheduler_ownership':a.app_execution_scheduler_ownership,
         'app_usage_lifecycle':a.app_usage_lifecycle,
         'app_knowledge_lifecycle':a.app_knowledge_lifecycle,
         'runner_managed_cleanup':a.runner_managed_cleanup,
         **({'app_runtime_final_text':a.app_runtime_final_text,
             'app_runtime_final_text_web':a.app_runtime_final_text_web,
             'acceptance_method':'runtime-final-text-owned-v1'}
            if a.app_runtime_final_text or a.app_runtime_final_text_web else {}),
         **({'app_task_execution_read':a.app_task_execution_read,
             'app_task_execution_read_web':a.app_task_execution_read_web,
             'acceptance_method':'task-execution-read-owned-v1'}
            if a.app_task_execution_read or a.app_task_execution_read_web else {}),
         **({'task_reorder':True, 'acceptance_method':'task-reorder-owned-v1'}
            if a.task_reorder else {}),
         **({'app_human_artifact':True, 'acceptance_method':'human-artifact-owned-v1'}
            if a.app_human_artifact else {}),
         **({'app_artifact_content':a.app_artifact_content,
             'app_artifact_content_web':a.app_artifact_content_web,
             'artifact_web_harness':str(a.artifact_web_harness) if a.artifact_web_harness else None,
             'acceptance_method':'artifact-content-owned-v1'}
            if a.app_artifact_content or a.app_artifact_content_web else {}),
         **({'app_artifact_owner_http':a.app_artifact_owner_http,
             'app_artifact_owner_web':a.app_artifact_owner_web,
             'artifact_web_harness':str(a.artifact_web_harness) if a.artifact_web_harness else None,
             'acceptance_method':'artifact-owner-owned-v1'}
            if a.app_artifact_owner_http or a.app_artifact_owner_web else {}),
         'runner_binary':str(a.runner_binary) if a.runner_binary else None,
         'runtime':str(a.runtime),'supervisor_pid':os.getpid(),
         'pg_current_only':a.pg_current_only,'project_creation':a.project_creation,
         'secret_delete':a.secret_delete,'project_usage_summary':a.project_usage_summary,
         'skill_package_import':a.skill_package_import,'task_search':a.task_search})
    began = time.monotonic()
    failed = False
    adopted = []
    child = None
    def stop(signum, frame):
        nonlocal failed
        failed = True
        if child is not None and child.poll() is None:
            child.terminate()  # Original shell forwards TERM and waits fixture cleanup.
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, stop)
    with (a.output/'browser.log').open('x', buffering=1) as log:
        child = subprocess.Popen(command, cwd=a.root, env=env, stdout=log,
                                 stderr=subprocess.STDOUT, start_new_session=True)
        print(json.dumps({'fixture_pid':child.pid,'supervisor_pid':os.getpid()}), flush=True)
        try:
            # D13 keeps the original 105s setup/body and 120s total boundary.
            # Any timeout remains FAIL while the original cleanup still joins.
            actual = child.wait(timeout=max(0, 105 - (time.monotonic() - began))
                                if a.native_pg_fts else 540)
        except subprocess.TimeoutExpired:
            failed = True
            child.terminate()
            try:
                actual = child.wait(timeout=60)
            except subprocess.TimeoutExpired:
                child.kill()
                actual = child.wait(timeout=5)
        log.write(f'UI actual_fixture_wait pid={child.pid} code={actual}\n')
        # Chromium may orphan already-exiting descendants to the owning subreaper.
        deadline = time.monotonic()+5
        while True:
            try:
                pid, status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                break
            if pid:
                adopted.append({'pid':pid,'actual_wait_status':status})
                log.write(f'UI adopted_actual_wait pid={pid} status={status}\n')
                failed |= status != 0
            elif time.monotonic() < deadline:
                time.sleep(.02)
            else:
                failed = True
                # A leak is FAIL; terminate only children actually owned by us.
                for pid in children():
                    try: os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError: pass
                deadline = time.monotonic()+3
                while children() and time.monotonic() < deadline:
                    try:
                        pid, status = os.waitpid(-1, os.WNOHANG)
                    except ChildProcessError:
                        break
                    if pid:
                        adopted.append({'pid':pid,'actual_wait_status':status})
                        log.write(f'UI adopted_actual_wait pid={pid} status={status}\n')
                    else: time.sleep(.02)
                break
        log.flush()
    raw = (a.output/'browser.log').read_text()
    runs = re.findall(r'^=== RUN\s+(\S+)\s*$', raw, re.M)
    results = re.findall(r'^\s*--- (PASS|FAIL|SKIP): (\S+) \(', raw, re.M)
    exact = (len(runs)==len(expected) and set(runs)==expected and len(results)==len(expected)
             and all(s=='PASS' for s,n in results) and {n for s,n in results}==expected
             and not re.search(r'^FAIL(?:\s|$)|^panic:|WARNING: DATA RACE',raw,re.M))
    go_waits = re.findall(r'^D03 explicit test actual_wait pid=(\d+) code=(-?\d+) selector=(.*)$',raw,re.M)
    node_waits = re.findall(r'Node actual_wait pid=(\d+) success=(true|false)',raw)
    exact &= (len(go_waits)==1 and go_waits[0][1:]==('0',selector)
              and len(node_waits)==node_count and all(ok=='true' for pid,ok in node_waits))
    runner_waits = re.findall(r'runner_cli_original_wait pid=(\d+) exit=(\d+)', raw)
    if a.runner_command or a.runner_write_file:
        exact &= (len(runner_waits)==3 and len({pid for pid,code in runner_waits})==3
                  and [code for pid,code in runner_waits]==['0','0','0'])
    if a.runner_directory:
        exact &= (len(runner_waits)==9 and len({pid for pid,code in runner_waits})==9
                  and [code for pid,code in runner_waits]==['0','0','0','0','0','1','2','2','2'])
    resources=[]
    record=a.output/'owned-resources.json'
    if record.is_file():
        owned=json.loads(record.read_text())
        for r in owned['resources']:
            assert r['kind'] in ('container','network') and re.fullmatch('[0-9a-f]{64}',r['id'])
            check=subprocess.run(['docker',r['kind'],'inspect',r['id']],capture_output=True,text=True,timeout=15)
            # Don't treat a daemon/permission error as absence.
            absent=check.returncode==1 and ('No such '+r['kind'] in check.stderr or 'not found' in check.stderr)
            resources.append(dict(r,absent=absent,inspect_exit=check.returncode))
        directories_absent=all(not Path(d).exists() for d in owned['directories'])
    else:
        directories_absent=False
    leftovers=sorted(str(p.relative_to(a.runtime)) for p in a.runtime.iterdir())
    expected_containers = 3 if a.pg_current_only else 4
    resources_match = (sum(r['kind']=='container' for r in resources)==expected_containers
                       and sum(r['kind']=='network' for r in resources)==3)
    okay=(not failed and actual==0 and exact and resources_match
          and all(r['absent'] for r in resources) and directories_absent
          and not leftovers and not children())
    if not leftovers: a.runtime.rmdir()
    elapsed = time.monotonic() - began
    if a.native_pg_fts:
        okay &= elapsed <= 120  # Failure cleanup grace never extends acceptance.
    result={'fixture_pid':child.pid,'fixture_actual_wait_exit':actual,
            'elapsed_seconds':round(elapsed,3),'exact_tests':bool(exact),
            'go_actual_waits':go_waits,'node_actual_waits':node_waits,
            'runner_actual_waits':runner_waits,'adopted_actual_waits':adopted,
            'resources':resources,'private_directories_absent':directories_absent,
            'runtime_leftovers':leftovers,'remaining_children':children(),'exit_code':int(not okay)}
    if a.native_pg_fts:
        result.update(native_pg_fts=True, work_budget_seconds=105,
                      total_budget_seconds=120, within_total_budget=elapsed <= 120)
    if a.runner_command:
        result['acceptance_method'] = 'runner-command-owned-v1'
    if a.runner_write_file:
        result['acceptance_method'] = 'runner-write-file-owned-v1'
    if a.app_human_artifact:
        result['acceptance_method'] = 'human-artifact-owned-v1'
    if a.app_artifact_owner_http or a.app_artifact_owner_web:
        result['acceptance_method'] = 'artifact-owner-owned-v1'
    if a.app_artifact_content or a.app_artifact_content_web:
        result['acceptance_method'] = 'artifact-content-owned-v1'
    if a.task_reorder:
        result['acceptance_method'] = 'task-reorder-owned-v1'
    if a.app_runtime_final_text or a.app_runtime_final_text_web:
        result['acceptance_method'] = 'runtime-final-text-owned-v1'
    if a.app_task_execution_read or a.app_task_execution_read_web:
        result['acceptance_method'] = 'task-execution-read-owned-v1'
    save(a.output/'result.json',result)
    print(json.dumps(result),flush=True)
    return int(not okay)

if __name__=='__main__':
    raise SystemExit(main())
