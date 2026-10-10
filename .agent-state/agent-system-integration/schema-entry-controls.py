#!/usr/bin/env python3
"""Configuration-schema metadata-family entry controls; no Go/Docker/socket.

The inverse removes only this exact selector extension to main 7a693cb6.
Task records and these controls are intentionally not schema runtime inputs.
"""
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
SELECTOR = '^TestAgentConfigurationSchema$'
TOP = 'TestAgentConfigurationSchema'
CASES = frozenset({TOP, TOP + '/fresh-prefix-and-repeat',
                   TOP + '/upgrade-preserves-facts-and-audit-checks',
                   TOP + '/schema-invariants-and-unbound-dependencies'})
BASE_SHA = {'.agent-state/work-owner-http/root_chain_driver.py': '6d35af038584d9db4757d96387e67d51ab26d8210346f805cea674830b6e887f',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'dfa4284e92f22c3b4349885b6ece0028404916087c5ee4797dd00efb15a89559'}
SOURCE_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [['TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestAgentConfigurationSchema$': "
                                                        "'tests/projectvariable',\n"],
                                                       ['def metadata_inputs(binary):\n',
                                                        'def metadata_inputs(binary, '
                                                        "selector='^TestAgentConfigurationMetadata$'):\n"
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n"
                                                        "        raise ValueError('exact configuration "
                                                        "family selector required')\n"],
                                                       ['    paths.update(REPOSITORY / name for name in (\n'
                                                        '        '
                                                        "'tests/projectvariable/agent_configuration_metadata_test.go',\n"
                                                        '        '
                                                        "'.agent-state/agent-configuration-metadata/README.md',\n"
                                                        '        '
                                                        "'.agent-state/agent-configuration-metadata/metadata-entry-controls.py'))\n",
                                                        '    if selector == '
                                                        "'^TestAgentConfigurationSchema$':\n"
                                                        '        # Runtime/compiled source only. Task '
                                                        'records and pure controls do not\n'
                                                        "        # alter this run's inputs or force another "
                                                        'candidate build.\n'
                                                        '        paths.update(REPOSITORY / name for name in '
                                                        '(\n'
                                                        '            '
                                                        "'tests/projectvariable/agent_configuration_schema_test.go',\n"
                                                        '            '
                                                        "'tests/testsupport/agentconfiguration/assembly.go'))\n"
                                                        '    else:\n'
                                                        '        paths.update(REPOSITORY / name for name in '
                                                        '(\n'
                                                        '            '
                                                        "'tests/projectvariable/agent_configuration_metadata_test.go',\n"
                                                        '            '
                                                        "'.agent-state/agent-configuration-metadata/README.md',\n"
                                                        '            '
                                                        "'.agent-state/agent-configuration-metadata/metadata-entry-controls.py'))\n"],
                                                       ['    if args.run == '
                                                        "'^TestAgentConfigurationMetadata$':\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n"]],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [['\n\ndef metadata_results(output):\n',
                                                                '\n'
                                                                '\n'
                                                                'SCHEMA_ROOT = '
                                                                "'^TestAgentConfigurationSchema$'\n"
                                                                'SCHEMA_CASES = frozenset({\n'
                                                                "    'TestAgentConfigurationSchema',\n"
                                                                '    '
                                                                "'TestAgentConfigurationSchema/fresh-prefix-and-repeat',\n"
                                                                '    '
                                                                "'TestAgentConfigurationSchema/upgrade-preserves-facts-and-audit-checks',\n"
                                                                '    '
                                                                "'TestAgentConfigurationSchema/schema-invariants-and-unbound-dependencies',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: SCHEMA_CASES}\n'
                                                                '\n'
                                                                '\n'
                                                                'def metadata_results(output, '
                                                                'selector=METADATA_ROOT):\n'
                                                                '    cases = METADATA_GROUPS.get(selector)\n'
                                                                '    if cases is None:\n'
                                                                '        return False\n'],
                                                               ['    return (len(runs) == '
                                                                'len(METADATA_CASES) and set(runs) == '
                                                                'METADATA_CASES\n'
                                                                '            and len(results) == '
                                                                'len(METADATA_CASES)\n'
                                                                "            and all(state == 'PASS' for "
                                                                'state, _ in results)\n'
                                                                '            and {name for _, name in '
                                                                'results} == METADATA_CASES\n'
                                                                '            and len(waits) == 1 and '
                                                                "waits[0][1:] == ('0', METADATA_ROOT)\n",
                                                                '    return (len(runs) == len(cases) and '
                                                                'set(runs) == cases\n'
                                                                '            and len(results) == len(cases)\n'
                                                                "            and all(state == 'PASS' for "
                                                                'state, _ in results)\n'
                                                                '            and {name for _, name in '
                                                                'results} == cases\n'
                                                                '            and len(waits) == 1 and '
                                                                "waits[0][1:] == ('0', selector)\n"],
                                                               ['def metadata_same(inputs, args, adapter):\n'
                                                                '    try:\n'
                                                                '        return {str(p): adapter.sha(p) for '
                                                                'p in adapter.metadata_inputs(args.binary)} '
                                                                '== inputs\n',
                                                                'def metadata_same(inputs, args, adapter, '
                                                                'selector=METADATA_ROOT):\n'
                                                                '    try:\n'
                                                                '        return {str(p): adapter.sha(p) for '
                                                                'p in adapter.metadata_inputs(args.binary, '
                                                                'selector)} == inputs\n'],
                                                               ['    if selector in (METADATA_ROOT, '
                                                                'GUARD_ROOT,',
                                                                '    if selector in (*METADATA_GROUPS, '
                                                                'GUARD_ROOT,'],
                                                               ['        METADATA_ROOT: '
                                                                "{'TestAgentConfigurationMetadata'},\n",
                                                                '        METADATA_ROOT: '
                                                                "{'TestAgentConfigurationMetadata'},\n"
                                                                '        SCHEMA_ROOT: '
                                                                "{'TestAgentConfigurationSchema'},\n"],
                                                               ['    if selector == METADATA_ROOT:\n'
                                                                '        complete = '
                                                                'metadata_results(output)\n',
                                                                '    if selector in METADATA_GROUPS:\n'
                                                                '        complete = metadata_results(output, '
                                                                'selector)\n'],
                                                               ["    if 'AgentConfigurationMetadata' in "
                                                                'args.run and (args.run != METADATA_ROOT or '
                                                                'not args.root_chain):\n',
                                                                '    if any(name in args.run for name in '
                                                                "('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema')) and (args.run "
                                                                'not in METADATA_GROUPS or not '
                                                                'args.root_chain):\n'],
                                                               ['    if args.run == METADATA_ROOT:\n'
                                                                '        inputs = {str(p): adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.metadata_inputs(args.binary)}\n',
                                                                '    if args.run in METADATA_GROUPS:\n'
                                                                '        inputs = {str(p): adapter.sha(p) '
                                                                'for p in '
                                                                'adapter.metadata_inputs(args.binary, '
                                                                'args.run)}\n'],
                                                               ['            if args.run == METADATA_ROOT:\n'
                                                                '                same = same and '
                                                                'metadata_same(inputs, args, adapter)\n',
                                                                '            if args.run in '
                                                                'METADATA_GROUPS:\n'
                                                                '                same = same and '
                                                                'metadata_same(inputs, args, adapter, '
                                                                'args.run)\n']]}


