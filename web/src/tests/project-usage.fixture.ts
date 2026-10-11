import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectUsageAPI, type ProjectUsageRecord } from '../api/project-usage'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'

export const usageID = (n: number) => `019a0000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const usageProject = usageID(1)
export const usageTime = '2026-10-11T12:00:00.123456Z'
export const usageNow = Date.parse('2026-10-11T13:00:00.000Z')
export const usageQuery = {
  from: '2026-09-11T13:00:00.000000Z',
  to: '2026-10-11T13:00:00.000000Z',
  limit: 50,
} as const
export const usageBase = `/api/v1/projects/${usageProject}/model-usage`
export const usagePath = '/owner/usage-demo/settings/model-usage'
export const usageRecord = (n = 100): ProjectUsageRecord => ({
  id: usageID(n),
  call_id: usageID(n + 1000),
  attempt_index: '9007199254740993',
  project_id: usageProject,
  consumer_kind: 'agent',
  purpose: 'agent_generation',
  agent_id: usageID(20),
  execution_id: usageID(21),
  meeting_id: null,
  provider_id: usageID(30),
  model_id: usageID(31),
  provider_name: '历史 Provider',
  model_name: '历史模型',
  provider_model_id: 'model-v1',
  protocol: 'openai-chat-completions',
  model_type: 'chat',
  live_provider_id: null,
  live_model_id: usageID(31),
  dispatch: 'sent',
  started_at: usageTime,
  dispatched_at: usageTime,
  final: { status: 'succeeded', finished_at: usageTime, error_category: null },
  usage: {
    input_tokens: '9007199254740993',
    output_tokens: '0',
    total_tokens: null,
    cached_input_tokens: null,
    cache_write_tokens: '9223372036854775807',
    reasoning_tokens: null,
    source: 'provider',
  },
  provider_request_id: 'safe-provider-request:1',
})
export const usagePage = (items = [usageRecord()], next_cursor: string | null = null) => ({
  items,
  next_cursor,
})
export const usageJSON = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': usageID(99) },
  })
export const usageProblem = (
  status: number,
  code = status === 503 ? 'DEPENDENCY_UNAVAILABLE' : 'FORBIDDEN',
) =>
  new Response(
    JSON.stringify({
      type: 'urn:agenteam:problem:test',
      title: 'Request rejected',
      detail: '',
      instance: '/api/v1',
      status,
      code,
      commit_state: 'not_started',
      request_id: usageID(99),
    }),
    {
      status,
      headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': usageID(99) },
    },
  )
export function usageDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
export function usageSessionFixture() {
  let session: SessionView = {
    user: {
      id: usageID(2),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: {
      id: usageID(3),
      issued_at: usageTime,
      idle_expires_at: usageTime,
      absolute_expires_at: usageTime,
    },
    csrf_token: 'S'.repeat(43),
  }
  let project: Project = {
    id: usageProject,
    owner_user_id: usageID(2),
    name: 'Usage-Demo',
    normalized_name: 'usage-demo',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: usageTime,
    updated_at: usageTime,
    archived_at: null,
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return usageJSON(session)
    if (
      url.pathname === '/api/v1/projects/resolve' ||
      url.pathname === `/api/v1/projects/${project.id}`
    )
      return usageJSON(project)
    if (url.pathname === `/api/v1/projects/${project.id}/model-usage`)
      return usageJSON(usagePage([{ ...usageRecord(), project_id: project.id }]))
    throw new Error('Unexpected controlled Usage metadata request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = { projectUsage: createProjectUsageAPI(fetch) }
  const auth = createSessionController(...args)
  return {
    auth,
    fetch,
    normal,
    intercept(value?: Fetch) {
      intercept = value
    },
    project(value: Project) {
      project = value
    },
    session(value: SessionView) {
      session = value
    },
    sessionValue: () => session,
  }
}
