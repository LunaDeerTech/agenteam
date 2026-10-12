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
MINIO = Path('/workspace/agenteam-skills-milestone-resume/output/ai/deps-minio/bin/minio')
MINIO_SHA = 'dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8'
# One closed same-package PG family: adding a scenario changes required
# inputs and expected test data, never the resource/Wait/tail implementation.
METADATA_INPUTS = {
    '^TestVariableProjectCallApp$': (
        'tests/projectvariable/variable_project_call_app_test.go',
        'tests/projectvariable/agent_project_call_app_test.go',
        'tests/projectvariable/agent_configuration_app_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/fixture_test.go',
        'tests/projectvariable/http_test.go',
        'tests/projectvariable/secret_fixture_test.go',
        'tests/projectvariable/secret_http_fixture_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'internal/central/app/account.go',
        'internal/central/app/project_variables.go',
        'internal/central/app/project_secret_variables.go',
        'internal/central/projectvariable/service.go',
        'internal/central/projectvariable/secret_service.go',
        'internal/central/projectvariable/commands.go',
        'internal/central/projectvariable/reader.go',
        'internal/central/projectvariable/secret_commands.go',
        'internal/central/projectvariable/secret_reader.go',
        'internal/central/projectvariable/secret_write_authority.go',
        'internal/central/projectvariable/project_lifecycle.go',
        'internal/central/projectvariable/project_call_ownership.go',
        'internal/central/projectvariable/project_call_repository.go',
        'internal/central/projectvariable/project_call_observer.go',
        'internal/central/object/process.go',
        'internal/central/object/process_binding.go',
        'internal/central/secret/project_variable_prepare.go',
        'db/migrations/embed.go',
        'db/migrations/00073_execution_round_model_claims.sql',
        'db/migrations/00074_agent_project_call_ownership.sql',
        'db/migrations/00075_project_variable_call_ownership.sql',
        '.agent-state/variable-project-call-app/profile-controls.py',
    ),
    '^TestVariableProjectCallOwnership$': (
        'tests/projectvariable/variable_project_call_ownership_test.go',
        'tests/projectvariable/variable_project_call_ownership_fixture_test.go',
        'tests/projectvariable/variable_project_call_ownership_process_test.go',
        'tests/projectvariable/agent_project_call_ownership_test.go',
        'tests/projectvariable/agent_project_call_ownership_fixture_test.go',
        'tests/projectvariable/agent_project_call_ownership_process_test.go',
        'tests/projectvariable/agent_project_lifecycle_test.go',
        'tests/projectvariable/agent_project_lifecycle_fixture_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/project_phase_stop_fixture_test.go',
        'tests/projectvariable/project_lifecycle_fixture_test.go',
        'tests/projectvariable/project_phase_recovery_guard_test.go',
        'tests/projectvariable/fixture_test.go',
        'tests/projectvariable/secret_fixture_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'internal/central/projectvariable/service.go',
        'internal/central/projectvariable/secret_service.go',
        'internal/central/projectvariable/commands.go',
        'internal/central/projectvariable/reader.go',
        'internal/central/projectvariable/secret_commands.go',
        'internal/central/projectvariable/secret_reader.go',
        'internal/central/projectvariable/secret_write_authority.go',
        'internal/central/projectvariable/project_lifecycle.go',
        'internal/central/projectvariable/project_call_ownership.go',
        'internal/central/projectvariable/project_call_repository.go',
        'internal/central/projectvariable/project_call_observer.go',
        'internal/central/object/process.go',
        'internal/central/object/process_binding.go',
        'internal/central/project/lifecycle_stop_round.go',
        'internal/central/secret/project_variable_prepare.go',
        'internal/central/secret/nonce.go',
        'db/migrations/00073_execution_round_model_claims.sql',
        'db/migrations/00074_agent_project_call_ownership.sql',
        'db/migrations/00075_project_variable_call_ownership.sql',
        '.agent-state/project-variables-independent/commitproxy/proxy.go',
        '.agent-state/variable-project-call-ownership/profile-controls.py',
    ),
    '^TestRunnerReadFileApp$': (
        'tests/projectvariable/runner_read_file_app_linux_test.go',
        'cmd/agenteam-runner/main.go',
        '.agent-state/runner-read-file-app/run-pg-checks.py',
        '.agent-state/runner-read-file-app/run-basic-checks.py',
        '.agent-state/runner-read-file-app/profile-controls.py',
        '.agent-state/runner-read-file-app/record-native.py',
    ),
    '^TestAgentProjectCallApp$': (
        'tests/projectvariable/agent_project_call_app_test.go',
        'tests/projectvariable/agent_configuration_app_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/fixture_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'internal/central/app/account.go',
        'internal/central/app/agent_configuration.go',
        'internal/central/app/agent_directory.go',
        'internal/central/agent/project_call_ownership.go',
        'internal/central/agent/project_call_repository.go',
        'internal/central/agent/project_call_observer.go',
        'internal/central/agent/calls.go',
        'internal/central/agent/service.go',
        'internal/central/agent/commands.go',
        'internal/central/agent/command_plan.go',
        'internal/central/agent/command_store.go',
        'internal/central/agent/reader.go',
        'internal/central/agent/runner_mounts.go',
        'internal/central/agent/directory.go',
        'internal/central/object/process.go',
        'internal/central/object/process_binding.go',
        'db/migrations/embed.go',
        'db/migrations/00073_execution_round_model_claims.sql',
        'db/migrations/00074_agent_project_call_ownership.sql',
        '.agent-state/project-variables-independent/commitproxy/proxy.go',
        '.agent-state/agent-project-call-app/profile-controls.py',
    ),
    '^TestRoundModelCallReservation$': (
        'tests/projectvariable/round_model_call_reservation_test.go',
        'tests/projectvariable/next_round_model_request_test.go',
        'tests/projectvariable/run_command_result_consumption_test.go',
        'tests/projectvariable/run_command_operation_test.go',
        'db/migrations/00066_execution_tool_result_consumptions.sql',
        'db/migrations/00068_execution_round_inputs.sql',
        'db/migrations/00071_execution_run_command_results.sql',
        'db/migrations/00072_execution_round_model_requests.sql',
        'db/migrations/00073_execution_round_model_claims.sql',
        'internal/central/execution/contract/round_model_call.go',
        'internal/central/execution/round_model_call.go',
        'internal/central/execution/round_model_call_owner.go',
        'internal/central/execution/round_model_call_repository.go',
        'internal/central/execution/contract/next_round_request_codec.go',
        'internal/central/execution/contract/round_model_request.go',
        'internal/central/execution/round_model_request.go',
        'internal/central/execution/round_model_request_owner.go',
        'internal/central/execution/round_model_request_repository.go',
        'internal/central/agentloop/next_round_request.go',
        'internal/central/object/process_binding.go',
        '.agent-state/round-model-call-reservation/profile-controls.py',
    ),
    '^TestAgentProjectCallOwnership$': (
        'tests/projectvariable/agent_project_call_ownership_test.go',
        'tests/projectvariable/agent_project_call_ownership_fixture_test.go',
        'tests/projectvariable/agent_project_call_ownership_process_test.go',
        'tests/projectvariable/agent_project_lifecycle_test.go',
        'tests/projectvariable/agent_project_lifecycle_fixture_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/project_phase_stop_fixture_test.go',
        'tests/projectvariable/project_phase_recovery_guard_test.go',
        'tests/projectvariable/fixture_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'internal/central/agent/project_call_ownership.go',
        'internal/central/agent/project_call_repository.go',
        'internal/central/agent/project_call_observer.go',
        'internal/central/agent/project_lifecycle.go',
        'internal/central/agent/calls.go',
        'internal/central/agent/service.go',
        'internal/central/agent/commands.go',
        'internal/central/agent/command_plan.go',
        'internal/central/agent/command_store.go',
        'internal/central/agent/reader.go',
        'internal/central/agent/runner_mounts.go',
        'internal/central/agent/directory.go',
        'internal/central/object/process.go',
        'internal/central/object/process_binding.go',
        'internal/central/project/lifecycle_stop_round.go',
        'db/migrations/00073_execution_round_model_claims.sql',
        'db/migrations/00074_agent_project_call_ownership.sql',
        '.agent-state/project-variables-independent/commitproxy/proxy.go',
        '.agent-state/agent-project-call-ownership/profile-controls.py',
    ),
    '^TestAgentProjectLocalStop$': (
        'tests/projectvariable/agent_project_lifecycle_test.go',
        'tests/projectvariable/agent_project_lifecycle_fixture_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/project_phase_stop_fixture_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'internal/central/agent/project_lifecycle.go',
        'internal/central/agent/calls.go',
        'internal/central/agent/service.go',
        'internal/central/agent/commands.go',
        'internal/central/agent/reader.go',
        'internal/central/agent/runner_mounts.go',
        'internal/central/agent/directory.go',
        'internal/central/project/lifecycle_stop_round.go',
        'db/migrations/00072_execution_round_model_requests.sql',
        '.agent-state/agent-project-local-stop/profile-controls.py',
    ),
    '^TestNextRoundModelRequest$': (
        'tests/projectvariable/next_round_model_request_test.go',
        'tests/projectvariable/run_command_result_consumption_test.go',
        'tests/projectvariable/run_command_operation_test.go',
        'db/migrations/00066_execution_tool_result_consumptions.sql',
        'db/migrations/00068_execution_round_inputs.sql',
        'db/migrations/00071_execution_run_command_results.sql',
        'db/migrations/00072_execution_round_model_requests.sql',
        'internal/central/execution/contract/next_round_request_codec.go',
        'internal/central/execution/contract/round_model_request.go',
        'internal/central/execution/round_model_request.go',
        'internal/central/execution/round_model_request_owner.go',
        'internal/central/execution/round_model_request_repository.go',
        'internal/central/agentloop/next_round_request.go',
        '.agent-state/next-round-model-request/profile-controls.py',
    ),
    '^TestRunCommandResultConsumption$': (
        'tests/projectvariable/run_command_result_consumption_test.go',
        'tests/projectvariable/run_command_operation_test.go',
        'db/migrations/00066_execution_tool_result_consumptions.sql',
        'db/migrations/00068_execution_round_inputs.sql',
        'db/migrations/00071_execution_run_command_results.sql',
        'internal/central/tool/runtime/run_command_result.go',
        'internal/central/execution/run_command_result_authority.go',
        'internal/central/execution/run_command_result_consumption.go',
        'internal/central/agentloop/run_command_result.go',
        '.agent-state/run-command-result-consumption/profile-controls.py',
    ),
    '^TestWriteFileResultConsumption$': (
        'tests/projectvariable/write_file_result_consumption_test.go',
        'db/migrations/00066_execution_tool_result_consumptions.sql',
        'db/migrations/00068_execution_round_inputs.sql',
        'internal/central/tool/runtime/write_file_result.go',
        'internal/central/execution/write_file_result_authority.go',
        'internal/central/execution/write_file_result_consumption.go',
        'internal/central/agentloop/write_file_result.go',
        '.agent-state/write-file-result-consumption/profile-controls.py',
    ),
    '^TestRunCommandOperation$': (
        'tests/projectvariable/run_command_operation_test.go',
        'db/migrations/00070_runner_command_operations.sql',
        'internal/central/tool/runtime/run_command_executor.go',
        'internal/central/tool/runtime/run_command_authority.go',
        'internal/central/mount/command.go',
        'internal/central/runner/http/command_dispatch.go',
        '.agent-state/runner-command-operation/profile-controls.py',
    ),
    '^TestWriteFileOperation$': (
        'tests/projectvariable/write_file_operation_test.go',
        'db/migrations/00069_runner_write_file_operations.sql',
        'internal/central/tool/runtime/write_file_executor.go',
        'internal/central/tool/runtime/write_file_authority.go',
        'internal/central/mount/write_file.go',
        'internal/central/runner/http/write_file_dispatch.go',
        '.agent-state/runner-write-file-operation/profile-controls.py',
    ),
    '^TestNextRoundProjection$': (
        'tests/projectvariable/next_round_projection_test.go',
        'db/migrations/00068_execution_round_inputs.sql',
        'internal/central/execution/contract/round_projection.go',
        'internal/central/execution/round_projection.go',
        'internal/central/agentloop/round_projection.go',
        '.agent-state/next-round-projection/profile-controls.py',
    ),
    '^TestExecutionRoundInput$': (
        'tests/projectvariable/execution_round_input_test.go',
        'db/migrations/00068_execution_round_inputs.sql',
        'internal/central/execution/round_input.go',
        'internal/central/execution/round_input_repository.go',
        'internal/central/skill/assignment_validation.go',
        '.agent-state/execution-round-input/profile-controls.py',
    ),
    '^TestSkillAssignmentIngress$': (
        'tests/projectvariable/skill_assignment_ingress_test.go',
        'db/migrations/00067_skill_assignment_ingress.sql',
        'internal/central/skill/assignment_commands.go',
        'internal/central/skill/assignment_authority.go',
        'internal/central/execution/skill_assignment_ingress.go',
        '.agent-state/skill-assignment-ingress/profile-controls.py',
    ),
    '^TestExecutionToolResultConsumption$': (
        'tests/projectvariable/execution_tool_result_consumption_test.go',
        'tests/projectvariable/runner_read_file_operation_test.go',
        'tests/projectvariable/execution_tool_call_facts_test.go',
        'db/migrations/00066_execution_tool_result_consumptions.sql',
        'internal/central/execution/contract/tool_result_consumption.go',
        'internal/central/execution/tool_result_consumption.go',
        'internal/central/execution/tool_result_consumption_repository.go',
        'internal/central/tool/contract/read_file_result.go',
        'internal/central/tool/runtime/read_file_result.go',
        'internal/central/agentloop/tool_result.go',
        '.agent-state/execution-tool-result-consumption/profile-controls.py',
    ),
    '^TestRunnerReadFileOperation$': (
        'tests/projectvariable/runner_read_file_operation_test.go',
        'tests/projectvariable/runner_read_file_adapter_test.go',
        'tests/projectvariable/execution_tool_call_facts_test.go',
        'db/migrations/00065_tool_read_file_operations.sql',
        'internal/central/execution/read_file_authority.go',
        'internal/central/tool/contract/read_file_operation.go',
        'internal/central/tool/runtime/read_file_authority.go',
        'internal/central/tool/runtime/read_file_executor.go',
        'internal/central/tool/runtime/read_file_attempt.go',
        'internal/central/tool/runtime/read_file_lookup.go',
        'internal/central/tool/runtime/read_file_profile.go',
        'internal/central/tool/runtime/read_file_repository.go',
        'internal/central/tool/runtime/read_file_schema.go',
        '.agent-state/runner-read-file-operation/profile-controls.py',
    ),
    '^TestExecutionToolCallFacts$': (
        'tests/projectvariable/execution_tool_call_facts_test.go',
        'tests/projectvariable/execution_mount_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'db/migrations/00063_tool_runner_bindings.sql',
        'db/migrations/00064_execution_tool_call_facts.sql',
        'internal/central/execution/tool_call.go',
        'internal/central/execution/tool_call_authority.go',
        'internal/central/execution/tool_call_repository.go',
        'internal/central/agentloop/tool_call.go',
        'internal/central/tool/registry/runner_binding_repository.go',
        '.agent-state/execution-tool-call-facts/profile-controls.py',
    ),
    '^TestRunnerDirectoryAdapter$': (
        'tests/projectvariable/runner_directory_adapter_test.go',
        'tests/projectvariable/runner_read_file_adapter_test.go',
        'internal/central/mount/contract/directory.go',
        'internal/central/mount/directory.go',
        'internal/central/mount/current_mount.go',
        'internal/central/runner/http/directory_dispatch.go',
        'internal/runnerprotocol/directory.go',
        'internal/runner/directory/handler.go',
        'internal/runner/control/directory.go',
        '.agent-state/runner-directory-adapter/profile-controls.py',
    ),
    '^TestRunnerReadFileAdapter$': (
        'tests/projectvariable/runner_read_file_adapter_test.go',
        'tests/projectvariable/agent_runner_mounts_integration_test.go',
        'internal/central/mount/contract/read_file.go',
        'internal/central/mount/read_file.go',
        'internal/central/runner/http/read_file_dispatch.go',
        'internal/runnerprotocol/read_file.go',
        'internal/runner/readfile/handler.go',
        'internal/runner/control/read_file.go',
        '.agent-state/runner-read-file-adapter/profile-controls.py',
    ),
    '^TestExecutionMountFirstRound$': (
        'tests/projectvariable/execution_mount_first_round_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_mount_capture_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'tests/projectvariable/execution_capture_providers_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'db/migrations/00061_execution_mount_capture.sql',
        'db/migrations/00062_execution_mount_use.sql',
        'internal/central/mount/contract/execution_capture.go',
        'internal/central/mount/contract/execution_use.go',
        'internal/central/mount/contract/workspace.go',
        'internal/central/mount/execution_use.go',
        'internal/central/mount/execution_use_repository.go',
        'internal/central/runner/contract/execution_capture.go',
        'internal/central/runner/contract/runner.go',
        'internal/central/runner/contract/commands.go',
    ),
    '^TestExecutionMountCapture$': (
        'tests/projectvariable/execution_mount_capture_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'tests/projectvariable/execution_capture_providers_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'db/migrations/00061_execution_mount_capture.sql',
        'internal/central/mount/contract/execution_capture.go',
        'internal/central/mount/contract/workspace.go',
        'internal/central/runner/contract/execution_capture.go',
        'internal/central/runner/contract/runner.go',
        'internal/central/runner/contract/commands.go',
    ),
    '^TestAgentRunnerMountsIntegration$': (
        'tests/projectvariable/agent_runner_mounts_integration_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
        'api/openapi/agent-configuration.json',
        'api/openapi/agent-runner-mounts.json',
    ),
    '^TestProjectOutboxStopParticipant$': (
        'tests/project/outbox_stop_participant_test.go',
        'tests/project/b03_r3_outbox_inspection_test.go',
        'tests/project/b03_secret_audit_fixture_test.go',
        'internal/central/project/outbox_stop_participant.go',
    ),
    '^TestObjectRuntimeJoin(ConcurrentDrainAndForceBudget|FirstReadWriterBeforeRegistration|ConfirmationFailureRetainsOwnership|ReadyWorkMakesProgress)$': (
        'tests/objects/runtime_join_test.go',
        'tests/objects/runtime_test.go',
        'tests/objects/common_test.go',
    ),
    '^TestObject(RuntimeForceJoinsMainAndTransferIOInOneBudget|RuntimeRecoveryCleanupKeepsStartupAndWorkerBudget|RuntimeBusyDoesNotMaskLaterRecoveryFailure|RuntimeMetadataGateUntilActualProbeAndRecovery|ProjectStopDeleteWaitsForActualIO|ProjectStopCleanupWaitsForActualIOAndCallbacks|CancelProtectsActualSourceUntilJoined)$': (
        'tests/objects/runtime_shutdown_test.go',
        'tests/objects/runtime_recovery_test.go',
        'tests/objects/project_stop_io_test.go',
        'tests/objects/read_cleanup_test.go',
        'tests/objects/runtime_test.go',
        'tests/objects/common_test.go',
    ),
    '^TestObjectProjectWorkPendingJoinKeepsRuntimeGuard$': (
        'tests/objects/runtime_join_test.go',
        'tests/objects/runtime_test.go',
        'tests/objects/common_test.go',
    ),
    '^TestProjectDomainBarrierParticipants$': (
        'tests/project/domain_barrier_participants_test.go',
        'tests/project/b03_secret_audit_fixture_test.go',
        'internal/central/project/domain_barrier_participants.go',
    ),
    '^TestProjectSchedulerWeb$': (
        'internal/central/app/project_scheduler_web_test.go',
        'internal/central/app/skill_package_web_test.go',
        'internal/central/app/skill_owner_web_test.go',
        'internal/central/app/knowledge_owner_web_test.go',
        'internal/central/app/knowledge_skills_process_test.go',
        'internal/central/app/outbox_process_test.go',
        'internal/central/app/app_test.go',
        'tests/account-captcha-web/project-scheduler.config.js',
        'tests/account-captcha-web/e2e/project-scheduler.spec.ts',
        'tests/account-captcha-web/e2e/project-scheduler.helpers.ts',
        'tests/account-captcha-web/e2e/task-review.helpers.ts',
        'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts',
        'api/openapi/project-scheduler.json',
        'api/openapi/project-owner.json',
        'api/openapi/common.json',
    ),
    '^TestProjectSchedulerOwnerHTTP$': (
        'tests/projectvariable/project_scheduler_http_test.go',
        'tests/projectvariable/fixture_test.go',
        'api/openapi/project-scheduler.json',
        'api/openapi/project-owner.json',
        'api/openapi/common.json',
    ),
    '^TestSkillPackageWeb$': ('internal/central/app/skill_package_web_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/skill-package.config.js', 'tests/account-captcha-web/e2e/skill-package.spec.ts', 'tests/account-captcha-web/e2e/skill-package.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/skill-package.json', 'api/openapi/skill-owner.json', 'api/openapi/common.json'),
    '^TestKnowledgeTextWeb$': ('internal/central/app/knowledge_text_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/knowledge-text.config.js', 'tests/account-captcha-web/e2e/knowledge-text.spec.ts', 'tests/account-captcha-web/e2e/knowledge-text.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/knowledge-text-commands.json', 'api/openapi/knowledge-owner.json', 'api/openapi/knowledge-content.json', 'api/openapi/common.json'),
    '^TestKnowledgeTitleSearchWeb$': ('internal/central/app/knowledge_title_search_web_test.go', 'internal/central/app/knowledge_text_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/knowledge-title-search.config.js', 'tests/account-captcha-web/e2e/knowledge-title-search.spec.ts', 'tests/account-captcha-web/e2e/knowledge-title-search.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/knowledge-text-commands.json', 'api/openapi/knowledge-owner.json', 'api/openapi/knowledge-content.json', 'api/openapi/common.json'),
    '^TestKnowledgeDeleteWeb$': ('internal/central/app/knowledge_delete_web_test.go', 'internal/central/app/knowledge_text_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/knowledge-delete.config.js', 'tests/account-captcha-web/e2e/knowledge-delete.spec.ts', 'tests/account-captcha-web/e2e/knowledge-delete.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/knowledge-text-commands.json', 'api/openapi/knowledge-tree-commands.json', 'api/openapi/knowledge-owner.json', 'api/openapi/knowledge-content.json', 'api/openapi/common.json'),
    '^TestProjectUsageWeb$': ('tests/model/project_usage_web_test.go', 'tests/model/project_usage_http_fixture_test.go', 'tests/model/project_usage_http_root_test.go', 'tests/model/usage_fixture_test.go', 'tests/account-captcha-web/project-usage.config.js', 'tests/account-captcha-web/e2e/project-usage.spec.ts', 'tests/account-captcha-web/e2e/project-usage.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/project-usage.json', 'api/openapi/project-owner.json', 'api/openapi/common.json'),
    '^TestProjectSecretsWeb$': ('internal/central/app/project_secrets_web_test.go', 'internal/central/app/knowledge_title_search_web_test.go', 'internal/central/app/knowledge_text_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/project-secrets.config.js', 'tests/account-captcha-web/e2e/project-secrets.spec.ts', 'tests/account-captcha-web/e2e/project-secrets.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/secret-variables.json', 'api/openapi/project-owner.json', 'api/openapi/common.json'),
    '^TestProjectSecretWriteWeb$': ('internal/central/app/project_secret_write_web_test.go', 'internal/central/app/project_secrets_web_test.go', 'internal/central/app/knowledge_title_search_web_test.go', 'internal/central/app/knowledge_text_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/skill_owner_web_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/project-secret-write.config.js', 'tests/account-captcha-web/e2e/project-secret-write.spec.ts', 'tests/account-captcha-web/e2e/project-secret-write.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/secret-variables.json', 'api/openapi/project-owner.json', 'api/openapi/common.json'),
    '^TestSkillOwnerPackageHTTP(Publication|CurrentAuthority)$': (
        'tests/skills/owner_package_http_fixture_test.go',
        'tests/skills/owner_package_http_test.go',
        'api/openapi/skill-package.json',
        'api/openapi/skill-owner.json',
        'api/openapi/common.json',
    ),
    '^TestSkillPackageHTTPDefaultRootComposition$': (
        'internal/central/app/skill_package_http_process_test.go',
        'internal/central/app/knowledge_skills.go',
        'internal/central/app/knowledge_skills_process_test.go',
        'api/openapi/skill-package.json',
        'api/openapi/skill-owner.json',
        'api/openapi/common.json',
    ),
    '^TestProjectVariablesWeb$': ('tests/projectvariable/project_variables_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/project-variables.config.js', 'tests/account-captcha-web/e2e/project-variables.spec.ts', 'tests/account-captcha-web/e2e/project-variables.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestMilestoneCreateWeb$': ('tests/projectvariable/milestone_create_web_test.go', 'tests/projectvariable/sprint_create_web_test.go', 'tests/projectvariable/sprint_start_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/milestone-create.config.js', 'tests/account-captcha-web/e2e/milestone-create.spec.ts', 'tests/account-captcha-web/e2e/milestone-create.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestMilestoneEditWeb$': ('tests/projectvariable/milestone_edit_web_test.go', 'tests/projectvariable/milestone_create_web_test.go', 'tests/projectvariable/sprint_create_web_test.go', 'tests/projectvariable/sprint_start_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/milestone-edit.config.js', 'tests/account-captcha-web/e2e/milestone-edit.spec.ts', 'tests/account-captcha-web/e2e/milestone-edit.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestSprintEditWeb$': ('tests/projectvariable/sprint_edit_web_test.go', 'tests/projectvariable/milestone_create_web_test.go', 'tests/projectvariable/sprint_create_web_test.go', 'tests/projectvariable/sprint_start_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/sprint-edit.config.js', 'tests/account-captcha-web/e2e/sprint-edit.spec.ts', 'tests/account-captcha-web/e2e/sprint-edit.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestSkillOwnerReadWeb$': ('internal/central/app/skill_owner_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'internal/central/app/outbox_process_test.go', 'internal/central/app/app_test.go', 'tests/account-captcha-web/skill-owner-read.config.js', 'tests/account-captcha-web/e2e/skill-owner-read.spec.ts', 'tests/account-captcha-web/e2e/skill-owner-read.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestKnowledgeTextHTTP(Publication|CurrentAuthority)$': (
        'tests/knowledge/owner_text_http_fixture_test.go',
        'tests/knowledge/owner_text_http_test.go',
        'api/openapi/knowledge-text-commands.json',
        'api/openapi/knowledge-owner.json',
        'api/openapi/knowledge-content.json',
        'api/openapi/common.json',
    ),
    '^TestKnowledgeTextHTTPDefaultRootComposition$': (
        'internal/central/app/knowledge_skills.go',
        'internal/central/app/knowledge_text_http_process_test.go',
        'internal/central/app/knowledge_skills_process_test.go',
        'api/openapi/knowledge-text-commands.json',
        'api/openapi/knowledge-owner.json',
        'api/openapi/knowledge-content.json',
        'api/openapi/common.json',
    ),
    '^TestSprintCreateWeb$': ('tests/projectvariable/sprint_create_web_test.go', 'tests/projectvariable/sprint_start_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/execution_preparation_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/sprint-create.config.js', 'tests/account-captcha-web/e2e/sprint-create.spec.ts', 'tests/account-captcha-web/e2e/sprint-create.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestAgentModelPolicyWeb$': ('tests/projectvariable/agent_model_policy_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/agent-model-policy.config.js', 'tests/account-captcha-web/e2e/agent-model-policy.spec.ts', 'tests/account-captcha-web/e2e/agent-model-policy.helpers.ts', 'tests/account-captcha-web/e2e/agent-configuration.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestKnowledgeOwnerRenameWeb$': ('internal/central/app/knowledge_owner_rename_web_test.go', 'internal/central/app/knowledge_owner_web_test.go', 'internal/central/app/knowledge_skills_process_test.go', 'tests/account-captcha-web/knowledge-owner-rename.config.js', 'tests/account-captcha-web/e2e/knowledge-owner-rename.spec.ts', 'tests/account-captcha-web/e2e/knowledge-owner-rename.native.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts', 'api/openapi/common.json', 'api/openapi/knowledge-owner.json', 'api/openapi/knowledge-content.json', 'api/openapi/knowledge-tree-commands.json'),
    '^TestSprintStartWeb$': ('tests/projectvariable/sprint_start_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/execution_preparation_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/sprint-start.config.js', 'tests/account-captcha-web/e2e/sprint-start.spec.ts', 'tests/account-captcha-web/e2e/sprint-start.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestAgentConfigurationUpdateApp$': ('tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go'),
    '^TestAgentEditWeb$': ('tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/agent-edit.config.js', 'tests/account-captcha-web/e2e/agent-edit.spec.ts', 'tests/account-captcha-web/e2e/agent-edit.helpers.ts', 'tests/account-captcha-web/e2e/agent-configuration.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestSystemRunnerManagementWeb$': ('tests/projectvariable/system_runner_management_web_test.go', 'tests/projectvariable/agent_runner_mounts_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/system-runner-management.config.js', 'tests/account-captcha-web/e2e/system-runner-management.spec.ts', 'tests/account-captcha-web/e2e/system-runner-management.helpers.ts', 'tests/account-captcha-web/e2e/agent-runner-mounts.helpers.ts', 'tests/account-captcha-web/e2e/agent-configuration.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestAgentRunnerMountsWeb$': ('tests/projectvariable/agent_runner_mounts_web_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/agent-runner-mounts.config.js', 'tests/account-captcha-web/e2e/agent-runner-mounts.spec.ts', 'tests/account-captcha-web/e2e/agent-runner-mounts.helpers.ts', 'tests/account-captcha-web/e2e/agent-configuration.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestAgentSecretAllowlistWeb$': ('tests/projectvariable/agent_secret_allowlist_web_test.go', 'tests/projectvariable/agent_runner_mounts_web_test.go', 'tests/projectvariable/secret_fixture_test.go', 'tests/projectvariable/agent_edit_web_test.go', 'tests/projectvariable/agent_configuration_app_test.go', 'tests/projectvariable/agent_configuration_web_test.go', 'tests/projectvariable/agent_configuration_create_test.go', 'tests/projectvariable/agent_configuration_facts_test.go', 'tests/projectvariable/skill_installation_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/testsupport/agentconfiguration/assembly.go', 'tests/account-captcha-web/agent-secret-allowlist.config.js', 'tests/account-captcha-web/e2e/agent-secret-allowlist.spec.ts', 'tests/account-captcha-web/e2e/agent-secret-allowlist.helpers.ts', 'tests/account-captcha-web/e2e/agent-runner-mounts.helpers.ts', 'tests/account-captcha-web/e2e/agent-configuration.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestTaskTimelineWeb$': ('tests/projectvariable/task_timeline_web_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/projectvariable/task_transition_scheduler_test.go', 'tests/projectvariable/fixture_test.go', 'tests/account-captcha-web/task-timeline.config.js', 'tests/account-captcha-web/e2e/task-timeline.spec.ts', 'tests/account-captcha-web/e2e/task-timeline.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestTaskTimeline(PersistenceAndPaging|AuthorityAndCancellation|IntegrityAndCompatibility)$': ('tests/work/task_timeline_test.go', 'tests/work/fixture_test.go', 'tests/work/task_fixture_test.go', 'tests/work/task_blocker_persistence_test.go', 'tests/work/task_concurrency_test.go'),
    "^TestAgentConfigurationWeb$": ("tests/projectvariable/agent_configuration_web_test.go", "tests/projectvariable/agent_configuration_app_test.go", "tests/projectvariable/agent_configuration_create_test.go", "tests/projectvariable/agent_configuration_facts_test.go", "tests/projectvariable/skill_installation_test.go", "tests/projectvariable/fixture_test.go", "tests/projectvariable/task_review_web_test.go", "tests/testsupport/agentconfiguration/assembly.go", "tests/account-captcha-web/agent-configuration.config.js", "tests/account-captcha-web/e2e/agent-configuration.spec.ts", "tests/account-captcha-web/e2e/agent-configuration.helpers.ts", "tests/account-captcha-web/e2e/task-review.helpers.ts", "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",),
    "^TestTaskEditWeb$": ("tests/projectvariable/task_edit_web_test.go", "tests/projectvariable/task_review_web_test.go", "tests/projectvariable/task_intake_web_test.go", "tests/projectvariable/task_transition_scheduler_test.go", "tests/projectvariable/scheduler_claim_test.go", "tests/account-captcha-web/task-edit.config.js", "tests/account-captcha-web/e2e/task-edit.spec.ts", "tests/account-captcha-web/e2e/task-edit.helpers.ts", "tests/account-captcha-web/e2e/task-review.helpers.ts", "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",),
    "^TestAgentConfigurationApp$": ("tests/projectvariable/agent_configuration_app_test.go", "tests/projectvariable/agent_configuration_create_test.go", "tests/projectvariable/agent_configuration_facts_test.go", "tests/projectvariable/skill_installation_test.go", "tests/projectvariable/fixture_test.go", "tests/testsupport/agentconfiguration/assembly.go", "tests/projectvariable/execution_preparation_test.go", "tests/projectvariable/task_transition_scheduler_test.go",),
    '^TestTaskIntakeWeb$': ('tests/projectvariable/task_intake_web_test.go', 'tests/projectvariable/task_review_web_test.go', 'tests/projectvariable/task_transition_scheduler_test.go', 'tests/projectvariable/scheduler_claim_test.go', 'tests/account-captcha-web/task-intake.config.js', 'tests/account-captcha-web/e2e/task-intake.spec.ts', 'tests/account-captcha-web/e2e/task-intake.helpers.ts', 'tests/account-captcha-web/e2e/task-review.helpers.ts', 'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'),
    '^TestAppExecutionRuntime$': ('tests/projectvariable/app_execution_runtime_test.go', 'tests/projectvariable/fixture_test.go', 'tests/projectvariable/execution_model_environment_capture_test.go', 'tests/projectvariable/execution_first_round_test.go'),
    '^TestTaskReviewWeb$': (
        'tests/projectvariable/task_review_web_test.go',
        'tests/projectvariable/task_review_test.go',
        'tests/account-captcha-web/task-review.config.js',
        'tests/account-captcha-web/e2e/task-review.spec.ts',
        'tests/account-captcha-web/e2e/task-review.helpers.ts',
        'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts',
    ),
    '^TestExecutionRuntimeAccessAudit$': (
        'tests/projectvariable/execution_runtime_access_audit_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestAgentDirectoryHTTP$': (
        'tests/projectvariable/agent_directory_http_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/fixture_test.go',
    ),
    '^TestProjectRunnerManager$': (
        'tests/projectvariable/project_runners_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/projectvariable/scheduler_execution_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'tests/projectvariable/scheduler_pending_visit_test.go',
    ),
    '^TestSchedulerReviewDispatch$': (
        'tests/projectvariable/scheduler_review_test.go',
        'tests/projectvariable/scheduler_relaunch_test.go',
        'tests/projectvariable/task_review_test.go',
        'tests/projectvariable/scheduler_execution_test.go',
        'tests/projectvariable/scheduler_launch_failure_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestTaskHumanReview$': (
        'tests/projectvariable/task_review_test.go',
        'tests/projectvariable/task_technical_resolution_http_test.go',
        'tests/projectvariable/task_human_http_test.go',
        'tests/projectvariable/scheduler_relaunch_test.go',
        'tests/projectvariable/scheduler_execution_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestSchedulerRelaunch$': (
        'tests/projectvariable/scheduler_relaunch_test.go',
        'tests/projectvariable/scheduler_execution_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestSchedulerExecution$': (
        'tests/projectvariable/scheduler_execution_test.go',
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestExecutionFirstRound$': (
        'tests/projectvariable/execution_first_round_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
    ),
    '^TestModelAgentRetryRuntime$': (
        'tests/model/agent_runtime_test.go',
        'tests/model/runtime_native_test.go',
        'tests/testsupport/outbound/fixture.go',
        'tests/testsupport/outbound/cmd/server/main.go',
    ),
    '^TestExecutionTaskContext$': (
        'tests/projectvariable/execution_context_test.go',
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'tests/projectvariable/execution_capture_providers_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
    ),
    '^TestExecutionModelEnvironmentCapture$': (
        'tests/projectvariable/execution_model_environment_capture_test.go',
        'tests/projectvariable/execution_capture_providers_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
    ),
    '^TestExecutionCaptureProviders$': (
        'tests/projectvariable/execution_capture_providers_test.go',
        'tests/projectvariable/agent_configuration_create_test.go',
    ),
    '^TestSchedulerProjectRunner$': (
        'tests/projectvariable/scheduler_project_runner_test.go',
    ),
    '^TestSchedulerBoundedRetry$': (
        'tests/projectvariable/scheduler_bounded_retry_test.go',
        'tests/projectvariable/scheduler_busy_compensation_test.go',
        'tests/projectvariable/scheduler_retry_binding_test.go',
        'tests/projectvariable/scheduler_launch_failure_test.go',
    ),
    '^TestSchedulerRetryBinding$': (
        'tests/projectvariable/scheduler_retry_binding_test.go',
        'tests/projectvariable/scheduler_claim_test.go',
    ),
    '^(TestTaskTechnicalResolutionHTTP|TestTaskTechnicalResolutionAtomic)$': (
        'tests/projectvariable/task_technical_resolution_http_test.go',
        'tests/projectvariable/task_unblock_atomic_test.go',
    ),
    '^TestSchedulerLaunchFinalFailure$': (
        'tests/projectvariable/scheduler_launch_failure_test.go',
    ),
    '^TestAgentConfigurationSchema$': (
        'tests/projectvariable/agent_configuration_schema_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
    ),
    '^TestAgentRuntimeSchema$': (
        'tests/projectvariable/agent_runtime_schema_test.go',
    ),
    '^TestExecutionPreparation$': (
        'tests/projectvariable/execution_preparation_test.go',
    ),
    '^TestAgentConfigurationCreate$': (
        'tests/projectvariable/agent_configuration_create_test.go',
        'tests/projectvariable/agent_configuration_facts_test.go',
        'tests/projectvariable/skill_installation_test.go',
        'tests/testsupport/agentconfiguration/assembly.go',
    ),
    '^TestTaskTransitionHuman$': (
        'tests/projectvariable/task_transition_scheduler_test.go',
    ),
    '^TestSchedulerClaim$': (
        'tests/projectvariable/scheduler_claim_test.go',
    ),
    '^TestSchedulerLaunch$': (
        'tests/projectvariable/scheduler_launch_test.go',
    ),
    '^TestSchedulerBusyCompensation$': (
        'tests/projectvariable/scheduler_busy_compensation_test.go',
    ),
    '^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$': (
        'tests/projectvariable/scheduler_pending_visit_test.go',
        'tests/projectvariable/task_human_http_test.go',
    ),
    '^TestSprintStartHTTP$': (
        'tests/projectvariable/task_human_http_test.go',
    ),
    '^TestAgentConfigurationMetadata$': (
        'tests/projectvariable/agent_configuration_metadata_test.go',
        '.agent-state/agent-configuration-metadata/README.md',
        '.agent-state/agent-configuration-metadata/metadata-entry-controls.py'),
    '^TestSkillInstallationPersistentObject$': (
        'tests/projectvariable/skill_installation_test.go',),
    '^TestSkillInstallationOwnerHTTP$': (
        'tests/projectvariable/skill_installation_http_test.go',
        'tests/projectvariable/skill_installation_test.go'),
    '^TestSkillInstallation(PersistentObject|OwnerHTTP)$': (
        'tests/projectvariable/skill_installation_http_test.go',
        'tests/projectvariable/skill_installation_test.go'),
}
TARGETS = {
    '^TestAgentRunnerMountsWeb$': 'tests/projectvariable',
    **dict.fromkeys(METADATA_INPUTS, 'tests/projectvariable'),
    '^TestAgentRunnerMountsIntegration$': 'tests/projectvariable',
    '^TestProjectOutboxStopParticipant$': 'tests/project',
    '^TestObjectRuntimeJoin(ConcurrentDrainAndForceBudget|FirstReadWriterBeforeRegistration|ConfirmationFailureRetainsOwnership|ReadyWorkMakesProgress)$': 'tests/objects',
    '^TestObject(RuntimeForceJoinsMainAndTransferIOInOneBudget|RuntimeRecoveryCleanupKeepsStartupAndWorkerBudget|RuntimeBusyDoesNotMaskLaterRecoveryFailure|RuntimeMetadataGateUntilActualProbeAndRecovery|ProjectStopDeleteWaitsForActualIO|ProjectStopCleanupWaitsForActualIOAndCallbacks|CancelProtectsActualSourceUntilJoined)$': 'tests/objects',
    '^TestObjectProjectWorkPendingJoinKeepsRuntimeGuard$': 'tests/objects',
    '^TestProjectDomainBarrierParticipants$': 'tests/project',
    '^TestProjectSchedulerWeb$': 'internal/central/app',
    '^TestSkillPackageWeb$': 'internal/central/app',
    '^TestSkillOwnerPackageHTTP(Publication|CurrentAuthority)$': 'tests/skills',
    '^TestSkillPackageHTTPDefaultRootComposition$': 'internal/central/app',
    '^TestKnowledgeTextHTTP(Publication|CurrentAuthority)$': 'tests/knowledge',
    '^TestKnowledgeTextHTTPDefaultRootComposition$': 'internal/central/app',
    '^TestKnowledgeTextWeb$': 'internal/central/app',
    '^TestKnowledgeTitleSearchWeb$': 'internal/central/app',
    '^TestKnowledgeDeleteWeb$': 'internal/central/app',
    '^TestProjectSecretsWeb$': 'internal/central/app',
    '^TestProjectSecretWriteWeb$': 'internal/central/app',
    '^TestProjectUsageWeb$': 'tests/model',
    '^TestKnowledgeOwnerRenameWeb$': 'internal/central/app',
    '^TestSkillOwnerReadWeb$': 'internal/central/app',
    '^TestTaskTimeline(PersistenceAndPaging|AuthorityAndCancellation|IntegrityAndCompatibility)$': 'tests/work',
    '^TestModelAgentRetryRuntime$': 'tests/model',
    '^TestProjectLifecycleStopBatchRealGuard$': 'tests/projectvariable',
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


def runner_read_file_app_inputs(binary):
    """Only this profile may supply its two prebuilt commands and compile union."""
    def regular(raw):
        if not isinstance(raw, str) or not raw:
            raise ValueError('Runner profile requires a canonical input path')
        path = Path(raw)
        if (not path.is_absolute() or path.is_symlink() or not path.is_file()
                or str(path) != raw or path.resolve(strict=True) != path):
            raise ValueError('Runner profile requires regular inputs without symlinks')
        return path

    def digest_shape(value):
        return (isinstance(value, str) and len(value) == 64
                and all(c in '0123456789abcdef' for c in value))

    descriptor = regular(os.environ.get('AGENTEAM_RUNNER_READ_FILE_APP_INPUTS', ''))
    if descriptor.stat().st_size > 8 << 20:
        raise ValueError('Runner compile descriptor exceeds its metadata bound')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate Runner compile descriptor key')
            result[key] = value
        return result
    value = json.loads(descriptor.read_text(), object_pairs_hook=unique)
    if (not isinstance(value, dict)
            or set(value) != {'schema', 'selector', 'artifacts', 'source_inputs', 'compiler_inputs'}
            or type(value['schema']) is not int or value['schema'] != 1
            or value['selector'] != '^TestRunnerReadFileApp$'):
        raise ValueError('exact Runner read-file compile descriptor required')
    artifacts, sources, closures = value['artifacts'], value['source_inputs'], value['compiler_inputs']
    if (not isinstance(artifacts, dict) or set(artifacts) != {'runner', 'pg'}
            or not isinstance(sources, dict) or not 1 <= len(sources) <= 40000
            or not isinstance(closures, dict) or set(closures) != {'runner', 'pg'}):
        raise ValueError('complete two-command Runner compilation required')
    paths = {descriptor}
    for kind in ('runner', 'pg'):
        artifact = artifacts[kind]
        if (not isinstance(artifact, dict) or set(artifact) != {'path', 'sha256', 'size_bytes'}
                or not digest_shape(artifact['sha256']) or type(artifact['size_bytes']) is not int
                or artifact['size_bytes'] <= 0):
            raise ValueError('complete Runner artifact identity required')
        path = regular(artifact['path'])
        if (path == GO or not os.access(path, os.X_OK)
                or path.stat().st_size != artifact['size_bytes'] or sha(path) != artifact['sha256']):
            raise ValueError('original Runner artifact changed')
        paths.add(path)
        closure = closures[kind]
        if (not isinstance(closure, list) or not closure
                or any(not isinstance(name, str) for name in closure)
                or closure != sorted(set(closure)) or not set(closure).issubset(sources)):
            raise ValueError('actual Runner compiler closure is incomplete')
    if (descriptor.name != 'runner-read-file-inputs.json'
            or artifacts['runner']['path'] != str(descriptor.parent / 'agenteam-runner')
            or artifacts['pg']['path'] != str(descriptor.parent / 'projectvariable.test')
            or artifacts['runner']['path'] != os.environ.get('AGENTEAM_RUNNER_BINARY')
            or artifacts['pg']['path'] != str(Path(binary))):
        raise ValueError('Runner and PG binary identities must match the original invocation')
    required = {'runner': REPOSITORY / 'cmd/agenteam-runner/main.go',
                'pg': REPOSITORY / 'tests/projectvariable/runner_read_file_app_linux_test.go'}
    if any(str(path) not in closures[kind] for kind, path in required.items()):
        raise ValueError('required original command or PG source missing')
    for name, expected in sources.items():
        path = regular(name)
        if not digest_shape(expected) or sha(path) != expected:
            raise ValueError('original Runner compilation source changed')
        paths.add(path)
    if (str(GO) not in sources or artifacts['runner']['path'] in sources
            or artifacts['pg']['path'] in sources or str(descriptor) in sources):
        raise ValueError('compile sources and produced Runner artifacts must be distinct')
    return paths, {'descriptor': str(descriptor), 'sha256': sha(descriptor),
                   'artifacts': artifacts, 'source_count': len(sources),
                   'compiler_counts': {kind: len(items) for kind, items in closures.items()}}


def metadata_inputs(binary, selector='^TestAgentConfigurationMetadata$'):
    if selector not in METADATA_INPUTS:
        raise ValueError('exact configuration family selector required')
    # Include the compiled package's complete fixtures and the original shared
    # support, not just the new top or the unrelated Model test package.
    paths = set(input_paths(binary)) | set((REPOSITORY / TARGETS[selector]).glob('*.go'))
    # Preserve the author's conservative full-internal compile provenance,
    # including other packages' test sources and non-Go package assets.
    paths.update(p for p in (REPOSITORY / 'internal').rglob('*') if p.is_file())
    paths.update((REPOSITORY / 'tests/testsupport').rglob('*.go'))
    paths.update((REPOSITORY / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
    # Only the explicitly retained legacy metadata profile includes its
    # historical records. New profiles contain actual compiled/runtime inputs.
    paths.update(REPOSITORY / name for name in METADATA_INPUTS[selector])
    if selector in (TASK_REVIEW_UI, TASK_INTAKE_UI, TASK_EDIT_UI, TASK_TIMELINE_UI, AGENT_CONFIGURATION_UI, AGENT_EDIT_UI, AGENT_RUNNER_MOUNTS_UI, SYSTEM_RUNNER_MANAGEMENT_UI, AGENT_SECRET_ALLOWLIST_UI, AGENT_MODEL_POLICY_UI, SPRINT_CREATE_UI, PROJECT_VARIABLES_UI, MILESTONE_CREATE_UI, MILESTONE_EDIT_UI, SPRINT_EDIT_UI, SKILL_OWNER_UI, SKILL_PACKAGE_UI, PROJECT_SCHEDULER_UI, SPRINT_START_UI, KNOWLEDGE_TEXT_UI, KNOWLEDGE_TITLE_UI, KNOWLEDGE_DELETE_UI, KNOWLEDGE_RENAME_UI, PROJECT_SECRETS_UI, PROJECT_SECRET_WRITE_UI, PROJECT_USAGE_UI):
        paths.update(task_review_ui_inputs(selector))
    if selector == APP_RUNTIME:
        paths.update(app_runtime_inputs())
    if selector == '^TestRunnerReadFileApp$':
        paths.update(runner_read_file_app_inputs(binary)[0])
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p for p in paths):
        raise ValueError('regular complete configuration metadata inputs required')
    return sorted(paths)


def guard_inputs(binary):
    # Retain the fixed candidate's 683-source provenance, then add the
    # original seven-resource runtime closure and this exact entry control.
    paths = set(input_paths(binary)) | set((REPOSITORY / 'tests/projectvariable').glob('*.go'))
    paths.update((REPOSITORY / 'tests/testsupport').rglob('*.go'))
    paths.update((REPOSITORY / '.agent-state/project-variables-independent/commitproxy').glob('*.go'))
    paths.update(REPOSITORY / name for name in (
        'tests/projectvariable/project_phase_recovery_guard_test.go',
        'tests/projectvariable/project_phase_stop_fixture_test.go',
        'tests/projectvariable/project_lifecycle_fixture_test.go',
        '.agent-state/project-variable-lifecycle/guard-entry-controls.py'))
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p for p in paths):
        raise ValueError('regular complete lifecycle guard inputs required')
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


TASK_REVIEW_UI = '^TestTaskReviewWeb$'
TASK_INTAKE_UI = '^TestTaskIntakeWeb$'
TASK_EDIT_UI = '^TestTaskEditWeb$'
TASK_TIMELINE_UI = '^TestTaskTimelineWeb$'
AGENT_CONFIGURATION_UI = '^TestAgentConfigurationWeb$'
AGENT_EDIT_UI = '^TestAgentEditWeb$'
AGENT_RUNNER_MOUNTS_UI = '^TestAgentRunnerMountsWeb$'
SYSTEM_RUNNER_MANAGEMENT_UI = '^TestSystemRunnerManagementWeb$'
AGENT_SECRET_ALLOWLIST_UI = '^TestAgentSecretAllowlistWeb$'
AGENT_MODEL_POLICY_UI = '^TestAgentModelPolicyWeb$'
SPRINT_CREATE_UI = '^TestSprintCreateWeb$'
PROJECT_VARIABLES_UI = '^TestProjectVariablesWeb$'
MILESTONE_CREATE_UI = '^TestMilestoneCreateWeb$'
MILESTONE_EDIT_UI = '^TestMilestoneEditWeb$'
SPRINT_EDIT_UI = '^TestSprintEditWeb$'
SKILL_OWNER_UI = '^TestSkillOwnerReadWeb$'
SKILL_PACKAGE_UI = '^TestSkillPackageWeb$'
PROJECT_SCHEDULER_UI = '^TestProjectSchedulerWeb$'
SPRINT_START_UI = '^TestSprintStartWeb$'
KNOWLEDGE_TEXT_UI = '^TestKnowledgeTextWeb$'
KNOWLEDGE_TITLE_UI = '^TestKnowledgeTitleSearchWeb$'
KNOWLEDGE_DELETE_UI = '^TestKnowledgeDeleteWeb$'
PROJECT_SECRETS_UI = '^TestProjectSecretsWeb$'
PROJECT_SECRET_WRITE_UI = '^TestProjectSecretWriteWeb$'
PROJECT_USAGE_UI = '^TestProjectUsageWeb$'
KNOWLEDGE_RENAME_UI = '^TestKnowledgeOwnerRenameWeb$'
APP_RUNTIME = '^TestAppExecutionRuntime$'
TASK_INTAKE_ENV = ('AGENTEAM_TASK_INTAKE_WEB_DIST', 'AGENTEAM_TASK_INTAKE_WEB_EVIDENCE')
TASK_EDIT_ENV = ('AGENTEAM_TASK_EDIT_WEB_DIST', 'AGENTEAM_TASK_EDIT_WEB_EVIDENCE')
TASK_TIMELINE_ENV = ('AGENTEAM_TASK_TIMELINE_WEB_DIST', 'AGENTEAM_TASK_TIMELINE_WEB_EVIDENCE')
AGENT_CONFIGURATION_ENV = ('AGENTEAM_AGENT_CONFIGURATION_WEB_DIST', 'AGENTEAM_AGENT_CONFIGURATION_WEB_EVIDENCE')
AGENT_EDIT_ENV = ('AGENTEAM_AGENT_EDIT_WEB_DIST', 'AGENTEAM_AGENT_EDIT_WEB_EVIDENCE')
SYSTEM_RUNNER_MANAGEMENT_ENV = ('AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_DIST', 'AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE')
AGENT_SECRET_ALLOWLIST_ENV = ('AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_DIST', 'AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB_EVIDENCE')
AGENT_RUNNER_MOUNTS_ENV = ('AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_DIST', 'AGENTEAM_AGENT_RUNNER_MOUNTS_WEB_EVIDENCE')
AGENT_MODEL_POLICY_ENV = ('AGENTEAM_AGENT_MODEL_POLICY_WEB_DIST', 'AGENTEAM_AGENT_MODEL_POLICY_WEB_EVIDENCE')
PROJECT_VARIABLES_ENV = ('AGENTEAM_PROJECT_VARIABLES_WEB_DIST', 'AGENTEAM_PROJECT_VARIABLES_WEB_EVIDENCE')
MILESTONE_CREATE_ENV = ('AGENTEAM_MILESTONE_CREATE_WEB_DIST', 'AGENTEAM_MILESTONE_CREATE_WEB_EVIDENCE')
MILESTONE_EDIT_ENV = ('AGENTEAM_MILESTONE_EDIT_WEB_DIST', 'AGENTEAM_MILESTONE_EDIT_WEB_EVIDENCE')
SPRINT_EDIT_ENV = ('AGENTEAM_SPRINT_EDIT_WEB_DIST', 'AGENTEAM_SPRINT_EDIT_WEB_EVIDENCE')
SPRINT_CREATE_ENV = ('AGENTEAM_SPRINT_CREATE_WEB_DIST', 'AGENTEAM_SPRINT_CREATE_WEB_EVIDENCE')
SKILL_OWNER_ENV = ('AGENTEAM_SKILL_OWNER_WEB_DIST', 'AGENTEAM_SKILL_OWNER_WEB_EVIDENCE')
SKILL_PACKAGE_ENV = ('AGENTEAM_SKILL_PACKAGE_WEB_DIST', 'AGENTEAM_SKILL_PACKAGE_WEB_EVIDENCE')
PROJECT_SCHEDULER_ENV = ('AGENTEAM_PROJECT_SCHEDULER_WEB_DIST', 'AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE')
SPRINT_START_ENV = ('AGENTEAM_SPRINT_START_WEB_DIST', 'AGENTEAM_SPRINT_START_WEB_EVIDENCE')
KNOWLEDGE_TITLE_ENV = ('AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_DIST', 'AGENTEAM_KNOWLEDGE_TITLE_SEARCH_WEB_EVIDENCE')
PROJECT_SECRET_WRITE_ENV = ('AGENTEAM_PROJECT_SECRET_WRITE_WEB_DIST', 'AGENTEAM_PROJECT_SECRET_WRITE_WEB_EVIDENCE')
KNOWLEDGE_DELETE_ENV = ('AGENTEAM_KNOWLEDGE_DELETE_WEB_DIST', 'AGENTEAM_KNOWLEDGE_DELETE_WEB_EVIDENCE')
PROJECT_SECRETS_ENV = ('AGENTEAM_PROJECT_SECRETS_WEB_DIST', 'AGENTEAM_PROJECT_SECRETS_WEB_EVIDENCE')
PROJECT_USAGE_ENV = ('AGENTEAM_PROJECT_USAGE_WEB_DIST', 'AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE')
KNOWLEDGE_TEXT_ENV = ('AGENTEAM_KNOWLEDGE_TEXT_WEB_DIST', 'AGENTEAM_KNOWLEDGE_TEXT_WEB_EVIDENCE')
KNOWLEDGE_RENAME_ENV = tuple('AGENTEAM_KNOWLEDGE_OWNER_RENAME_WEB_' + part for part in ('DIST', 'EVIDENCE', 'SCHEMA_PYTHON', 'CASE'))
TASK_REVIEW_ENV = ('AGENTEAM_TASK_REVIEW_WEB_DIST', 'AGENTEAM_TASK_REVIEW_WEB_EVIDENCE')
# Each recovered worktree installs the existing locks privately; no historical
# author's physical tree is a runtime dependency.
TASK_REVIEW_HARNESS_MODULES = REPOSITORY / 'tests/account-captcha-web/node_modules'
TASK_REVIEW_WEB_MODULES = REPOSITORY / 'web/node_modules'


def task_review_ui_profile(selector):
    if selector == PROJECT_SCHEDULER_UI:
        return PROJECT_SCHEDULER_ENV, 'output/ai/project-scheduler-ui', 'project-scheduler'
    if selector == TASK_REVIEW_UI:
        return TASK_REVIEW_ENV, 'output/ai/work-review-web', 'task-review'
    if selector == TASK_INTAKE_UI:
        return TASK_INTAKE_ENV, 'output/ai/task-intake-web', 'task-intake'
    if selector == TASK_EDIT_UI:
        return TASK_EDIT_ENV, 'output/ai/task-edit-web', 'task-edit'
    if selector == TASK_TIMELINE_UI:
        return TASK_TIMELINE_ENV, 'output/ai/task-timeline-web', 'task-timeline'
    if selector == AGENT_CONFIGURATION_UI:
        return AGENT_CONFIGURATION_ENV, 'output/ai/agent-configuration-web', 'agent-configuration'
    if selector == AGENT_MODEL_POLICY_UI:
        return AGENT_MODEL_POLICY_ENV, 'output/ai/agent-model-policy-web', 'agent-model-policy'
    if selector == SYSTEM_RUNNER_MANAGEMENT_UI:
        return SYSTEM_RUNNER_MANAGEMENT_ENV, 'output/ai/system-runner-management-ui', 'system-runner-management'
    if selector == AGENT_SECRET_ALLOWLIST_UI:
        return AGENT_SECRET_ALLOWLIST_ENV, 'output/ai/agent-secret-allowlist-ui', 'agent-secret-allowlist'
    if selector == AGENT_RUNNER_MOUNTS_UI:
        return AGENT_RUNNER_MOUNTS_ENV, 'output/ai/agent-runner-mounts-ui', 'agent-runner-mounts'
    if selector == AGENT_EDIT_UI:
        return AGENT_EDIT_ENV, 'output/ai/agent-edit-web', 'agent-edit'
    if selector == PROJECT_VARIABLES_UI:
        return PROJECT_VARIABLES_ENV, 'output/ai/project-variables-web', 'project-variables'
    if selector == SPRINT_EDIT_UI:
        return SPRINT_EDIT_ENV, 'output/ai/sprint-edit-ui', 'sprint-edit'
    if selector == MILESTONE_EDIT_UI:
        return MILESTONE_EDIT_ENV, 'output/ai/milestone-edit-ui', 'milestone-edit'
    if selector == MILESTONE_CREATE_UI:
        return MILESTONE_CREATE_ENV, 'output/ai/milestone-create-web', 'milestone-create'
    if selector == SPRINT_CREATE_UI:
        return SPRINT_CREATE_ENV, 'output/ai/sprint-create-web', 'sprint-create'
    if selector == SKILL_PACKAGE_UI:
        return SKILL_PACKAGE_ENV, 'output/ai/skill-package-ui', 'skill-package'
    if selector == SKILL_OWNER_UI:
        return SKILL_OWNER_ENV, 'output/ai/skills-owner-ui', 'skill-owner-read'
    if selector == SPRINT_START_UI:
        return SPRINT_START_ENV, 'output/ai/sprint-start-web', 'sprint-start'
    if selector == PROJECT_USAGE_UI:
        return PROJECT_USAGE_ENV, 'output/ai/project-usage-read-ui', 'project-usage'
    if selector == PROJECT_SECRET_WRITE_UI:
        return PROJECT_SECRET_WRITE_ENV, 'output/ai/project-secrets-write-ui', 'project-secret-write'
    if selector == PROJECT_SECRETS_UI:
        return PROJECT_SECRETS_ENV, 'output/ai/project-secrets-read-ui', 'project-secrets'
    if selector == KNOWLEDGE_DELETE_UI:
        return KNOWLEDGE_DELETE_ENV, 'output/ai/knowledge-delete-ui', 'knowledge-delete'
    if selector == KNOWLEDGE_TITLE_UI:
        return KNOWLEDGE_TITLE_ENV, 'output/ai/knowledge-title-search-ui', 'knowledge-title-search'
    if selector == KNOWLEDGE_TEXT_UI:
        return KNOWLEDGE_TEXT_ENV, 'output/ai/knowledge-text-ui', 'knowledge-text'
    if selector == KNOWLEDGE_RENAME_UI:
        return KNOWLEDGE_RENAME_ENV, 'output/ai/knowledge-owner-rename', 'knowledge-owner-rename'
    raise ValueError('exact Task browser profile required')


def task_review_ui_environment(selector=TASK_REVIEW_UI):
    names, _, _ = task_review_ui_profile(selector)
    values = {key: os.environ.get(key, '') for key in names}
    if selector == KNOWLEDGE_RENAME_UI and (values[names[2]] != str(KNOWLEDGE_PYTHON) or values[names[3]] != 'rename'
            or not KNOWLEDGE_PYTHON.is_file() or not os.access(KNOWLEDGE_PYTHON, os.X_OK)):
        raise ValueError('exact Rename case and fixed Schema interpreter required')
    return values


def task_review_ui_assets(selector=TASK_REVIEW_UI):
    names, output, _ = task_review_ui_profile(selector)
    owned = (REPOSITORY / output).resolve()
    dist = Path(task_review_ui_environment(selector)[names[0]])
    if (not dist.is_absolute() or dist != dist.resolve() or not dist.is_relative_to(owned)
            or not (dist / 'index.html').is_file()):
        raise ValueError('owned frozen Task Review dist required')
    assets = list(dist.rglob('*'))
    if any(p.is_symlink() for p in assets):
        raise ValueError('Task Review assets must not alias another source')
    return sorted(p for p in assets if p.is_file())


def task_review_ui_configuration(directory, selector=TASK_REVIEW_UI):
    names, output, _ = task_review_ui_profile(selector)
    values = task_review_ui_environment(selector)
    task_review_ui_assets(selector)
    evidence = Path(values[names[1]])
    owned = (REPOSITORY / output).resolve()
    if (len(str(directory / 'runtime')) > 45 or not evidence.is_absolute()
            or evidence != evidence.resolve() or evidence.exists() or evidence.is_symlink()
            or not evidence.parent.is_dir() or not evidence.parent.is_relative_to(owned)
            or evidence.is_relative_to(Path(values[names[0]]))
            or not KNOWLEDGE_NODE.is_file() or not os.access(KNOWLEDGE_NODE, os.X_OK)):
        raise ValueError('fresh owned Task Review evidence and short runtime required')
    return values


def task_review_ui_inputs(selector=TASK_REVIEW_UI):
    _, _, stem = task_review_ui_profile(selector)
    paths = set(task_review_ui_assets(selector))
    harness = REPOSITORY / 'tests/account-captcha-web'
    modules = harness / 'node_modules'
    web_modules = REPOSITORY / 'web/node_modules'
    if (modules.resolve(strict=True) != TASK_REVIEW_HARNESS_MODULES.resolve(strict=True)
            or web_modules.resolve(strict=True) != TASK_REVIEW_WEB_MODULES.resolve(strict=True)):
        raise ValueError('approved shared Task Review installations required')
    paths.update(harness / name for name in ('package.json', 'package-lock.json',
        stem + '.config.js', 'e2e/' + stem + '.spec.ts',
        'e2e/' + stem + ('.native.ts' if selector == KNOWLEDGE_RENAME_UI else '.helpers.ts')))
    if selector == KNOWLEDGE_RENAME_UI:
        paths.add(KNOWLEDGE_PYTHON.resolve())
    for name in ('@playwright/test', 'playwright', 'playwright-core'):
        package = (modules / name).resolve(strict=True)
        if not package.is_relative_to(TASK_REVIEW_HARNESS_MODULES.resolve(strict=True)):
            raise ValueError('Task Review package escaped approved installation')
        if json.loads((package / 'package.json').read_text())['version'] != '1.56.1':
            raise ValueError('locked Playwright 1.56.1 required')
        paths.update(p for p in package.rglob('*') if p.is_file() or p.is_symlink())
    typescript = (web_modules / 'typescript').resolve(strict=True)
    if not typescript.is_relative_to(TASK_REVIEW_WEB_MODULES.resolve(strict=True)):
        raise ValueError('Task Review compiler escaped approved installation')
    paths.update(typescript / name for name in ('package.json', 'lib/typescript.js'))
    paths.update(p for p in (REPOSITORY / 'web/src').rglob('*')
                 if p.is_file() and '.spec.' not in p.name and '.test.' not in p.name)
    paths.update(REPOSITORY / 'web' / name for name in
                 ('package.json', 'package-lock.json', 'index.html', 'vite.config.ts'))
    paths.update((REPOSITORY / 'web').glob('tsconfig*.json'))
    paths.update({KNOWLEDGE_NODE, Path('/usr/bin/chromium'), Path('/usr/lib/chromium/chromium')})
    paths.update(p for p in Path('/etc/chromium.d').glob('*') if p.is_file() or p.is_symlink())
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p for p in paths):
        raise ValueError('regular complete Task Review browser inputs required')
    return sorted(paths)


def task_review_ui_input_hash(binary, selector=TASK_REVIEW_UI):
    inputs = {str(p): sha(p) for p in metadata_inputs(binary, selector)}
    return hashlib.sha256(json.dumps(inputs, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


APP_RUNTIME_UNSHARE = Path('/usr/bin/unshare')
APP_RUNTIME_PYTHON = KNOWLEDGE_PYTHON.resolve()
# Executed twice by the original PG owner: list, then the exact whole top. There
# is no child/fork or network namespace. Its PID becomes the real Go candidate.
APP_RUNTIME_WRAPPER = r"""import ctypes
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import stat
import sys
import tempfile


def main():
    args = sys.argv[1:]
    inside = args[:1] == ['--inside-private-dns']
    old = args[1:3] if inside else None
    if inside:
        args = args[3:]
    phase = ('list' if args == ['-test.list=^TestAppExecutionRuntime$'] else
             'run' if args == ['-test.v', '-test.count=1', '-test.timeout=6m',
                              '-test.run=^TestAppExecutionRuntime$'] else None)
    if phase is None or CANDIDATE.is_symlink() or not CANDIDATE.is_file():
        raise ValueError('exact candidate and original arguments required')
    with CANDIDATE.open('rb') as stream:
        if hashlib.file_digest(stream, 'sha256').hexdigest() != DIGEST:
            raise ValueError('candidate changed')
    current = [os.readlink('/proc/self/ns/' + name) for name in ('mnt', 'user')]
    if not inside:
        os.execv(UNSHARE, [UNSHARE, '--user', '--map-root-user', '--mount',
                          '--propagation', 'private', PYTHON, __file__,
                          '--inside-private-dns', *current, *args])
    if len(old) != 2 or any(a == b for a, b in zip(old, current)):
        raise ValueError('private namespaces required')
    descriptor = Path(os.environ['AGENTEAM_OUTBOUND_FIXTURE'])
    info = descriptor.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077
            or info.st_size > 16384):
        raise ValueError('owned descriptor required')
    value = json.loads(descriptor.read_text())
    if set(value) != {'ContainerID', 'NetworkID', 'Nonce', 'ControlPort', 'PrivateIP', 'CAFile'}:
        raise ValueError('exact outbound descriptor required')
    ip = ipaddress.IPv4Address(value['PrivateIP'])
    if not any(ip in ipaddress.IPv4Network(cidr) for cidr in
               ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16')):
        raise ValueError('private fixture address required')
    if not Path('/etc/resolv.conf').is_file():
        raise ValueError('fixed resolver configuration required')
    libc = ctypes.CDLL(None, use_errno=True)
    libc.mount.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_char_p,
                          ctypes.c_ulong, ctypes.c_void_p]
    libc.mount.restype = ctypes.c_int
    if libc.mount(None, b'/', None, (1 << 14) | (1 << 18), None) != 0:
        raise ValueError('private propagation required')
    fd, source = tempfile.mkstemp(prefix='app-dns-', dir=OWNED)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(b'nameserver ' + str(ip).encode('ascii') + b'\n')
        if libc.mount(os.fsencode(source), b'/etc/resolv.conf', None, 1 << 12, None) != 0:
            raise ValueError('private DNS bind failed')
    finally:
        os.unlink(source)
    # Descriptor ownership is independently revalidated by the real Go fixture.
    # This marker is observation only, not an authority or a business grant.
    os.environ['AGENTEAM_APP_RUNTIME_PRIVATE_DNS'] = '1'
    print('APP_RUNTIME private_dns phase=%s pid=%d candidate_sha=%s' %
          (phase, os.getpid(), DIGEST), file=sys.stderr, flush=True)
    os.execv(str(CANDIDATE), [str(CANDIDATE), *args])


try:
    main()
except (OSError, ValueError, KeyError, TypeError):
    print('STOP App runtime private DNS setup failed', file=sys.stderr)
    sys.exit(1)
"""


def app_runtime_inputs():
    paths = (APP_RUNTIME_UNSHARE, APP_RUNTIME_PYTHON)
    if any(not p.is_file() or p.is_symlink() or p.resolve(strict=True) != p
           or not os.access(p, os.X_OK) for p in paths):
        raise ValueError('fixed private namespace executables required')
    return paths


def app_runtime_wrapper(binary, directory):
    app_runtime_inputs()
    binary = Path(binary).resolve(strict=True)
    header = ('#!' + str(APP_RUNTIME_PYTHON) + '\n'
              'from pathlib import Path\n'
              'CANDIDATE = Path(' + repr(str(binary)) + ')\n'
              'DIGEST = ' + repr(sha(binary)) + '\n'
              'UNSHARE = ' + repr(str(APP_RUNTIME_UNSHARE)) + '\n'
              'PYTHON = ' + repr(str(APP_RUNTIME_PYTHON)) + '\n'
              'OWNED = ' + repr(str(directory / 'runtime')) + '\n')
    return header + APP_RUNTIME_WRAPPER


def prepare_app_runtime_wrapper(binary, directory):
    target = directory / 'app-runtime-candidate'
    source = app_runtime_wrapper(binary, directory)
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        stream.write(source)
    target.chmod(0o700)
    return target


def app_runtime_wrapper_same(binary, directory):
    target = directory / 'app-runtime-candidate'
    try:
        return (not target.is_symlink() and target.stat().st_mode & 0o777 == 0o700
                and target.read_text() == app_runtime_wrapper(binary, directory))
    except (OSError, ValueError):
        return False


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
    if selector == '^TestRunnerReadFileApp$':
        plan['runner_read_file_app'] = runner_read_file_app_inputs(binary)[1]
    if selector in (TASK_REVIEW_UI, TASK_INTAKE_UI, TASK_EDIT_UI, TASK_TIMELINE_UI, AGENT_CONFIGURATION_UI, AGENT_EDIT_UI, AGENT_RUNNER_MOUNTS_UI, SYSTEM_RUNNER_MANAGEMENT_UI, AGENT_SECRET_ALLOWLIST_UI, AGENT_MODEL_POLICY_UI, SPRINT_CREATE_UI, PROJECT_VARIABLES_UI, MILESTONE_CREATE_UI, MILESTONE_EDIT_UI, SPRINT_EDIT_UI, SKILL_OWNER_UI, SKILL_PACKAGE_UI, PROJECT_SCHEDULER_UI, SPRINT_START_UI, KNOWLEDGE_TEXT_UI, KNOWLEDGE_TITLE_UI, KNOWLEDGE_DELETE_UI, KNOWLEDGE_RENAME_UI, PROJECT_SECRETS_UI, PROJECT_SECRET_WRITE_UI, PROJECT_USAGE_UI):
        plan['task_review_ui'] = task_review_ui_configuration(directory, selector)
    if selector == APP_RUNTIME:
        app_runtime_inputs()
        plan['app_runtime_wrapper_sha'] = hashlib.sha256(app_runtime_wrapper(binary, directory).encode()).hexdigest()
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
    if args.run in METADATA_INPUTS:
        env.pop('AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD', None)
        prepare_history_go_environment(directory, env)
    if args.run == '^TestProjectLifecycleStopBatchRealGuard$':
        env.pop('AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD', None)
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
    if args.run in (TASK_REVIEW_UI, TASK_INTAKE_UI, TASK_EDIT_UI, TASK_TIMELINE_UI, AGENT_CONFIGURATION_UI, AGENT_EDIT_UI, AGENT_RUNNER_MOUNTS_UI, SYSTEM_RUNNER_MANAGEMENT_UI, AGENT_SECRET_ALLOWLIST_UI, AGENT_MODEL_POLICY_UI, SPRINT_CREATE_UI, PROJECT_VARIABLES_UI, MILESTONE_CREATE_UI, MILESTONE_EDIT_UI, SPRINT_EDIT_UI, SKILL_OWNER_UI, SKILL_PACKAGE_UI, PROJECT_SCHEDULER_UI, SPRINT_START_UI, KNOWLEDGE_TEXT_UI, KNOWLEDGE_TITLE_UI, KNOWLEDGE_DELETE_UI, KNOWLEDGE_RENAME_UI, PROJECT_SECRETS_UI, PROJECT_SECRET_WRITE_UI, PROJECT_USAGE_UI):
        ui = plan['task_review_ui']
        names, _, _ = task_review_ui_profile(args.run)
        Path(ui[names[1]]).mkdir(mode=0o700)
        env.update(ui)
        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),
                    names[0].removesuffix('_DIST') + '_INPUT_HASH': task_review_ui_input_hash(args.test_binary, args.run),
                    'PATH': str(KNOWLEDGE_NODE.parent) + os.pathsep + env.get('PATH', '')})
    if args.run == APP_RUNTIME:
        source = app_runtime_wrapper(args.test_binary, directory)
        if hashlib.sha256(source.encode()).hexdigest() != plan['app_runtime_wrapper_sha']:
            raise ValueError('private DNS wrapper inputs changed')
        env['AGENTEAM_FIXTURE_TEST_BINARY'] = str(prepare_app_runtime_wrapper(args.test_binary, directory))
    os.chdir(REPOSITORY)
    # No child is started here: the original shell chain replaces this PID.
    # Its nested Go Cmd.Run and shell wait remain the actual child owners.
    os.execve('/bin/sh', ['/bin/sh', 'scripts/test-objects.sh', '--run', args.run], env)


if __name__ == '__main__':
    sys.exit(main())