RUNTIME_SELECTOR = "^TestAgentRuntimeSchema$"
RUNTIME_TOP = "TestAgentRuntimeSchema"
RUNTIME_CASES = frozenset({RUNTIME_TOP,
    RUNTIME_TOP + "/prefix36-upgrade-and-repeat",
    RUNTIME_TOP + "/runtime-attempt-and-terminal",
    RUNTIME_TOP + "/execution-slot-and-unbound-launch",
    RUNTIME_TOP + "/human-compatibility-and-agent-origin"})
RUNTIME_BASE = {'.agent-state/work-owner-http/root_chain_driver.py': 'a2dafc36700f4caa8a2b0f7d068afc771354068903d3c1d9feea86dd2d33603d',
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'e66109118867c3c477aaae481b5f34f9ba52a39de34b3800048927f3e879daee'}
RUNTIME_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [('TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestAgentRuntimeSchema$': "
                                                        "'tests/projectvariable',\n"),
                                                       ('    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n",
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$'):\n"),
                                                       ('    if selector == '
                                                        "'^TestAgentConfigurationSchema$':\n",
                                                        '    if selector == '
                                                        "'^TestAgentRuntimeSchema$':\n"
                                                        '        paths.add(REPOSITORY / '
                                                        "'tests/projectvariable/agent_runtime_schema_test.go')\n"
                                                        '    elif selector == '
                                                        "'^TestAgentConfigurationSchema$':\n"),
                                                       ('    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$'):\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$'):\n")],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES}\n',
                                                                'RUNTIME_SCHEMA_ROOT = '
                                                                "'^TestAgentRuntimeSchema$'\n"
                                                                'RUNTIME_SCHEMA_CASES = frozenset({\n'
                                                                "    'TestAgentRuntimeSchema',\n"
                                                                '    '
                                                                "'TestAgentRuntimeSchema/prefix36-upgrade-and-repeat',\n"
                                                                '    '
                                                                "'TestAgentRuntimeSchema/runtime-attempt-and-terminal',\n"
                                                                '    '
                                                                "'TestAgentRuntimeSchema/execution-slot-and-unbound-launch',\n"
                                                                '    '
                                                                "'TestAgentRuntimeSchema/human-compatibility-and-agent-origin',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES,\n'
                                                                '                   '
                                                                'RUNTIME_SCHEMA_ROOT: '
                                                                'RUNTIME_SCHEMA_CASES}\n'),
                                                               ('        SCHEMA_ROOT: '
                                                                "{'TestAgentConfigurationSchema'},\n",
                                                                '        SCHEMA_ROOT: '
                                                                "{'TestAgentConfigurationSchema'},\n"
                                                                '        RUNTIME_SCHEMA_ROOT: '
                                                                "{'TestAgentRuntimeSchema'},\n"),
                                                               ("('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema')) and",
                                                                "('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema', "
                                                                "'AgentRuntimeSchema')) and")]}

