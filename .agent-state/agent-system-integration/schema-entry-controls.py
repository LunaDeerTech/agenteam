#!/usr/bin/env python3
"""System profiles on main's existing metadata family; no Go, Docker or sockets."""
import ast
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SYSTEM_INPUTS = {'^TestSchedulerRetryBinding$': ('tests/projectvariable/scheduler_retry_binding_test.go', 'tests/projectvariable/scheduler_claim_test.go'),
 '^TestAgentConfigurationSchema$': ('tests/projectvariable/agent_configuration_schema_test.go',
                                    'tests/testsupport/agentconfiguration/assembly.go'),
 '^TestAgentRuntimeSchema$': ('tests/projectvariable/agent_runtime_schema_test.go',),
 '^TestExecutionPreparation$': ('tests/projectvariable/execution_preparation_test.go',),
 '^TestAgentConfigurationCreate$': ('tests/projectvariable/agent_configuration_create_test.go',
                                    'tests/projectvariable/agent_configuration_facts_test.go',
                                    'tests/projectvariable/skill_installation_test.go',
                                    'tests/testsupport/agentconfiguration/assembly.go'),
 '^TestTaskTransitionHuman$': ('tests/projectvariable/task_transition_scheduler_test.go',),
 '^TestSchedulerClaim$': ('tests/projectvariable/scheduler_claim_test.go',),
 '^TestSchedulerLaunch$': ('tests/projectvariable/scheduler_launch_test.go',),
 '^TestSchedulerBusyCompensation$': ('tests/projectvariable/scheduler_busy_compensation_test.go',),
 '^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': ('tests/projectvariable/scheduler_pending_visit_test.go',
                                                     'tests/projectvariable/task_human_http_test.go'),
 '^TestSprintStartHTTP$': ('tests/projectvariable/task_human_http_test.go',),
 '^TestSchedulerLaunchFinalFailure$': ('tests/projectvariable/scheduler_launch_failure_test.go',),
 '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': ('tests/projectvariable/task_technical_resolution_http_test.go', 'tests/projectvariable/task_unblock_atomic_test.go')}
