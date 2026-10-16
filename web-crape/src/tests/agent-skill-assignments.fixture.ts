import { expect, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { computed, shallowReactive, shallowRef } from 'vue'
import { createAccountAPI, type SessionView } from '../api/account'
import { createAgentConfigurationAPI, type AgentConfiguration } from '../api/agent-configuration'
import { createAgentDirectoryAPI } from '../api/agent-directory'
import { createAgentRunnerMountsAPI } from '../api/agent-runner-mounts'
import { createSkillOwnerAPI } from '../api/skill-owner'
import {
  createAgentSkillAssignmentsAPI,
  type AgentSkillAssignment,
  type AgentSkillCommand,
  type AgentSkillMutation,
} from '../api/agent-skill-assignments'
import type { Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectAgents } from '../composables/useProjectAgents'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'

export const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const project = id(10),
  agent = id(20),
  skill = id(30),
  instant = '2026-10-13T00:00:00.000000Z'
export const base = `/api/v1/projects/${project}/agents/${agent}/skills`
export const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
export const problem = (code: string, status = 409, commit = 'not_committed') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code,
      instance: '/api/v1/projects/{project_id}/agents/{agent_id}/skills/assign',
      request_id: id(99),
      commit_state: commit,
    },
    status,
  )
export const assignment = (n = 30, enabled = true, sequence = '1'): AgentSkillAssignment => ({
  id: id(n + 1000),
  project_id: project,
  agent_id: agent,
  skill_id: id(n),
  enabled,
  assignment_sequence: sequence,
  created_at: instant,
})
export const metadata = (n = 30) => ({
  id: id(n),
  project_id: project,
  name: `Skill-${n}`,
  normalized_name: `skill-${n}`,
  description: '可分配技能',
  protected: false,
  current_revision: '1',
  version: '1',
})
export const command = (enabled = true): AgentSkillCommand => ({
  kind: 'assign',
  input: { expected_version: '1', request: { skill_id: skill, enabled } },
})
export function receipt(
  c = command(),
  changed = true,
  sequence = changed ? '2' : '1',
): AgentSkillMutation {
  return {
    project_id: project,
    agent_id: agent,
    agent_version: String(BigInt(c.input.expected_version) + (changed ? 1n : 0n)),
    assignment_sequence: sequence,
    change_id: id(50),
    changed,
    assignment:
      c.kind === 'remove'
        ? null
        : {
            ...assignment(30, c.input.request.enabled, sequence),
            id: id(3000 + Number(sequence)),
            skill_id: c.input.request.skill_id,
          },
    runtime:
      changed && c.kind === 'assign' && c.input.request.enabled
        ? { change_id: id(50), queued: false, execution_id: null }
        : null,
  }
}
const configuration = (): AgentConfiguration => ({
  id: agent,
  project_id: project,
  name: 'Skills-Agent',
  normalized_name: 'skills-agent',
  display_name: '技能助手',
  tag_color: null,
  description: '',
  instructions: '',
  inject_agents_md: true,
  model_ref: id(60),
  reasoning_effort: null,
  approval_policy: 'default',
  approval_model_ref: null,
  allowed_tool_ids: [],
  allowed_mount_ids: [],
  allowed_secret_variable_ids: [],
  lifecycle: 'active',
  version: '1',
  created_at: instant,
  updated_at: instant,
})
const session = (n = 2): SessionView => ({
  user: {
    id: id(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: {
    id: id(n),
    issued_at: instant,
    absolute_expires_at: instant,
    idle_expires_at: instant,
  },
  csrf_token: 'S'.repeat(43),
})

// Controlled wire responses exercise the real client and Session. They do not
// replace the independently tested transactional assignment writer.
export async function fixture() {
  const state = {
    config: configuration(),
    session: session(),
    sequence: '1',
    rows: [] as readonly AgentSkillAssignment[],
    candidates: [metadata()],
    last: null as AgentSkillMutation | null,
  }
  let intercept:
    | ((
        path: string,
        init: Parameters<Fetch>[1],
      ) => Promise<Response | undefined> | Response | undefined)
    | undefined
  const fetch = vi.fn<Fetch>(async (path, init) => {
    const override = await intercept?.(path, init)
    if (override) return override
    if (path === '/api/v1/session') return json(state.session)
    const url = new URL(path, 'https://example.test')
    if (url.pathname === `/api/v1/projects/${project}/agents` && init.method === 'GET') {
      const a = state.config
      return json({
        items: [
          {
            id: a.id,
            project_id: project,
            name: a.name,
            display_name: a.display_name,
            tag_color: a.tag_color,
            description: a.description,
            version: a.version,
            created_at: instant,
            updated_at: instant,
          },
        ],
      })
    }
    if (url.pathname.endsWith('/configuration') && init.method === 'GET') return json(state.config)
    if (url.pathname.endsWith('/runner-mounts'))
      return json({
        project_id: project,
        agent_id: agent,
        agent_version: state.config.version,
        items: [],
      })
    if (url.pathname === `/api/v1/projects/${project}/skills/catalog`)
      return json({ items: state.candidates })
    if (url.pathname === base && init.method === 'GET')
      return json({
        project_id: project,
        agent_id: agent,
        agent_version: state.config.version,
        assignment_sequence: state.sequence,
        items: state.rows,
      })
    if (path === `${base}/commands/lookup`)
      return json(
        state.last
          ? { state: 'committed', receipt: state.last }
          : { state: 'not_observed', receipt: null },
      )
    if (path === `${base}/assign` || path === `${base}/remove`) {
      const c = {
        kind: path.endsWith('/remove') ? 'remove' : 'assign',
        input: JSON.parse(init.body as string),
      } as AgentSkillCommand
      const seq = String(BigInt(state.sequence) + 1n)
      const result = receipt(c, true, seq)
      state.config = { ...state.config, version: result.agent_version }
      state.sequence = seq
      state.rows = result.assignment ? [result.assignment] : []
      state.last = result
      return json(result)
    }
    throw new Error(`Unexpected Skills fixture request: ${url.pathname}`)
  })
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[17] = createAgentDirectoryAPI(fetch)
  args[19] = {
    agentSkillAssignments: createAgentSkillAssignmentsAPI(fetch),
    agentConfiguration: createAgentConfigurationAPI(fetch),
    agentRunnerMounts: createAgentRunnerMountsAPI(fetch),
    skills: createSkillOwnerAPI(fetch),
  }
  const auth = createSessionController(...args)
  await auth.restore()
  return {
    auth,
    state,
    fetch,
    setIntercept(value: typeof intercept) {
      intercept = value
    },
    calls: (suffix: string) => fetch.mock.calls.filter(([path]) => path === base + suffix),
    dispose: () => auth.leave(),
    replaceSession: () => {
      state.session = session(3)
    },
  }
}
export async function pageFixture() {
  const f = await fixture()
  const context = shallowRef<{
    identity: NonNullable<typeof f.auth.personalContext.identity>
    projectID: string
    generation: number
    readGeneration: number
  } | null>({
    identity: f.auth.personalContext.identity!,
    projectID: project,
    generation: 1,
    readGeneration: 1,
  })
  const detail = shallowReactive({ project: { id: project, lifecycle: 'active' } })
  const workspace = {
    currentReadContext: context,
    detail,
    paths: computed(() => ({ home: '/owner/demo' })),
  } as unknown as ProjectWorkspace
  const owner = createProjectAgents(f.auth, workspace)
  owner.afterNavigation('/owner/demo/settings/agents')
  await flushPromises()
  await owner.selectAgent(agent)
  expect(owner.state.selectedID).toBe(agent)
  expect(owner.state.detailPhase, owner.state.detailMessage).toBe('ready')
  expect(owner.busy.value).toBe(false)
  return {
    ...f,
    owner,
    context,
    detail,
    dispose: () => {
      owner.dispose()
      f.dispose()
    },
  }
}
