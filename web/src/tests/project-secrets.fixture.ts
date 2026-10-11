import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectSecretsAPI, type ProjectSecretMetadata } from '../api/project-secrets'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'

export const secretID = (n: number) => `019a0000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const secretProject = secretID(1)
export const secretTime = '2026-10-11T12:00:00.123456Z'
export const secretBase = `/api/v1/projects/${secretProject}/secret-variables`
export const secretPath = '/owner/secrets-demo/settings/secrets'
export const secretMetadata = (n = 10, name = 'SERVICE_TOKEN'): ProjectSecretMetadata => ({
  id: secretID(n),
  project_id: secretProject,
  type: 'secret',
  name,
  description: '供服务使用\n安全描述',
  version: '9007199254740993',
  created_at: secretTime,
  updated_at: secretTime,
})
export const secretJSON = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': secretID(99) },
  })
export const secretProblem = (
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
      request_id: secretID(99),
    }),
    {
      status,
      headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': secretID(99) },
    },
  )
export function secretDeferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
export function secretSessionFixture() {
  let session: SessionView = {
    user: {
      id: secretID(2),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: {
      id: secretID(3),
      issued_at: secretTime,
      idle_expires_at: secretTime,
      absolute_expires_at: secretTime,
    },
    csrf_token: 'S'.repeat(43),
  }
  let project: Project = {
    id: secretProject,
    owner_user_id: secretID(2),
    name: 'Secrets-Demo',
    normalized_name: 'secrets-demo',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: secretTime,
    updated_at: secretTime,
    archived_at: null,
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return secretJSON(session)
    if (
      url.pathname === '/api/v1/projects/resolve' ||
      url.pathname === `/api/v1/projects/${project.id}`
    )
      return secretJSON(project)
    if (url.pathname === `/api/v1/projects/${project.id}/secret-variables`)
      return secretJSON({ items: [{ ...secretMetadata(), project_id: project.id }] })
    if (url.pathname === `/api/v1/projects/${project.id}/secret-variables/${secretID(10)}`)
      return secretJSON({ ...secretMetadata(), project_id: project.id })
    throw new Error('Unexpected controlled Secret metadata request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = { projectSecrets: createProjectSecretsAPI(fetch) }
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