SYSTEM_CASES = {'^TestSchedulerRetryBinding$': ('TestSchedulerRetryBinding', 'TestSchedulerRetryBinding/config-bound-claim-and-real-lock-timeout', 'TestSchedulerRetryBinding/legacy-null-policy-stays-unbound'),
 '^TestAgentConfigurationSchema$': ('TestAgentConfigurationSchema',
                                    'TestAgentConfigurationSchema/fresh-prefix-and-repeat',
                                    'TestAgentConfigurationSchema/schema-invariants-and-unbound-dependencies',
                                    'TestAgentConfigurationSchema/upgrade-preserves-facts-and-audit-checks'),
 '^TestAgentRuntimeSchema$': ('TestAgentRuntimeSchema',
                              'TestAgentRuntimeSchema/execution-slot-and-unbound-launch',
                              'TestAgentRuntimeSchema/human-compatibility-and-agent-origin',
                              'TestAgentRuntimeSchema/prefix36-upgrade-and-repeat',
                              'TestAgentRuntimeSchema/runtime-attempt-and-terminal'),
 '^TestExecutionPreparation$': ('TestExecutionPreparation',
                                'TestExecutionPreparation/current-owner-task-input',
                                'TestExecutionPreparation/prefix39-upgrade-and-repeat',
                                'TestExecutionPreparation/preparation-claim-and-attempt',
                                'TestExecutionPreparation/project-preparation-gate'),
 '^TestAgentConfigurationCreate$': ('TestAgentConfigurationCreate',
                                    'TestAgentConfigurationCreate/default-create-and-replay',
                                    'TestAgentConfigurationCreate/final-transaction-rollback'),
 '^TestTaskTransitionHuman$': ('TestTaskTransitionHuman',
                               'TestTaskTransitionHuman/assignment-config-and-replay',
                               'TestTaskTransitionHuman/final-transaction-rollback'),
 '^TestSchedulerClaim$': ('TestSchedulerClaim',
                          'TestSchedulerClaim/final-transaction-rollback',
                          'TestSchedulerClaim/start-sprint-claim-and-replay'),
 '^TestSchedulerLaunch$': ('TestSchedulerLaunch',
                           'TestSchedulerLaunch/association-failure-lookup-recovery',
                           'TestSchedulerLaunch/created-association-and-replay'),
 '^TestSchedulerBusyCompensation$': ('TestSchedulerBusyCompensation',
                                     'TestSchedulerBusyCompensation/preserve-user-update',
                                     'TestSchedulerBusyCompensation/rollback-restore-and-replay'),
 '^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': ('TestSchedulerPendingVisit',
                                                     'TestSchedulerPendingVisit/association-rollback-original-lookup',
                                                     'TestSchedulerPendingVisit/paused-enumeration-and-resume',
                                                     'TestTaskHumanHTTP',
                                                     'TestTaskHumanHTTP/owner-csrf-and-new-session-lookup',
                                                     'TestTaskHumanHTTP/transfer-lookup-replay-and-get'),
 '^TestSprintStartHTTP$': ('TestSprintStartHTTP', 'TestSprintStartHTTP/paused-start-get-lookup-replay'),
 '^TestSchedulerLaunchFinalFailure$': ('TestSchedulerLaunchFinalFailure',
                                       'TestSchedulerLaunchFinalFailure/title-preserved-technical-blocker-and-replay',
                                       'TestSchedulerLaunchFinalFailure/late-transaction-rollback-and-settlement'),
 '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': ('TestTaskTechnicalResolutionHTTP', 'TestTaskTechnicalResolutionHTTP/resolve-to-todo-lookup-replay', 'TestTaskTechnicalResolutionAtomic', 'TestTaskTechnicalResolutionAtomic/late-transaction-rollback-and-replay')}
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': '776e6306214722a1eb9f6ca124c12f5e05a3f71d3daffaf3a155c8cee747009c',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'ce09376d0db54c1ef805974c491836cb854f3e23468e0414b0ef31235d00e589'}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [('METADATA_INPUTS = {\n',
                                                        'METADATA_INPUTS = {\n'
                                                        "    '^TestSchedulerRetryBinding$': (\n        'tests/projectvariable/scheduler_retry_binding_test.go',\n        'tests/projectvariable/scheduler_claim_test.go',\n    ),\n"
                                                        "    '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': (\n        'tests/projectvariable/task_technical_resolution_http_test.go',\n        'tests/projectvariable/task_unblock_atomic_test.go',\n    ),\n"
                                                        "    '^TestSchedulerLaunchFinalFailure$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/scheduler_launch_failure_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestAgentConfigurationSchema$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/agent_configuration_schema_test.go',\n"
                                                        '        '
                                                        "'tests/testsupport/agentconfiguration/assembly.go',\n"
                                                        '    ),\n'
                                                        "    '^TestAgentRuntimeSchema$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/agent_runtime_schema_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestExecutionPreparation$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/execution_preparation_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestAgentConfigurationCreate$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/agent_configuration_create_test.go',\n"
                                                        '        '
                                                        "'tests/projectvariable/agent_configuration_facts_test.go',\n"
                                                        '        '
                                                        "'tests/projectvariable/skill_installation_test.go',\n"
                                                        '        '
                                                        "'tests/testsupport/agentconfiguration/assembly.go',\n"
                                                        '    ),\n'
                                                        "    '^TestTaskTransitionHuman$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/task_transition_scheduler_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestSchedulerClaim$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/scheduler_claim_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestSchedulerLaunch$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/scheduler_launch_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestSchedulerBusyCompensation$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/scheduler_busy_compensation_test.go',\n"
                                                        '    ),\n'
                                                        '    '
                                                        "'^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': "
                                                        '(\n'
                                                        '        '
                                                        "'tests/projectvariable/scheduler_pending_visit_test.go',\n"
                                                        '        '
                                                        "'tests/projectvariable/task_human_http_test.go',\n"
                                                        '    ),\n'
                                                        "    '^TestSprintStartHTTP$': (\n"
                                                        '        '
                                                        "'tests/projectvariable/task_human_http_test.go',\n"
                                                        '    ),\n')],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {\n',
                                                                'METADATA_GROUPS = {\n'
                                                                "    '^TestSchedulerRetryBinding$': frozenset({\n        'TestSchedulerRetryBinding',\n        'TestSchedulerRetryBinding/config-bound-claim-and-real-lock-timeout',\n        'TestSchedulerRetryBinding/legacy-null-policy-stays-unbound',\n    }),\n"
                                                                "    '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': frozenset({\n        'TestTaskTechnicalResolutionHTTP',\n        'TestTaskTechnicalResolutionHTTP/resolve-to-todo-lookup-replay',\n        'TestTaskTechnicalResolutionAtomic',\n        'TestTaskTechnicalResolutionAtomic/late-transaction-rollback-and-replay',\n    }),\n"
                                                                "    '^TestSchedulerLaunchFinalFailure$': "
                                                                'frozenset({\n'
                                                                "        'TestSchedulerLaunchFinalFailure',\n"
                                                                '        '
                                                                "'TestSchedulerLaunchFinalFailure/title-preserved-technical-blocker-and-replay',\n"
                                                                '        '
                                                                "'TestSchedulerLaunchFinalFailure/late-transaction-rollback-and-settlement',\n"
                                                                '    }),\n'
                                                                "    '^TestAgentConfigurationSchema$': "
                                                                'frozenset({\n'
                                                                "        'TestAgentConfigurationSchema',\n"
                                                                '        '
                                                                "'TestAgentConfigurationSchema/fresh-prefix-and-repeat',\n"
                                                                '        '
                                                                "'TestAgentConfigurationSchema/schema-invariants-and-unbound-dependencies',\n"
                                                                '        '
                                                                "'TestAgentConfigurationSchema/upgrade-preserves-facts-and-audit-checks',\n"
                                                                '    }),\n'
                                                                "    '^TestAgentRuntimeSchema$': "
                                                                'frozenset({\n'
                                                                "        'TestAgentRuntimeSchema',\n"
                                                                '        '
                                                                "'TestAgentRuntimeSchema/execution-slot-and-unbound-launch',\n"
                                                                '        '
                                                                "'TestAgentRuntimeSchema/human-compatibility-and-agent-origin',\n"
                                                                '        '
                                                                "'TestAgentRuntimeSchema/prefix36-upgrade-and-repeat',\n"
                                                                '        '
                                                                "'TestAgentRuntimeSchema/runtime-attempt-and-terminal',\n"
                                                                '    }),\n'
                                                                "    '^TestExecutionPreparation$': "
                                                                'frozenset({\n'
                                                                "        'TestExecutionPreparation',\n"
                                                                '        '
                                                                "'TestExecutionPreparation/current-owner-task-input',\n"
                                                                '        '
                                                                "'TestExecutionPreparation/prefix39-upgrade-and-repeat',\n"
                                                                '        '
                                                                "'TestExecutionPreparation/preparation-claim-and-attempt',\n"
                                                                '        '
                                                                "'TestExecutionPreparation/project-preparation-gate',\n"
                                                                '    }),\n'
                                                                "    '^TestAgentConfigurationCreate$': "
                                                                'frozenset({\n'
                                                                "        'TestAgentConfigurationCreate',\n"
                                                                '        '
                                                                "'TestAgentConfigurationCreate/default-create-and-replay',\n"
                                                                '        '
                                                                "'TestAgentConfigurationCreate/final-transaction-rollback',\n"
                                                                '    }),\n'
                                                                "    '^TestTaskTransitionHuman$': "
                                                                'frozenset({\n'
                                                                "        'TestTaskTransitionHuman',\n"
                                                                '        '
                                                                "'TestTaskTransitionHuman/assignment-config-and-replay',\n"
                                                                '        '
                                                                "'TestTaskTransitionHuman/final-transaction-rollback',\n"
                                                                '    }),\n'
                                                                "    '^TestSchedulerClaim$': frozenset({\n"
                                                                "        'TestSchedulerClaim',\n"
                                                                '        '
                                                                "'TestSchedulerClaim/final-transaction-rollback',\n"
                                                                '        '
                                                                "'TestSchedulerClaim/start-sprint-claim-and-replay',\n"
                                                                '    }),\n'
                                                                "    '^TestSchedulerLaunch$': frozenset({\n"
                                                                "        'TestSchedulerLaunch',\n"
                                                                '        '
                                                                "'TestSchedulerLaunch/association-failure-lookup-recovery',\n"
                                                                '        '
                                                                "'TestSchedulerLaunch/created-association-and-replay',\n"
                                                                '    }),\n'
                                                                "    '^TestSchedulerBusyCompensation$': "
                                                                'frozenset({\n'
                                                                "        'TestSchedulerBusyCompensation',\n"
                                                                '        '
                                                                "'TestSchedulerBusyCompensation/preserve-user-update',\n"
                                                                '        '
                                                                "'TestSchedulerBusyCompensation/rollback-restore-and-replay',\n"
                                                                '    }),\n'
                                                                '    '
                                                                "'^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': "
                                                                'frozenset({\n'
                                                                "        'TestSchedulerPendingVisit',\n"
                                                                '        '
                                                                "'TestSchedulerPendingVisit/association-rollback-original-lookup',\n"
                                                                '        '
                                                                "'TestSchedulerPendingVisit/paused-enumeration-and-resume',\n"
                                                                "        'TestTaskHumanHTTP',\n"
                                                                '        '
                                                                "'TestTaskHumanHTTP/owner-csrf-and-new-session-lookup',\n"
                                                                '        '
                                                                "'TestTaskHumanHTTP/transfer-lookup-replay-and-get',\n"
                                                                '    }),\n'
                                                                "    '^TestSprintStartHTTP$': frozenset({\n"
                                                                "        'TestSprintStartHTTP',\n"
                                                                '        '
                                                                "'TestSprintStartHTTP/paused-start-get-lookup-replay',\n"
                                                                '    }),\n'),
                                                               ("    if 'TestSkillInstallation' in args.run",
                                                                '    if any(name in args.run for name in '
                                                                "('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema', "
                                                                "'AgentRuntimeSchema', "
                                                                "'ExecutionPreparation', "
                                                                "'AgentConfigurationCreate', "
                                                                "'TaskTransitionHuman', 'SchedulerClaim', "
                                                                "'SchedulerLaunch', "
                                                                "'SchedulerBusyCompensation', "
                                                                "'SchedulerPendingVisit', 'TaskHumanHTTP', "
                                                                "'SprintStartHTTP', 'TaskTechnicalResolutionHTTP', 'TaskTechnicalResolutionAtomic', 'SchedulerRetryBinding')) and (args.run not in "
                                                                'METADATA_GROUPS or not args.root_chain):\n'
                                                                "        parser.error('System configuration "
                                                                'requires one exact original root-chain '
                                                                "profile')\n"
                                                                "    if 'TestSkillInstallation' in args.run"),
                                                               ('def descendants(root):\n',
                                                                'def tcp_failure_sample(delta, phase):\n'
                                                                '    """Project only the last existing '
                                                                'observation; never poll or infer '
                                                                'ownership."""\n'
                                                                "    if phase not in ('supervisor', "
                                                                "'outer'):\n"
                                                                "        raise ValueError('exact TCP failure "
                                                                "phase required')\n"
                                                                '    rows = sorted(delta)\n'
                                                                "    return {'phase': phase, 'utc': "
                                                                "time.strftime('%Y-%m-%dT%H:%M:%SZ', "
                                                                'time.gmtime()),\n'
                                                                "            'total': len(rows), "
                                                                "'truncated': len(rows) > 32,\n"
                                                                "            'rows': [dict(zip(('family', "
                                                                "'localhex', 'remotehex', 'state', 'inode'), "
                                                                'row))\n'
                                                                '                     for row in '
                                                                'rows[:32]]}\n'
                                                                '\n'
                                                                '\n'
                                                                'def descendants(root):\n'),
                                                               ('            tail_deadline = '
                                                                'time.monotonic() + 75\n'
                                                                '            empty = 0\n',
                                                                '            tail_deadline = '
                                                                'time.monotonic() + 75\n'
                                                                '            empty, delta = 0, set()\n'),
                                                               ("                log.write(f'STOP host TCP "
                                                                'delta tail not empty: {len(tcp() - '
                                                                "baseline)} rows\\n')\n",
                                                                "                log.write(f'STOP host TCP "
                                                                'delta tail not empty: {len(delta)} '
                                                                "rows\\n')\n"
                                                                '                sample = '
                                                                "tcp_failure_sample(delta, 'supervisor')\n"
                                                                "                log.write('HOST_TCP "
                                                                "failure_sample=' + json.dumps(sample, "
                                                                "sort_keys=True) + '\\n')\n"
                                                                '                # stdout is retained in the '
                                                                "task's existing private supervisor.log.\n"
                                                                '                '
                                                                "print(json.dumps({'host_tcp_failure': "
                                                                'sample}, sort_keys=True), flush=True)\n')]}


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    for before, after in reversed(SOURCE_HUNKS[name]):
        if source.count(after) != 1:
            raise ValueError('unknown or ambiguous System profile delta')
        source = source.replace(after, before, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown main baseline change')
    return source


def projection(name, source):
    if any(selector in source for selector in SYSTEM_INPUTS):
        return inverse(name, source)
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def output(selector, cases):
    return (''.join('=== RUN   ' + name + '\n--- PASS: ' + name + ' (0.01s)\n' for name in cases)
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + selector + '\n')


class SystemEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('system_union_driver', DRIVER)
        cls.sup = load('system_union_sup', SUP)

    def test_inverse_and_original_profiles(self):
        for name, pairs in SOURCE_HUNKS.items():
            source = (ROOT / name).read_text()
            ast.parse(source)
            restored = inverse(name, source)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            baseline = {'__file__': str(ROOT / name), '__name__': 'main_projection'}
            exec(compile(restored, name, 'exec'), baseline)
            if name == DRIVER:
                self.assertEqual({k:v for k,v in self.driver.METADATA_INPUTS.items() if k not in SYSTEM_INPUTS}, baseline['METADATA_INPUTS'])
                self.assertEqual({k:v for k,v in self.driver.TARGETS.items() if k not in SYSTEM_INPUTS}, baseline['TARGETS'])
            else:
                self.assertEqual({k:v for k,v in self.sup.METADATA_GROUPS.items() if k not in SYSTEM_INPUTS}, baseline['METADATA_GROUPS'])
            for before, after in pairs:
                self.assertEqual(source.count(after), 1)
                for bad in (source.replace(after,before,1), source+after, source+'\n# unknown\n'):
                    with self.assertRaises(ValueError): inverse(name,bad)
        self.assertEqual(set(self.driver.METADATA_INPUTS),set(self.sup.METADATA_GROUPS))
        for key, paths in SYSTEM_INPUTS.items():
            self.assertEqual(self.driver.METADATA_INPUTS[key],paths)
            self.assertEqual(self.sup.METADATA_GROUPS[key],frozenset(SYSTEM_CASES[key]))
        self.assertEqual(self.sup.budgets(True),(540,60))
        self.assertEqual(self.sup.budgets(False),(123,3))

    def test_exact_results_root_mode_and_original_wait(self):
        base=['supervisor','--driver','/unused/driver','--binary','/unused/candidate','--output','/unused/out','--run']
        for selector,cases in SYSTEM_CASES.items():
            good=output(selector,cases)
            self.assertTrue(self.sup.metadata_results(good,selector))
            for name in cases:
                run,passed='=== RUN   '+name+'\n','--- PASS: '+name+' (0.01s)\n'
                for bad in (good.replace(run,''),good.replace(passed,''),good+run,good+passed,
                            good.replace(passed,passed.replace('PASS','SKIP')),good.replace(passed,passed.replace('PASS','FAIL'))):
                    self.assertFalse(self.sup.metadata_results(bad,selector))
            wait=good.splitlines(True)[-1]
            for bad in (good+wait,good.replace(wait,''),good.replace('code=0','code=1'),good+'FAIL\n'):
                self.assertFalse(self.sup.metadata_results(bad,selector))
            tops=[name for name in cases if '/' not in name]
            for bad,root in [(selector,False),(selector+'x',True)]+[(name,True) for name in tops]:
                with patch.object(sys,'argv',base+[bad]+(['--root-chain'] if root else [])), \
                        patch.object(self.sup,'budgets') as budget,patch.object(self.sup.subprocess,'Popen') as spawn, \
                        patch('sys.stderr',io.StringIO()):
                    with self.assertRaises(SystemExit):self.sup.main()
                    budget.assert_not_called();spawn.assert_not_called()
            class ReachedBudget(Exception):pass
            with patch.object(sys,'argv',base+[selector,'--root-chain']),patch.object(self.sup,'budgets',side_effect=ReachedBudget) as budget:
                with self.assertRaises(ReachedBudget):self.sup.main()
                budget.assert_called_once_with(True)

    def test_actual_inputs_and_final_reenumeration(self):
        for selector,required in SYSTEM_INPUTS.items():
            with tempfile.TemporaryDirectory(prefix='system-union-input-') as tmp:
                root=Path(tmp).resolve()
                names=('candidate.test','tests/projectvariable/original_test.go','internal/other/assets/NOTICE','tests/testsupport/original.go',*required)
                for name in names:
                    path=root/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('source')
                binary=root/'candidate.test'
                with patch.object(self.driver,'REPOSITORY',root),patch.object(self.driver,'input_paths',return_value=[binary]):
                    paths=self.driver.metadata_inputs(binary,selector)
                    self.assertEqual(set(paths),{root/name for name in names})
                    inputs={str(p):self.driver.sha(p) for p in paths};args=SimpleNamespace(binary=binary)
                    self.assertTrue(self.sup.metadata_same(inputs,args,self.driver,selector))
                    added=root/'tests/projectvariable/later_test.go';added.write_text('new')
                    self.assertFalse(self.sup.metadata_same(inputs,args,self.driver,selector));added.unlink()
                    for name in required:
                        path=root/name;path.write_text('changed')
                        self.assertFalse(self.sup.metadata_same(inputs,args,self.driver,selector));path.unlink()
                        self.assertFalse(self.sup.metadata_same(inputs,args,self.driver,selector));path.symlink_to(binary)
                        self.assertFalse(self.sup.metadata_same(inputs,args,self.driver,selector));path.unlink();path.write_text('source')
                    with self.assertRaises(ValueError):self.driver.metadata_inputs(binary,selector+'x')

    def test_original_observer_requires_all_resource_tails(self):
        for selector,cases in SYSTEM_CASES.items():
            with tempfile.TemporaryDirectory(prefix='system-union-observer-') as tmp:
                root=Path(tmp);runtime=root/'runtime';runtime.mkdir();private=root/'private'
                record={'resources':[{'kind':'container','id':str(n),'nonce':'controlled'} for n in range(7)],'directories':[str(private)]}
                log=root/'case.log';good=output(selector,cases);log.write_text(good)
                with patch.object(self.sup,'root_record',return_value=record),patch.object(self.sup,'exact_absent',return_value=True) as absent,patch.object(self.sup.time,'sleep'):
                    self.assertTrue(self.sup.observe_root_chain(root,io.StringIO(),log,selector));self.assertEqual(absent.call_count,14)
                    log.write_text(good.replace('--- PASS: '+cases[-1]+' (0.01s)\n',''))
                    self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,selector));log.write_text(good)
                    private.mkdir();self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,selector));private.rmdir()
                    absent.return_value=False;self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,selector));absent.return_value=True
                    (runtime/'held').write_text('held');self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,selector))

    def check_original_exec(self):
        class OriginalExecBoundary(Exception):
            pass
        with tempfile.TemporaryDirectory(prefix='agent-schema-exec-') as tmp:
            root = Path(tmp)
            binary = root / 'candidate.test'; binary.write_text('controlled candidate'); binary.chmod(0o700)
            directory = root / 'owned'
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'GO', binary), patch.object(self.driver, 'sha', return_value=self.driver.MINIO_SHA):
                plan = self.driver.configuration(binary, SELECTOR, directory)
            self.assertEqual(plan['cwd'], str(root / 'tests/projectvariable'))
            self.assertEqual((plan['resources'], plan['test_timeout']), (7, '6m'))
            args = ['driver', '--test-binary', plan['binary'], '--run', SELECTOR, '--directory', str(directory)]
            def original_exec(path, argv, environment):
                self.assertEqual(path, '/bin/sh')
                self.assertEqual(argv, ['/bin/sh', 'scripts/test-objects.sh', '--run', SELECTOR])
                config = directory / 'go-config'
                self.assertEqual(environment['XDG_CONFIG_HOME'], str(config))
                self.assertEqual((config / 'go/telemetry/mode').read_text(), 'off\n')
                self.assertEqual(environment['AGENTEAM_FIXTURE_TEST_BINARY'], plan['binary'])
                self.assertEqual(environment['GOFLAGS'], '-mod=readonly -p=2')
                for name in ('TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD',
                             'AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD'):
                    self.assertNotIn(name, environment)
                raise OriginalExecBoundary()
            with patch.object(sys, 'argv', args), patch.object(self.driver, 'configuration', return_value=plan), \
                    patch.object(self.driver.os, 'chdir'), patch.object(self.driver.os, 'execve', side_effect=original_exec), \
                    patch.dict(os.environ, {name: 'must-be-removed' for name in (
                        'TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD',
                        'AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD')}):
                with self.assertRaises(OriginalExecBoundary):
                    self.driver.main()


    def test_busy_tcp_failure_uses_only_the_last_bounded_snapshot(self):
        rows = {('tcp', '%08X:AAAA' % index, '0100007F:BBBB', '06', str(index))
                for index in range(35)}
        before = rows.copy()
        with patch.object(self.sup, 'tcp', side_effect=AssertionError('must not poll')):
            for phase in ('supervisor', 'outer'):
                sample = self.sup.tcp_failure_sample(rows, phase)
                self.assertEqual((sample['phase'], sample['total'], sample['truncated']), (phase, 35, True))
                self.assertEqual(len(sample['rows']), 32)
                self.assertEqual([tuple(row[k] for k in ('family', 'localhex', 'remotehex', 'state', 'inode'))
                                  for row in sample['rows']], sorted(rows)[:32])
                self.assertRegex(sample['utc'], r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$')
            empty = self.sup.tcp_failure_sample(set(), 'outer')
            self.assertEqual((empty['total'], empty['truncated'], empty['rows']), (0, False, []))
            with self.assertRaises(ValueError):
                self.sup.tcp_failure_sample(rows, 'unknown')
        self.assertEqual(rows, before)


    def test_original_exec_for_exact_profiles(self):
        for selector in SYSTEM_INPUTS:
            with patch.dict(globals(), SELECTOR=selector):
                self.check_original_exec()


if __name__ == '__main__':
    unittest.main()
