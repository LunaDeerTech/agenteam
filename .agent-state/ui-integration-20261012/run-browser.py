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
    p.add_argument('--runner-binary', type=Path)
    p.add_argument('--input-hash', required=True)
    p.add_argument('--pg-current-only', action='store_true',
                   help='explicitly omit the unused PG16 negative-control fixture')
    scope = p.add_mutually_exclusive_group()
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
    a = p.parse_args()
    selector = (ARTIFACT_SOURCE_CLEANUP_SELECTOR if a.artifact_source_cleanup else
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
                RUNNER_SELECTOR if a.runner_directory else
                TASK_SELECTOR if a.task_search else
                SKILL_SELECTOR if a.skill_package_import else
                USAGE_SELECTOR if a.project_usage_summary else
                SECRET_SELECTOR if a.secret_delete else
                CREATE_SELECTOR if a.project_creation else SELECTOR)
    expected = (ARTIFACT_SOURCE_CLEANUP_EXPECTED if a.artifact_source_cleanup else
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
                RUNNER_EXPECTED if a.runner_directory else
                TASK_EXPECTED if a.task_search else
                SKILL_EXPECTED if a.skill_package_import else
                USAGE_EXPECTED if a.project_usage_summary else
                SECRET_EXPECTED if a.secret_delete else
                CREATE_EXPECTED if a.project_creation else EXPECTED)
    node_count = (0 if a.runner_directory or a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup else
                  1 if a.project_creation or a.secret_delete or a.project_usage_summary or a.skill_package_import or a.task_search else 2)
    web_scopes = ([] if a.runner_directory or a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup else
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
    if a.runner_directory:
        assert a.dist is None and a.runner_binary is not None
        assert (a.runner_binary.is_absolute() and a.runner_binary.is_file()
                and a.runner_binary.resolve() == a.runner_binary
                and os.access(a.runner_binary, os.X_OK))
    elif a.execution_project_stop or a.outbox_cleanup or a.audit_cleanup or a.skills_cleanup or a.agent_retirement or a.agent_initial_grant or a.agent_assignment_history or a.variables_cleanup or a.agent_variables_finalization or a.secret_cleanup or a.artifact_object_cleanup or a.artifact_stop or a.artifact_source_stop or a.artifact_source_cleanup:
        assert a.dist is None and a.runner_binary is None
    else:
        assert a.runner_binary is None
        assert a.dist is not None and a.dist.is_absolute() and (a.dist / 'index.html').is_file()
    assert re.fullmatch('[0-9a-f]{64}', a.input_hash)
    a.output.mkdir(mode=0o700)
    a.runtime.mkdir(mode=0o700)
    for prefix, name in web_scopes:
        (a.output / name).mkdir(mode=0o700)
    if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0):
        raise OSError(ctypes.get_errno(), 'subreaper unavailable')
    env = dict(os.environ, TMPDIR=str(a.runtime), AGENTEAM_AUTH_WEB_RUNTIME=str(a.runtime),
               AGENTEAM_FIXTURE_TEST_BINARY=str(a.binary),
               AGENTEAM_FIXTURE_TEST_CWD=str(a.root / ('tests/model' if a.project_usage_summary else 'tests/projectvariable')),
               AGENTEAM_FIXTURE_OWNED_RECORD=str(a.output / 'owned-resources.json'))
    env.pop('AGENTEAM_FIXTURE_PG_CURRENT_ONLY', None)
    if a.pg_current_only:
        env['AGENTEAM_FIXTURE_PG_CURRENT_ONLY'] = '1'
    if a.runner_directory:
        env['AGENTEAM_RUNNER_BINARY'] = str(a.runner_binary)
    for prefix, name in web_scopes:
        env.update({prefix+'_DIST': str(a.dist), prefix+'_EVIDENCE': str(a.output/name),
                    prefix+'_INPUT_HASH': a.input_hash})
    command = ['sh', 'scripts/test-objects.sh', '-run', selector]
    save(a.output/'invocation.json', {'command':command,'cwd':str(a.root),
         'input_hash':a.input_hash,'binary':str(a.binary),'dist':str(a.dist) if a.dist else None,
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
            actual = child.wait(timeout=540)
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
    result={'fixture_pid':child.pid,'fixture_actual_wait_exit':actual,
            'elapsed_seconds':round(time.monotonic()-began,3),'exact_tests':bool(exact),
            'go_actual_waits':go_waits,'node_actual_waits':node_waits,
            'runner_actual_waits':runner_waits,'adopted_actual_waits':adopted,
            'resources':resources,'private_directories_absent':directories_absent,
            'runtime_leftovers':leftovers,'remaining_children':children(),'exit_code':int(not okay)}
    save(a.output/'result.json',result)
    print(json.dumps(result),flush=True)
    return int(not okay)

if __name__=='__main__':
    raise SystemExit(main())
