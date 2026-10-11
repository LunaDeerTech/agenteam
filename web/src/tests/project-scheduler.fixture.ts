import { vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import {
  createProjectSchedulerAPI,
  type ProjectSchedulerConfiguration,
} from '../api/project-scheduler'
import type { Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { createProjectScheduler } from '../composables/useProjectScheduler'
export const schedulerID = (n: number) =>
  `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const schedulerBase = `/api/v1/projects/${schedulerID(10)}/scheduler`
export const schedulerLookup = `/api/v1/projects/${schedulerID(10)}/commands/lookup`
const time = '2026-10-11T00:00:00.123456Z'
export const schedulerSession: SessionView = {
  user: {
    id: schedulerID(1),
    email: 'owner@example.test',
    username: 'owner',
    display_name: 'Owner',
    role: 'user',
    theme: 'system',
    version: '1',
    initial_password_suggestion: false,
  },
  session: {
    id: schedulerID(2),
    issued_at: time,
    idle_expires_at: time,
    absolute_expires_at: time,
  },
  csrf_token: 'S'.repeat(43),
}
export function schedulerJSON(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status < 400 ? 'application/json' : 'application/problem+json',
      'X-Request-ID': schedulerID(99),
    },
  })
}
export function schedulerProblem(status: number, code: string, commit_state = 'not_started') {
  return schedulerJSON(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      status,
      detail: '',
      instance: '/api/v1',
      code,
      request_id: schedulerID(99),
      commit_state,
    },
    status,
  )
}
export function schedulerDeferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
export async function schedulerFixture(withPage = true) {
  let intercept: Fetch | undefined
  const configuration: { value: ProjectSchedulerConfiguration } = {
    value: {
      project_id: schedulerID(10),
      version: '1',
      scheduler_enabled: true,
      scheduler_max_concurrency: null,
    },
  }
  const project = (): Project => ({
    id: schedulerID(10),
    owner_user_id: schedulerID(1),
    name: 'Demo',
    normalized_name: 'demo',
    description: '',
    lifecycle: 'active',
    version: configuration.value.version,
    current_sprint_id: null,
    created_at: time,
    updated_at: time,
    archived_at: null,
  })
  const commands = new Map<string, Project>()
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return schedulerJSON(schedulerSession)
    if (path === '/api/v1/auth/bootstrap')
      return schedulerJSON({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (
      path.startsWith('/api/v1/projects/resolve?') ||
      path === `/api/v1/projects/${schedulerID(10)}`
    )
      return schedulerJSON(project())
    if (path === schedulerBase && init.method === 'GET') return schedulerJSON(configuration.value)
    if (path === schedulerBase && init.method === 'PATCH') {
      const body = JSON.parse(String(init.body)),
        key = new Headers(init.headers).get('Idempotency-Key')!
      const previous = commands.get(key)
      if (previous) return schedulerJSON({ project_id: previous.id, version: previous.version })
      if (body.expected_version !== configuration.value.version)
        return schedulerProblem(409, 'VERSION_CONFLICT', 'not_committed')
      const changed =
        body.scheduler_enabled !== configuration.value.scheduler_enabled ||
        body.scheduler_max_concurrency !== configuration.value.scheduler_max_concurrency
      configuration.value = {
        project_id: schedulerID(10),
        version: String(BigInt(configuration.value.version) + (changed ? 1n : 0n)),
        scheduler_enabled: body.scheduler_enabled,
        scheduler_max_concurrency: body.scheduler_max_concurrency,
      }
      commands.set(key, project())
      return schedulerJSON({ project_id: schedulerID(10), version: configuration.value.version })
    }
    if (path === schedulerLookup) {
      const previous = commands.get(new Headers(init.headers).get('Idempotency-Key')!)
      return schedulerJSON(
        previous
          ? { state: 'committed', result: { command: 'update', project: previous } }
          : { state: 'not_observed' },
      )
    }
    throw new Error('unexpected controlled scheduler request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = { scheduler: createProjectSchedulerAPI(fetch) }
  const auth = createSessionController(...args)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  const page = createProjectScheduler(auth, workspace)
  if (withPage) {
    workspace.afterNavigation('/owner/demo/settings/scheduler')
    page.afterNavigation('/owner/demo/settings/scheduler')
    await flushPromises()
  }
  return {
    auth,
    workspace,
    page,
    fetch,
    normal,
    configuration,
    commands,
    project,
    intercept(value?: Fetch) {
      intercept = value
    },
    async close() {
      page.dispose()
      workspace.dispose()
      auth.leave()
      await flushPromises()
    },
  }
}