CLAIM_SELECTOR = '^TestSchedulerClaim$'
CLAIM_TOP = 'TestSchedulerClaim'
CLAIM_CASES = frozenset({CLAIM_TOP, CLAIM_TOP + '/start-sprint-claim-and-replay',
                         CLAIM_TOP + '/final-transaction-rollback'})
CLAIM_BASE = {'.agent-state/work-owner-http/root_chain_driver.py': '2fc6f02c219cb3649c597e4c0c4aea40c297847d0b255e197e951d17fc4629e9', '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'a4380f79c76550ca8e362d816822cd67623d851c359b3a226210041d0769a5ea'}
CLAIM_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [('TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestSchedulerClaim$': "
                                                        "'tests/projectvariable',\n"),
                                                       ('    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$'):\n",
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$', "
                                                        "'^TestSchedulerClaim$'):\n"),
                                                       ('    if selector == '
                                                        "'^TestTaskTransitionHuman$':\n",
                                                        '    if selector == '
                                                        "'^TestSchedulerClaim$':\n"
                                                        '        paths.add(REPOSITORY / '
                                                        "'tests/projectvariable/scheduler_claim_test.go')\n"
                                                        '    elif selector == '
                                                        "'^TestTaskTransitionHuman$':\n"),
                                                       ('    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$'):\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$', "
                                                        "'^TestSchedulerClaim$'):\n")],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES,\n',
                                                                'SCHEDULER_CLAIM_ROOT = '
                                                                "'^TestSchedulerClaim$'\n"
                                                                'SCHEDULER_CLAIM_CASES = '
                                                                'frozenset({\n'
                                                                "    'TestSchedulerClaim',\n"
                                                                '    '
                                                                "'TestSchedulerClaim/start-sprint-claim-and-replay',\n"
                                                                '    '
                                                                "'TestSchedulerClaim/final-transaction-rollback',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES,\n'),
                                                               ('                   '
                                                                'TASK_HUMAN_ROOT: '
                                                                'TASK_HUMAN_CASES}\n',
                                                                '                   '
                                                                'TASK_HUMAN_ROOT: '
                                                                'TASK_HUMAN_CASES,\n'
                                                                '                   '
                                                                'SCHEDULER_CLAIM_ROOT: '
                                                                'SCHEDULER_CLAIM_CASES}\n'),
                                                               ('        TASK_HUMAN_ROOT: '
                                                                "{'TestTaskTransitionHuman'},\n",
                                                                '        TASK_HUMAN_ROOT: '
                                                                "{'TestTaskTransitionHuman'},\n"
                                                                '        SCHEDULER_CLAIM_ROOT: '
                                                                "{'TestSchedulerClaim'},\n"),
                                                               ("'AgentConfigurationCreate', "
                                                                "'TaskTransitionHuman')) and",
                                                                "'AgentConfigurationCreate', "
                                                                "'TaskTransitionHuman', "
                                                                "'SchedulerClaim')) and")]}

def claim_projection(name, source):
    if "'^TestSchedulerClaim$'" not in source:
        return source
    for before, after in reversed(CLAIM_HUNKS[name]):
        if source.count(after) != 1:
            raise ValueError('unknown or ambiguous Scheduler claim data')
        source = source.replace(after, before, 1)
    if hashlib.sha256(source.encode()).hexdigest() != CLAIM_BASE[name]:
        raise ValueError('unknown Scheduler claim baseline')
    return source


TASK_SELECTOR = '^TestTaskTransitionHuman$'
TASK_TOP = 'TestTaskTransitionHuman'
TASK_CASES = frozenset({TASK_TOP, TASK_TOP + '/assignment-config-and-replay',
                        TASK_TOP + '/final-transaction-rollback'})
TASK_BASE = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': '04cf0e70fdb148916253f735fc2b69d13492f0bc5aa010ef39f044a4e5e90a7b',
 '.agent-state/work-owner-http/root_chain_driver.py': 'afb3eaa1a52523d14e74063eb0d170a77d473ac2c4e8471c48fbfca934425a8e'}
