import {
  runnerMountDeclarations,
  runnerMountReceipt,
  type RunnerMountDeclaration,
  type RunnerMountBinding,
} from './agent-runner-mounts'
import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import type { WriteOptions } from './account'
import { parseSystemInstant } from './system-account'
import { workID, workText, workVersion } from './work-review'

export type AgentApprovalPolicy = 'default' | 'auto' | 'allow'
export type AgentCreateFields = Readonly<{
  agent_id: string
  name: string
  model_ref: string
  inject_agents_md: boolean
  approval_policy: AgentApprovalPolicy
  allowed_tool_ids: readonly string[]
  allowed_mount_ids?: readonly string[]
  runner_mounts?: readonly RunnerMountDeclaration[]
  allowed_secret_variable_ids: readonly string[]
  display_name?: string | null
  tag_color?: string | null
  description?: string
  instructions?: string
  reasoning_effort?: string | null
  approval_model_ref?: string | null
  add_skills_enabled?: boolean
  install_skill_enabled?: boolean
}>
export type AgentUpdateFields = Readonly<{
  runner_mounts?: readonly RunnerMountDeclaration[]
  allowed_secret_variable_ids?: readonly string[]
  name?: string
  display_name?: string | null
  tag_color?: string | null
  description?: string
  instructions?: string
  model_ref?: string
  reasoning_effort?: null
  inject_agents_md?: boolean
  approval_policy?: AgentApprovalPolicy
  approval_model_ref?: string | null
}>
export type AgentUpdateInput = Readonly<{ expected_version: string; request: AgentUpdateFields }>
export type AgentUpdateReceipt = Readonly<{
  runner_mounts?: readonly RunnerMountBinding[]
  agent: AgentConfiguration
  changed: boolean
  event_ids: readonly string[]
}>
export type AgentUpdateLookup =
  | Readonly<{ status: 'committed'; receipt: AgentUpdateReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export type AgentCreateInput = Readonly<{ request: AgentCreateFields }>
export type AgentConfiguration = Readonly<{
  id: string
  project_id: string
  name: string
  normalized_name: string
  display_name: string | null
  tag_color: string | null
  description: string
  instructions: string
  inject_agents_md: boolean
  model_ref: string
  reasoning_effort: string | null
  approval_policy: AgentApprovalPolicy
  approval_model_ref: string | null
  allowed_tool_ids: readonly string[]
  allowed_mount_ids: readonly string[]
  allowed_secret_variable_ids: readonly string[]
  lifecycle: 'active'
  version: string
  created_at: string
  updated_at: string
}>
export type AgentCreateReceipt = Readonly<{
  runner_mounts?: readonly RunnerMountBinding[]
  agent: AgentConfiguration
  changed: true
  event_ids: readonly string[]
}>
export type AgentCreateLookup =
  | Readonly<{ status: 'committed'; receipt: AgentCreateReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface AgentConfigurationAPI {
  update(
    project: string,
    agent: string,
    input: AgentUpdateInput,
    write: WriteOptions,
  ): Promise<AgentUpdateReceipt>
  lookupUpdate(
    project: string,
    agent: string,
    input: AgentUpdateInput,
    write: WriteOptions,
  ): Promise<AgentUpdateLookup>
  create(project: string, input: AgentCreateInput, write: WriteOptions): Promise<AgentCreateReceipt>
  getConfiguration(project: string, agent: string, signal: AbortSignal): Promise<AgentConfiguration>
  lookup(project: string, input: AgentCreateInput, write: WriteOptions): Promise<AgentCreateLookup>
}

function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(fn: () => T): T {
  try {
    return fn()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function bool(value: unknown): boolean {
  requireValue(typeof value === 'boolean')
  return value
}
function nullable<T>(value: unknown, parse: (value: unknown) => T): T | null {
  return value === null ? null : parse(value)
}
function name(value: unknown): string {
  const result = workText(value, 32)
  requireValue(/^[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]$/.test(result))
  requireValue(
    ![
      'api',
      'assets',
      'auth',
      'login',
      'logout',
      'invite',
      'reset',
      'settings',
      'system',
      'personal',
      'diagnostics',
      'livez',
      'readyz',
      'debug',
      'support',
      'root',
      'admin',
    ].includes(result.toLowerCase()),
  )
  return result
}
function color(value: unknown): string {
  const result = workText(value, 7)
  requireValue(/^#[0-9a-f]{6}$/.test(result))
  return result
}
function effort(value: unknown): string {
  const result = workText(value, 32)
  requireValue(/^[A-Za-z0-9_.:-]{1,32}$/.test(result))
  return result
}
function approval(value: unknown): AgentApprovalPolicy {
  requireValue(value === 'default' || value === 'auto' || value === 'allow')
  return value
}
function references(value: unknown): readonly string[] {
  requireValue(Array.isArray(value) && value.length <= 128)
  const result = Array.from(value, workID)
  for (let n = 1; n < result.length; n++) requireValue(result[n - 1]! < result[n]!)
  return Object.freeze(result)
}
function relations(
  value: Pick<
    AgentCreateFields,
    | 'approval_policy'
    | 'approval_model_ref'
    | 'allowed_tool_ids'
    | 'allowed_mount_ids'
    | 'runner_mounts'
    | 'allowed_secret_variable_ids'
  >,
) {
  requireValue((value.approval_policy === 'auto') === (value.approval_model_ref != null))
  requireValue(
    value.allowed_tool_ids.length +
      (value.runner_mounts?.length ?? value.allowed_mount_ids?.length ?? 0) +
      value.allowed_secret_variable_ids.length <=
      256,
  )
}

// Capture owns all arrays and preserves omission, explicit null and false.
// It neither invents an Agent ID nor expands the server's initialization defaults.
export function captureAgentCreateInput(value: unknown): AgentCreateInput {
  return input(() => {
    const outer = shape(value, ['request'])
    const v = shape(
      outer.request,
      [
        'agent_id',
        'name',
        'model_ref',
        'inject_agents_md',
        'approval_policy',
        'allowed_tool_ids',
        'allowed_secret_variable_ids',
      ],
      [
        'allowed_mount_ids',
        'runner_mounts',
        'display_name',
        'tag_color',
        'description',
        'instructions',
        'reasoning_effort',
        'approval_model_ref',
        'add_skills_enabled',
        'install_skill_enabled',
      ],
    )
    requireValue(Object.hasOwn(v, 'runner_mounts') !== Object.hasOwn(v, 'allowed_mount_ids'))
    const request: AgentCreateFields = Object.freeze({
      agent_id: workID(v.agent_id),
      name: name(v.name),
      model_ref: workID(v.model_ref),
      inject_agents_md: bool(v.inject_agents_md),
      approval_policy: approval(v.approval_policy),
      allowed_tool_ids: references(v.allowed_tool_ids),
      ...(Object.hasOwn(v, 'runner_mounts')
        ? { runner_mounts: runnerMountDeclarations(v.runner_mounts) }
        : { allowed_mount_ids: references(v.allowed_mount_ids) }),
      allowed_secret_variable_ids: references(v.allowed_secret_variable_ids),
      ...(Object.hasOwn(v, 'display_name')
        ? { display_name: nullable(v.display_name, (x) => workText(x, 1024, true)) }
        : {}),
      ...(Object.hasOwn(v, 'tag_color') ? { tag_color: nullable(v.tag_color, color) } : {}),
      ...(Object.hasOwn(v, 'description') ? { description: workText(v.description, 8192) } : {}),
      ...(Object.hasOwn(v, 'instructions')
        ? { instructions: workText(v.instructions, 32768) }
        : {}),
      ...(Object.hasOwn(v, 'reasoning_effort')
        ? { reasoning_effort: nullable(v.reasoning_effort, effort) }
        : {}),
      ...(Object.hasOwn(v, 'approval_model_ref')
        ? { approval_model_ref: nullable(v.approval_model_ref, workID) }
        : {}),
      ...(Object.hasOwn(v, 'add_skills_enabled')
        ? { add_skills_enabled: bool(v.add_skills_enabled) }
        : {}),
      ...(Object.hasOwn(v, 'install_skill_enabled')
        ? { install_skill_enabled: bool(v.install_skill_enabled) }
        : {}),
    })
    relations(request)
    return Object.freeze({ request })
  })
}

// Omission keeps a field; null clears display metadata and empty text clears long text.
export function captureAgentUpdateInput(value: unknown): AgentUpdateInput {
  return input(() => {
    const outer = shape(value, ['expected_version', 'request'])
    const v = shape(
      outer.request,
      [],
      [
        'runner_mounts',
        'allowed_secret_variable_ids',
        'name',
        'display_name',
        'tag_color',
        'description',
        'instructions',
        'model_ref',
        'reasoning_effort',
        'inject_agents_md',
        'approval_policy',
        'approval_model_ref',
      ],
    )
    requireValue(Object.keys(v).length > 0)
    if (Object.hasOwn(v, 'reasoning_effort')) requireValue(v.reasoning_effort === null)
    // Partial updates retain omitted fields. Reject contradictory explicit pairs;
    // the backend revalidates the complete postimage and current model references.
    if (Object.hasOwn(v, 'approval_policy') && Object.hasOwn(v, 'approval_model_ref'))
      requireValue((v.approval_policy === 'auto') === (v.approval_model_ref !== null))
    return Object.freeze({
      expected_version: workVersion(outer.expected_version),
      request: Object.freeze({
        ...(Object.hasOwn(v, 'runner_mounts')
          ? { runner_mounts: runnerMountDeclarations(v.runner_mounts) }
          : {}),
        ...(Object.hasOwn(v, 'allowed_secret_variable_ids')
          ? { allowed_secret_variable_ids: references(v.allowed_secret_variable_ids) }
          : {}),
        ...(Object.hasOwn(v, 'name') ? { name: name(v.name) } : {}),
        ...(Object.hasOwn(v, 'display_name')
          ? { display_name: nullable(v.display_name, (x) => workText(x, 1024, true)) }
          : {}),
        ...(Object.hasOwn(v, 'tag_color') ? { tag_color: nullable(v.tag_color, color) } : {}),
        ...(Object.hasOwn(v, 'description') ? { description: workText(v.description, 8192) } : {}),
        ...(Object.hasOwn(v, 'instructions')
          ? { instructions: workText(v.instructions, 32768) }
          : {}),
        ...(Object.hasOwn(v, 'model_ref') ? { model_ref: workID(v.model_ref) } : {}),
        ...(Object.hasOwn(v, 'reasoning_effort') ? { reasoning_effort: null } : {}),
        ...(Object.hasOwn(v, 'inject_agents_md')
          ? { inject_agents_md: bool(v.inject_agents_md) }
          : {}),
        ...(Object.hasOwn(v, 'approval_policy')
          ? { approval_policy: approval(v.approval_policy) }
          : {}),
        ...(Object.hasOwn(v, 'approval_model_ref')
          ? { approval_model_ref: nullable(v.approval_model_ref, workID) }
          : {}),
      }),
    })
  })
}
function updateReceipt(
  value: unknown,
  project: string,
  target: string,
  original: AgentUpdateInput,
): AgentUpdateReceipt {
  const v = shape(value, ['agent', 'changed', 'event_ids'], ['runner_mounts'])
  const agent = configuration(v.agent, project, target),
    changed = bool(v.changed)
  const version = BigInt(original.expected_version) + (changed ? 1n : 0n)
  requireValue(version <= 9223372036854775807n && agent.version === String(version))
  for (const key of [
    'name',
    'display_name',
    'tag_color',
    'description',
    'instructions',
    'model_ref',
    'reasoning_effort',
    'inject_agents_md',
    'approval_policy',
    'approval_model_ref',
  ] as const)
    if (Object.hasOwn(original.request, key)) requireValue(agent[key] === original.request[key])
  if (original.request.allowed_secret_variable_ids !== undefined)
    requireValue(
      sameIDs(agent.allowed_secret_variable_ids, original.request.allowed_secret_variable_ids),
    )
  requireValue(Array.isArray(v.event_ids) && v.event_ids.length === (changed ? 1 : 0))
  const bindings = receiptMounts(v, original.request, agent)
  return Object.freeze({
    agent,
    changed,
    event_ids: Object.freeze(v.event_ids.map(workID)),
    ...bindings,
  })
}

function configuration(value: unknown, project: string, target: string): AgentConfiguration {
  const v = shape(value, [
    'id',
    'project_id',
    'name',
    'normalized_name',
    'display_name',
    'tag_color',
    'description',
    'instructions',
    'inject_agents_md',
    'model_ref',
    'reasoning_effort',
    'approval_policy',
    'approval_model_ref',
    'allowed_tool_ids',
    'allowed_mount_ids',
    'allowed_secret_variable_ids',
    'lifecycle',
    'version',
    'created_at',
    'updated_at',
  ])
  requireValue(v.lifecycle === 'active')
  const result: AgentConfiguration = Object.freeze({
    id: workID(v.id),
    project_id: workID(v.project_id),
    name: name(v.name),
    normalized_name: workText(v.normalized_name, 32),
    display_name: nullable(v.display_name, (x) => workText(x, 1024, true)),
    tag_color: nullable(v.tag_color, color),
    description: workText(v.description, 8192),
    instructions: workText(v.instructions, 32768),
    inject_agents_md: bool(v.inject_agents_md),
    model_ref: workID(v.model_ref),
    reasoning_effort: nullable(v.reasoning_effort, effort),
    approval_policy: approval(v.approval_policy),
    approval_model_ref: nullable(v.approval_model_ref, workID),
    allowed_tool_ids: references(v.allowed_tool_ids),
    allowed_mount_ids: references(v.allowed_mount_ids),
    allowed_secret_variable_ids: references(v.allowed_secret_variable_ids),
    lifecycle: 'active',
    version: workVersion(v.version),
    created_at: parseSystemInstant(v.created_at),
    updated_at: parseSystemInstant(v.updated_at),
  })
  requireValue(
    result.project_id === project &&
      result.id === target &&
      result.normalized_name === result.name.toLowerCase() &&
      result.created_at <= result.updated_at,
  )
  relations(result)
  return result
}
function sameIDs(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((value, n) => value === b[n])
}
function receipt(value: unknown, project: string, original: AgentCreateInput): AgentCreateReceipt {
  const v = shape(value, ['agent', 'changed', 'event_ids'], ['runner_mounts']),
    r = original.request
  const agent = configuration(v.agent, project, r.agent_id)
  requireValue(v.changed === true && agent.version === '1' && agent.created_at === agent.updated_at)
  requireValue(
    agent.name === r.name &&
      agent.display_name === (r.display_name ?? null) &&
      agent.tag_color === (r.tag_color ?? null) &&
      agent.description === (r.description ?? '') &&
      agent.instructions === (r.instructions ?? '') &&
      agent.inject_agents_md === r.inject_agents_md &&
      agent.model_ref === r.model_ref &&
      agent.reasoning_effort === (r.reasoning_effort ?? null) &&
      agent.approval_policy === r.approval_policy &&
      agent.approval_model_ref === (r.approval_model_ref ?? null),
  )
  requireValue(
    (r.runner_mounts !== undefined || sameIDs(agent.allowed_mount_ids, r.allowed_mount_ids!)) &&
      sameIDs(agent.allowed_secret_variable_ids, r.allowed_secret_variable_ids),
  )
  if (r.install_skill_enabled ?? true) {
    // Registry owns the default Tool identity. It can already be explicitly
    // selected; accept only the original set plus at most that single Tool.
    requireValue(
      agent.allowed_tool_ids.length >= Math.max(1, r.allowed_tool_ids.length) &&
        agent.allowed_tool_ids.length <= r.allowed_tool_ids.length + 1 &&
        r.allowed_tool_ids.every((id) => agent.allowed_tool_ids.includes(id)),
    )
  } else requireValue(sameIDs(agent.allowed_tool_ids, r.allowed_tool_ids))
  requireValue(Array.isArray(v.event_ids) && v.event_ids.length === 1)
  const bindings = receiptMounts(v, r, agent)
  return Object.freeze({
    agent,
    changed: true,
    event_ids: Object.freeze(v.event_ids.map(workID)),
    ...bindings,
  })
}

function receiptMounts(
  value: Record<string, unknown>,
  original: Readonly<{ runner_mounts?: readonly RunnerMountDeclaration[] }>,
  agent: AgentConfiguration,
) {
  requireValue(Object.hasOwn(value, 'runner_mounts') === Object.hasOwn(original, 'runner_mounts'))
  return original.runner_mounts === undefined
    ? {}
    : {
        runner_mounts: runnerMountReceipt(
          value.runner_mounts,
          original.runner_mounts,
          agent.allowed_mount_ids,
        ),
      }
}

export function createAgentConfigurationAPI(fetcher?: Fetch): AgentConfigurationAPI {
  const call = accountTransport(fetcher)
  return {
    update(project, target, value, write) {
      const original = captureAgentUpdateInput(value)
      return call('agentConfigurationUpdate', (v) => updateReceipt(v, project, target, original), {
        projectID: input(() => workID(project)),
        target: input(() => workID(target)),
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookupUpdate(project, target, value, write) {
      const original = captureAgentUpdateInput(value)
      input(() => workID(target))
      return call(
        'agentConfigurationLookup',
        (value): AgentUpdateLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: updateReceipt(v.receipt, project, target, original),
            })
          requireValue(
            (v.status === 'in_progress' || v.status === 'not_observed') && v.receipt === null,
          )
          return Object.freeze({
            status: v.status as 'in_progress' | 'not_observed',
            receipt: null,
          })
        },
        {
          projectID: input(() => workID(project)),
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'agent.update', target_id: target, ...original },
        },
      )
    },
    create(project, value, write) {
      const original = captureAgentCreateInput(value)
      return call('agentConfigurationCreate', (v) => receipt(v, project, original), {
        projectID: input(() => workID(project)),
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    getConfiguration(project, target, signal) {
      input(() => {
        workID(project)
        workID(target)
      })
      return call('agentConfigurationGet', (v) => configuration(v, project, target), {
        projectID: project,
        target,
        signal,
      })
    },
    lookup(project, value, write) {
      const original = captureAgentCreateInput(value)
      return call(
        'agentConfigurationLookup',
        (value): AgentCreateLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: receipt(v.receipt, project, original),
            })
          requireValue(
            (v.status === 'in_progress' || v.status === 'not_observed') && v.receipt === null,
          )
          return Object.freeze({
            status: v.status as 'in_progress' | 'not_observed',
            receipt: null,
          })
        },
        {
          projectID: input(() => workID(project)),
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'agent.create', ...original },
        },
      )
    },
  }
}
