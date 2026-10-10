import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectSecretsAPI, type SecretMetadata } from '../api/project-secrets'
import { type Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
export const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const time = '2026-10-10T12:00:00.123456Z',
  projectID = id(10),
  target = id(20)
export const base = `/api/v1/projects/${projectID}/secret-variables`
export const session = (): SessionView => ({
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
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
export const project = (): Project => ({
  id: projectID,
  owner_user_id: id(1),
  name: 'Demo',
  normalized_name: 'demo',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: null,
})
export const metadata = (): SecretMetadata => ({
  id: target,
  project_id: projectID,
  type: 'secret',
  name: 'TOKEN',
  description: 'Safe description',
  version: '1',
  created_at: time,
  updated_at: time,
})
export const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
export const problem = (status: number, code: string, commit_state = 'not_started') =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code,
      instance: base,
      request_id: id(99),
      commit_state,
    },
    status,
  )
export const receipt = (
  kind: 'create' | 'update' = 'create',
  value = metadata(),
  changed = true,
) => ({
  command: `project.secret_variable.${kind}`,
  changed,
  event_id: changed ? id(40) : null,
  audit_id: changed ? id(41) : null,
  variable: value,
})
export function barrier<T = void>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
export function fixture() {
  let view = session(),
    currentProject = project(),
    rows: SecretMetadata[] = [metadata()],
    last: unknown = null
  let intercept: Fetch | undefined
  const normal: Fetch = async (path, init) => {
    if (path === '/api/v1/session') return json(view)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${projectID}`)
      return json(currentProject)
    if (path.startsWith(base + '?')) return json({ items: rows })
    if (path === base + '/commands/lookup')
      return json(
        last ? { status: 'committed', receipt: last } : { status: 'not_observed', receipt: null },
      )
    if (path === base && init.method === 'POST') {
      const { request } = JSON.parse(init.body as string)
      const value = {
        ...metadata(),
        id: request.variable_id,
        name: request.name,
        description: request.description,
      }
      rows = [...rows, value].sort((a, b) => (a.name < b.name ? -1 : 1))
      last = receipt('create', value)
      return json(last)
    }
    if (path.startsWith(base + '/')) {
      const found = rows.find((row) => row.id === path.slice(base.length + 1))
      if (!found) return problem(404, 'NOT_FOUND')
      if (init.method === 'GET') return json(found)
      const body = JSON.parse(init.body as string)
      if (body.expected_version !== found.version)
        return problem(409, 'VERSION_CONFLICT', 'not_committed')
      if (init.method === 'DELETE') {
        rows = rows.filter((row) => row !== found)
        last = {
          command: 'project.secret_variable.delete',
          changed: true,
          event_id: id(40),
          audit_id: id(41),
          deleted: {
            id: found.id,
            project_id: projectID,
            type: 'secret',
            version: String(BigInt(found.version) + 1n),
            deleted_at: time,
          },
        }
        return json(last)
      }
      if (init.method === 'PATCH') {
        const changed =
          Object.hasOwn(body.request, 'value') ||
          Object.entries(body.request).some(
            ([key, value]) => found[key as keyof SecretMetadata] !== value,
          )
        const value = {
          ...found,
          ...(body.request.name === undefined ? {} : { name: body.request.name }),
          ...(body.request.description === undefined
            ? {}
            : { description: body.request.description }),
          version: String(BigInt(found.version) + (changed ? 1n : 0n)),
        }
        rows = rows
          .map((row) => (row === found ? value : row))
          .sort((a, b) => (a.name < b.name ? -1 : 1))
        last = receipt('update', value, changed)
        return json(last)
      }
    }
    throw new Error('Unexpected fixture endpoint')
  }
  const fetch = vi.fn<Fetch>((url, init) => (intercept ? intercept(url, init) : normal(url, init)))
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[16] = { secrets: createProjectSecretsAPI(fetch) }
  const auth = createSessionController(...args)
  return {
    auth,
    fetch,
    normal,
    intercept: (next?: Fetch) => {
      intercept = next
    },
    session: (next: SessionView) => {
      view = next
    },
    project: (next: Project) => {
      currentProject = next
    },
    rows: (next: SecretMetadata[]) => {
      rows = next
    },
  }
}