TASK_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [('TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestTaskTransitionHuman$': "
                                                        "'tests/projectvariable',\n"),
                                                       ('    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$'):\n",
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$'):\n"),
                                                       ('    if selector == '
                                                        "'^TestAgentConfigurationCreate$':\n",
                                                        '    if selector == '
                                                        "'^TestTaskTransitionHuman$':\n"
                                                        '        paths.add(REPOSITORY / '
                                                        "'tests/projectvariable/task_transition_scheduler_test.go')\n"
                                                        '    elif selector == '
                                                        "'^TestAgentConfigurationCreate$':\n"),
                                                       ('    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$'):\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$', "
                                                        "'^TestTaskTransitionHuman$'):\n")],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {METADATA_ROOT:',
                                                                'TASK_HUMAN_ROOT = '
                                                                "'^TestTaskTransitionHuman$'\n"
                                                                'TASK_HUMAN_CASES = frozenset({\n'
                                                                "    'TestTaskTransitionHuman',\n"
                                                                '    '
                                                                "'TestTaskTransitionHuman/assignment-config-and-replay',\n"
                                                                '    '
                                                                "'TestTaskTransitionHuman/final-transaction-rollback',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = '
                                                                '{METADATA_ROOT:'),
                                                               ('                   '
                                                                'AGENT_CREATE_ROOT: '
                                                                'AGENT_CREATE_CASES}\n',
                                                                '                   '
                                                                'AGENT_CREATE_ROOT: '
                                                                'AGENT_CREATE_CASES,\n'
                                                                '                   '
                                                                'TASK_HUMAN_ROOT: '
                                                                'TASK_HUMAN_CASES}\n'),
                                                               ('        AGENT_CREATE_ROOT: '
                                                                "{'TestAgentConfigurationCreate'},\n",
                                                                '        AGENT_CREATE_ROOT: '
                                                                "{'TestAgentConfigurationCreate'},\n"
                                                                '        TASK_HUMAN_ROOT: '
                                                                "{'TestTaskTransitionHuman'},\n"),
                                                               ("'ExecutionPreparation', "
                                                                "'AgentConfigurationCreate')) and",
                                                                "'ExecutionPreparation', "
                                                                "'AgentConfigurationCreate', "
                                                                "'TaskTransitionHuman')) and")]}

def task_projection(name, source):
    source = claim_projection(name, source)
    if "'^TestTaskTransitionHuman$'" not in source:
        return source
    for before, after in reversed(TASK_HUNKS[name]):
        if source.count(after) != 1:
            raise ValueError('unknown or ambiguous Task Human data')
        source = source.replace(after, before, 1)
    if hashlib.sha256(source.encode()).hexdigest() != TASK_BASE[name]:
        raise ValueError('unknown Task Human baseline')
    return source


CREATE_SELECTOR = '^TestAgentConfigurationCreate$'
CREATE_TOP = 'TestAgentConfigurationCreate'
CREATE_CASES = frozenset({CREATE_TOP, CREATE_TOP + '/default-create-and-replay',
                          CREATE_TOP + '/final-transaction-rollback'})
CREATE_BASE = {'.agent-state/work-owner-http/root_chain_driver.py': '3a8b513901b4fedc602edeff4f094f41da19c9d87f5bcdba2f5f188aa314dd0d', '.agent-state/task-planning-recovery/pg_only_supervisor.py': 'c7dff4f78c0cc3a574564c93f7f167b24a4bc454c8c693c230b607d7eb5a45c0'}
CREATE_HUNKS = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: SCHEMA_CASES,\n'
                                                                '                   RUNTIME_SCHEMA_ROOT: '
                                                                'RUNTIME_SCHEMA_CASES,\n'
                                                                '                   PREPARATION_ROOT: '
                                                                'PREPARATION_CASES}\n',
                                                                'AGENT_CREATE_ROOT = '
                                                                "'^TestAgentConfigurationCreate$'\n"
                                                                'AGENT_CREATE_CASES = frozenset({\n'
                                                                "    'TestAgentConfigurationCreate',\n"
                                                                '    '
                                                                "'TestAgentConfigurationCreate/default-create-and-replay',\n"
                                                                '    '
                                                                "'TestAgentConfigurationCreate/final-transaction-rollback',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: SCHEMA_CASES,\n'
                                                                '                   RUNTIME_SCHEMA_ROOT: '
                                                                'RUNTIME_SCHEMA_CASES,\n'
                                                                '                   PREPARATION_ROOT: '
                                                                'PREPARATION_CASES,\n'
                                                                '                   AGENT_CREATE_ROOT: '
                                                                'AGENT_CREATE_CASES}\n'),
                                                               ('        PREPARATION_ROOT: '
                                                                "{'TestExecutionPreparation'},\n",
                                                                '        PREPARATION_ROOT: '
                                                                "{'TestExecutionPreparation'},\n"
                                                                '        AGENT_CREATE_ROOT: '
                                                                "{'TestAgentConfigurationCreate'},\n"),
                                                               ("'AgentRuntimeSchema', "
                                                                "'ExecutionPreparation')) and",
                                                                "'AgentRuntimeSchema', "
                                                                "'ExecutionPreparation', "
                                                                "'AgentConfigurationCreate')) and")],
 '.agent-state/work-owner-http/root_chain_driver.py': [('TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestAgentConfigurationCreate$': "
                                                        "'tests/projectvariable',\n"),
                                                       ('    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$'):\n",
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$'):\n"),
                                                       ('    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$'):\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$', "
                                                        "'^TestAgentConfigurationCreate$'):\n"),
                                                       ("    if selector == '^TestExecutionPreparation$':\n",
                                                        '    if selector == '
                                                        "'^TestAgentConfigurationCreate$':\n"
                                                        '        paths.update(REPOSITORY / name for name in '
                                                        '(\n'
                                                        '            '
                                                        "'tests/projectvariable/agent_configuration_create_test.go',\n"
                                                        '            '
                                                        "'tests/projectvariable/agent_configuration_facts_test.go',\n"
                                                        '            '
                                                        "'tests/projectvariable/skill_installation_test.go',\n"
                                                        '            '
                                                        "'tests/testsupport/agentconfiguration/assembly.go'))\n"
                                                        '    elif selector == '
                                                        "'^TestExecutionPreparation$':\n")]}

def create_projection(name, source):
    source = task_projection(name, source)
    if "'^TestAgentConfigurationCreate$'" not in source:
        return source
    for old, new in reversed(CREATE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous Agent Create data')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != CREATE_BASE[name]:
        raise ValueError('unknown Agent Create baseline')
    return source


PREPARATION_SELECTOR = "^TestExecutionPreparation$"
PREPARATION_TOP = "TestExecutionPreparation"
PREPARATION_CASES = frozenset({PREPARATION_TOP,
    PREPARATION_TOP + "/prefix39-upgrade-and-repeat",
    PREPARATION_TOP + "/preparation-claim-and-attempt",
    PREPARATION_TOP + "/project-preparation-gate",
    PREPARATION_TOP + "/current-owner-task-input"})
PREPARATION_BASE = {'.agent-state/work-owner-http/root_chain_driver.py': '409a122d19a840f42d268ea147fe5a1899afb20770e2348526da23d6b5ffebde', '.agent-state/task-planning-recovery/pg_only_supervisor.py': '5cc7d2dee481da371ad92d2b22f756a9f70f36eebffabd136d3221c6b048df88'}
PREPARATION_HUNKS = {'.agent-state/work-owner-http/root_chain_driver.py': [('TARGETS = {\n',
                                                        'TARGETS = {\n'
                                                        "    '^TestExecutionPreparation$': "
                                                        "'tests/projectvariable',\n"),
                                                       ('    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$'):\n",
                                                        '    if selector not in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$'):\n"),
                                                       ('    if selector == '
                                                        "'^TestAgentRuntimeSchema$':\n",
                                                        '    if selector == '
                                                        "'^TestExecutionPreparation$':\n"
                                                        '        paths.add(REPOSITORY / '
                                                        "'tests/projectvariable/execution_preparation_test.go')\n"
                                                        '    elif selector == '
                                                        "'^TestAgentRuntimeSchema$':\n"),
                                                       ('    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$'):\n",
                                                        '    if args.run in '
                                                        "('^TestAgentConfigurationMetadata$', "
                                                        "'^TestAgentConfigurationSchema$', "
                                                        "'^TestAgentRuntimeSchema$', "
                                                        "'^TestExecutionPreparation$'):\n")],
 '.agent-state/task-planning-recovery/pg_only_supervisor.py': [('METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES,\n'
                                                                '                   '
                                                                'RUNTIME_SCHEMA_ROOT: '
                                                                'RUNTIME_SCHEMA_CASES}\n',
                                                                'PREPARATION_ROOT = '
                                                                "'^TestExecutionPreparation$'\n"
                                                                'PREPARATION_CASES = frozenset({\n'
                                                                "    'TestExecutionPreparation',\n"
                                                                '    '
                                                                "'TestExecutionPreparation/prefix39-upgrade-and-repeat',\n"
                                                                '    '
                                                                "'TestExecutionPreparation/preparation-claim-and-attempt',\n"
                                                                '    '
                                                                "'TestExecutionPreparation/project-preparation-gate',\n"
                                                                '    '
                                                                "'TestExecutionPreparation/current-owner-task-input',\n"
                                                                '})\n'
                                                                'METADATA_GROUPS = {METADATA_ROOT: '
                                                                'METADATA_CASES, SCHEMA_ROOT: '
                                                                'SCHEMA_CASES,\n'
                                                                '                   '
                                                                'RUNTIME_SCHEMA_ROOT: '
                                                                'RUNTIME_SCHEMA_CASES,\n'
                                                                '                   '
                                                                'PREPARATION_ROOT: '
                                                                'PREPARATION_CASES}\n'),
                                                               ('        RUNTIME_SCHEMA_ROOT: '
                                                                "{'TestAgentRuntimeSchema'},\n",
                                                                '        RUNTIME_SCHEMA_ROOT: '
                                                                "{'TestAgentRuntimeSchema'},\n"
                                                                '        PREPARATION_ROOT: '
                                                                "{'TestExecutionPreparation'},\n"),
                                                               ("('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema', "
                                                                "'AgentRuntimeSchema')) and",
                                                                "('AgentConfigurationMetadata', "
                                                                "'AgentConfigurationSchema', "
                                                                "'AgentRuntimeSchema', "
                                                                "'ExecutionPreparation')) and")]}

def preparation_projection(name, source):
    source = create_projection(name, source)
    if "'^TestExecutionPreparation$'" not in source:
        return source
    for old, new in reversed(PREPARATION_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous preparation data')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != PREPARATION_BASE[name]:
        raise ValueError('unknown preparation baseline')
    return source


def runtime_projection(name, source):
    source = preparation_projection(name, source)
    if "'^TestAgentRuntimeSchema$'" not in source:
        return source
    for old, new in reversed(RUNTIME_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous runtime schema data')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != RUNTIME_BASE[name]:
        raise ValueError('unknown runtime schema baseline')
    return source


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    source = runtime_projection(name, source)
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous schema hunk')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown shared baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def original_log():
    return (''.join('=== RUN   ' + name + '\n' for name in sorted(CASES))
            + ''.join('--- PASS: ' + name + ' (0.01s)\n' for name in sorted(CASES))
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + SELECTOR + '\n')


class SchemaEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver = load('schema_entry_driver', DRIVER)
        cls.sup = load('schema_entry_supervisor', SUP)

    def test_exact_delta_and_existing_family(self):
        for name in BASE_SHA:
            source = runtime_projection(name, (ROOT / name).read_text())
            ast.parse(source)
            restored = inverse(name, source)
            ast.parse(restored)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            with self.assertRaises(ValueError):
                inverse(name, source + '\n# unknown change\n')
            for old, new in SOURCE_HUNKS[name]:
                self.assertEqual(source.count(new), 1)
                for bad in (source.replace(new, old, 1), source + new):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        baseline = {'__file__': str(ROOT / DRIVER), '__name__': 'schema_baseline'}
        exec(compile(inverse(DRIVER, (ROOT / DRIVER).read_text()), DRIVER, 'exec'), baseline)
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'tests/projectvariable')
        self.assertEqual({k: v for k, v in self.driver.TARGETS.items() if k not in (SELECTOR, RUNTIME_SELECTOR, PREPARATION_SELECTOR, CREATE_SELECTOR, TASK_SELECTOR, CLAIM_SELECTOR)}, baseline['TARGETS'])
        self.assertEqual(self.sup.budgets(True), (540, 60))
        self.assertEqual(self.sup.budgets(False), (123, 3))
        for path, constant in (
                ('.agent-state/agent-configuration-metadata/metadata-entry-controls.py', 'BASE_SHA'),
                ('.agent-state/project-variable-lifecycle/guard-entry-controls.py', 'BASE_SHA'),
                ('.agent-state/model-text-runtime/entry-controls.py', 'BASE_SHA')):
            previous = load('schema_previous_' + path.split('/')[1].replace('-', '_'), path)
            for name in BASE_SHA:
                restored = previous.inverse(name, (ROOT / name).read_text())
                self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), getattr(previous, constant)[name])

    def test_exact_three_subcases_and_original_wait(self):
        good = original_log()
        self.assertEqual(self.sup.SCHEMA_ROOT, SELECTOR)
        self.assertEqual(self.sup.SCHEMA_CASES, CASES)
        self.assertTrue(self.sup.metadata_results(good, SELECTOR))
        self.assertFalse(self.sup.metadata_results(good))
        self.assertFalse(self.sup.metadata_results(good, SELECTOR + 'x'))
        for name in CASES:
            run, passed = '=== RUN   ' + name + '\n', '--- PASS: ' + name + ' (0.01s)\n'
            for bad in (good.replace(run, ''), good.replace(passed, ''), good + run, good + passed,
                        good.replace(passed, passed.replace('PASS', 'SKIP')),
                        good.replace(passed, passed.replace('PASS', 'FAIL'))):
                self.assertFalse(self.sup.metadata_results(bad, SELECTOR))
        wait = good.splitlines(True)[-1]
        for bad in (good + '=== RUN   TestOther\n', good + wait, good.replace(wait, ''),
                    good.replace('code=0', 'code=1'), good.replace('pid=42', 'pid=0'),
                    good.replace('selector=' + SELECTOR, 'selector=' + self.sup.METADATA_ROOT),
                    good + 'D03 explicit test actual_wait malformed\n', good + 'FAIL\n'):
            self.assertFalse(self.sup.metadata_results(bad, SELECTOR))

    def test_actual_main_requires_exact_root_mode(self):
        base = ['supervisor', '--driver', '/not-used/driver', '--binary', '/not-used/candidate',
                '--output', '/not-used/output', '--run']
        for selector, root in ((SELECTOR, False), (SELECTOR + 'x', True),
                               (TOP, True), ('^' + TOP + '(Extra)?$', True),
                               (SELECTOR + '|' + self.sup.METADATA_ROOT, True)):
            args = base + [selector] + (['--root-chain'] if root else [])
            with patch.object(sys, 'argv', args), patch.object(self.sup, 'budgets') as budgets, \
                    patch.object(self.sup, 'root_adapter') as adapter, \
                    patch.object(self.sup.subprocess, 'Popen') as spawned, \
                    patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit):
                    self.sup.main()
                budgets.assert_not_called()
                adapter.assert_not_called()
                spawned.assert_not_called()
        class ReachedOriginalBudget(Exception):
            pass
        for selector in (SELECTOR, self.sup.METADATA_ROOT, self.sup.GUARD_ROOT, self.sup.MODEL_RUNTIME):
            with patch.object(sys, 'argv', base + [selector, '--root-chain']), \
                    patch.object(self.sup, 'budgets', side_effect=ReachedOriginalBudget) as budgets:
                with self.assertRaises(ReachedOriginalBudget):
                    self.sup.main()
                budgets.assert_called_once_with(True)

    def test_actual_schema_inputs_reenumerate_runtime_sources(self):
        with tempfile.TemporaryDirectory(prefix='agent-schema-inputs-') as tmp:
            root = Path(tmp).resolve()
            names = ('candidate.test', 'production.go',
                     ('tests/projectvariable/scheduler_claim_test.go' if SELECTOR == CLAIM_SELECTOR
                      else 'tests/projectvariable/task_transition_scheduler_test.go' if SELECTOR == TASK_SELECTOR
                      else 'tests/projectvariable/agent_configuration_create_test.go' if SELECTOR == CREATE_SELECTOR
                      else 'tests/projectvariable/execution_preparation_test.go' if SELECTOR == PREPARATION_SELECTOR
                      else 'tests/projectvariable/agent_runtime_schema_test.go' if SELECTOR == RUNTIME_SELECTOR
                      else 'tests/projectvariable/agent_configuration_schema_test.go'),
                     'tests/projectvariable/original_helper_test.go',
                     'tests/projectvariable/agent_configuration_facts_test.go',
                     'tests/projectvariable/skill_installation_test.go',
                     'internal/other/other_test.go', 'internal/other/assets/NOTICE',
                     'tests/testsupport/agentconfiguration/assembly.go',
                     'tests/testsupport/postgres/original.go',
                     '.agent-state/project-variables-independent/commitproxy/proxy.go')
            for name in names:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('controlled source\n')
            records = ('.agent-state/current.md', '.agent-state/agent-system-integration/README.md',
                       '.agent-state/agent-system-integration/schema-entry-controls.py',
                       '.agent-state/agent-configuration-metadata/README.md')
            for name in records:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('non-runtime record\n')
            binary = root / 'candidate.test'
            with patch.object(self.driver, 'REPOSITORY', root), \
                    patch.object(self.driver, 'input_paths', return_value=[binary, root / 'production.go']):
                paths = self.driver.metadata_inputs(binary, SELECTOR)
                self.assertEqual(paths, sorted(root / name for name in names))
                inputs = {str(p): self.driver.sha(p) for p in paths}
                args = SimpleNamespace(binary=binary)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                for name in records:
                    (root / name).write_text('changed non-runtime record\n')
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                for relative in ('tests/projectvariable/later_helper_test.go',
                                 'tests/testsupport/agentconfiguration/later.go',
                                 'internal/other/later_test.go', 'internal/other/assets/later.txt'):
                    added = root / relative
                    added.write_text('later source')
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    added.unlink()
                for name in names[2:]:
                    path = root / name
                    original = path.read_text()
                    path.write_text('changed source')
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    path.unlink()
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    path.symlink_to(binary)
                    self.assertFalse(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                    path.unlink()
                    path.write_text(original)
                self.assertTrue(self.sup.metadata_same(inputs, args, self.driver, SELECTOR))
                with self.assertRaises(ValueError):
                    self.driver.metadata_inputs(binary, SELECTOR + 'x')

    def test_actual_observer_keeps_original_resource_tails(self):
        with tempfile.TemporaryDirectory(prefix='agent-schema-observer-') as tmp:
            root = Path(tmp)
            runtime = root / 'runtime'; runtime.mkdir()
            private = root / 'private'
            record = {'resources': [{'kind': 'container', 'id': str(n), 'nonce': 'controlled'} for n in range(7)],
                      'directories': [str(private)]}
            log = root / 'case.log'; log.write_text(original_log())
            with patch.object(self.sup, 'root_record', return_value=record), \
                    patch.object(self.sup, 'exact_absent', return_value=True) as absent, \
                    patch.object(self.sup.time, 'sleep'):
                self.assertTrue(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                self.assertEqual(absent.call_count, 14)
                for value in (original_log().replace('--- PASS: ' + sorted(CASES - {TOP})[0] + ' (0.01s)\n', ''),
                              original_log().replace('code=0', 'code=1')):
                    log.write_text(value)
                    self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                log.write_text(original_log())
                private.mkdir()
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                private.rmdir()
                absent.return_value = False
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))
                absent.return_value = True
                (runtime / 'pending').write_text('controlled')
                self.assertFalse(self.sup.observe_root_chain(root, io.StringIO(), log, SELECTOR))

    def test_runtime_schema_reuses_the_original_family(self):
        self.assertEqual(self.driver.TARGETS[RUNTIME_SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[RUNTIME_SELECTOR], RUNTIME_CASES)
        for name, pairs in RUNTIME_HUNKS.items():
            source = preparation_projection(name, (ROOT / name).read_text())
            self.assertEqual(hashlib.sha256(runtime_projection(name, source).encode()).hexdigest(), RUNTIME_BASE[name])
            for old, new in pairs:
                for bad in (source.replace(new, old, 1), source + new):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        with patch.dict(globals(), SELECTOR=RUNTIME_SELECTOR, TOP=RUNTIME_TOP, CASES=RUNTIME_CASES), \
                patch.object(self.sup, 'SCHEMA_ROOT', RUNTIME_SELECTOR), \
                patch.object(self.sup, 'SCHEMA_CASES', RUNTIME_CASES):
            self.test_exact_three_subcases_and_original_wait()
            self.test_actual_main_requires_exact_root_mode()
            self.test_actual_schema_inputs_reenumerate_runtime_sources()
            self.test_actual_observer_keeps_original_resource_tails()
            self.test_actual_driver_fixed_environment_before_original_exec()

    def test_preparation_reuses_the_original_family(self):
        self.assertEqual(self.driver.TARGETS[PREPARATION_SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[PREPARATION_SELECTOR], PREPARATION_CASES)
        for name, pairs in PREPARATION_HUNKS.items():
            source = create_projection(name, (ROOT / name).read_text())
            self.assertEqual(hashlib.sha256(preparation_projection(name, source).encode()).hexdigest(), PREPARATION_BASE[name])
            for old, new in pairs:
                self.assertEqual(source.count(new), 1)
                for bad in (source.replace(new, old, 1), source + new, source + '\n# unknown\n'):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        with patch.dict(globals(), SELECTOR=PREPARATION_SELECTOR, TOP=PREPARATION_TOP, CASES=PREPARATION_CASES), \
                patch.object(self.sup, 'SCHEMA_ROOT', PREPARATION_SELECTOR), \
                patch.object(self.sup, 'SCHEMA_CASES', PREPARATION_CASES):
            self.test_exact_three_subcases_and_original_wait()
            self.test_actual_main_requires_exact_root_mode()
            self.test_actual_schema_inputs_reenumerate_runtime_sources()
            self.test_actual_observer_keeps_original_resource_tails()
            self.test_actual_driver_fixed_environment_before_original_exec()

    def test_agent_create_reuses_the_original_family(self):
        self.assertEqual(self.driver.TARGETS[CREATE_SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[CREATE_SELECTOR], CREATE_CASES)
        for name, pairs in CREATE_HUNKS.items():
            source = task_projection(name, (ROOT / name).read_text())
            self.assertEqual(hashlib.sha256(create_projection(name, source).encode()).hexdigest(), CREATE_BASE[name])
            for old, new in pairs:
                self.assertEqual(source.count(new), 1)
                for bad in (source.replace(new, old, 1), source + new, source + '\n# unknown\n'):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        with patch.dict(globals(), SELECTOR=CREATE_SELECTOR, TOP=CREATE_TOP, CASES=CREATE_CASES), \
                patch.object(self.sup, 'SCHEMA_ROOT', CREATE_SELECTOR), \
                patch.object(self.sup, 'SCHEMA_CASES', CREATE_CASES):
            self.test_exact_three_subcases_and_original_wait()
            self.test_actual_main_requires_exact_root_mode()
            self.test_actual_schema_inputs_reenumerate_runtime_sources()
            self.test_actual_observer_keeps_original_resource_tails()
            self.test_actual_driver_fixed_environment_before_original_exec()

    def test_task_human_reuses_the_original_family(self):
        self.assertEqual(self.driver.TARGETS[TASK_SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[TASK_SELECTOR], TASK_CASES)
        for name, pairs in TASK_HUNKS.items():
            source = claim_projection(name, (ROOT / name).read_text())
            self.assertEqual(hashlib.sha256(task_projection(name, source).encode()).hexdigest(), TASK_BASE[name])
            for old, new in pairs:
                self.assertEqual(source.count(new), 1)
                for bad in (source.replace(new, old, 1), source + new, source + '\n# unknown\n'):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        with patch.dict(globals(), SELECTOR=TASK_SELECTOR, TOP=TASK_TOP, CASES=TASK_CASES), \
                patch.object(self.sup, 'SCHEMA_ROOT', TASK_SELECTOR), \
                patch.object(self.sup, 'SCHEMA_CASES', TASK_CASES):
            self.test_exact_three_subcases_and_original_wait()
            self.test_actual_main_requires_exact_root_mode()
            self.test_actual_schema_inputs_reenumerate_runtime_sources()
            self.test_actual_observer_keeps_original_resource_tails()
            self.test_actual_driver_fixed_environment_before_original_exec()

    def test_scheduler_claim_reuses_the_original_family(self):
        self.assertEqual(self.driver.TARGETS[CLAIM_SELECTOR], 'tests/projectvariable')
        self.assertEqual(self.sup.METADATA_GROUPS[CLAIM_SELECTOR], CLAIM_CASES)
        for name, pairs in CLAIM_HUNKS.items():
            source = (ROOT / name).read_text()
            self.assertEqual(hashlib.sha256(claim_projection(name, source).encode()).hexdigest(), CLAIM_BASE[name])
            for old, new in pairs:
                self.assertEqual(source.count(new), 1)
                for bad in (source.replace(new, old, 1), source + new, source + '\n# unknown\n'):
                    with self.assertRaises(ValueError):
                        inverse(name, bad)
        with patch.dict(globals(), SELECTOR=CLAIM_SELECTOR, TOP=CLAIM_TOP, CASES=CLAIM_CASES), \
                patch.object(self.sup, 'SCHEMA_ROOT', CLAIM_SELECTOR), \
                patch.object(self.sup, 'SCHEMA_CASES', CLAIM_CASES):
            self.test_exact_three_subcases_and_original_wait()
            self.test_actual_main_requires_exact_root_mode()
            self.test_actual_schema_inputs_reenumerate_runtime_sources()
            self.test_actual_observer_keeps_original_resource_tails()
            self.test_actual_driver_fixed_environment_before_original_exec()

    def test_actual_driver_fixed_environment_before_original_exec(self):
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


if __name__ == '__main__':
    unittest.main()
