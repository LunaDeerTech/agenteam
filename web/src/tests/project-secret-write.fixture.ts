import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createProjectSecretsAPI, type ProjectSecretMetadata } from '../api/project-secrets'
import {
  createProjectSecretCommandsAPI,
  type ProjectSecretReceipt,
} from '../api/project-secret-commands'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'
import {
  secretBase,
  secretID,
  secretJSON,
  secretMetadata,
  secretProject,
  secretTime,
  secretProblem,
} from './project-secrets.fixture'
export {
  secretBase,
  secretID,
  secretJSON,
  secretProject,
  secretTime,
  secretProblem,
  secretDeferred,
  secretPath,
} from './project-secrets.fixture'
export function secretReceipt(
  kind: 'create' | 'update',
  metadata: ProjectSecretMetadata,
  changed = true,
): ProjectSecretReceipt {
  return {
    command: `project.secret_variable.${kind}`,
    changed,
    variable: metadata,
    event_id: changed ? secretID(80) : null,
    audit_id: changed ? secretID(81) : null,
  }
}
export function secretWriteFixture() {
  const session: SessionView = {
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
  const project: Project = {
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
  let current: ProjectSecretMetadata = { ...secretMetadata(), version: '1' }
  let intercept: Fetch | undefined,
    receipt: ProjectSecretReceipt | null = null,
    cut = false
  const writes: { key: string; method: string; body: Record<string, any> }[] = []
  const lookups: { key: string; body: unknown }[] = []
  const normal: Fetch = async (path, init) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return secretJSON(session)
    if (
      url.pathname === '/api/v1/projects/resolve' ||
      url.pathname === `/api/v1/projects/${secretProject}`
    )
      return secretJSON(project)
    if (url.pathname === secretBase + '/commands/lookup') {
      lookups.push({
        key: new Headers(init.headers).get('Idempotency-Key')!,
        body: JSON.parse(String(init.body)),
      })
      return secretJSON(
        receipt ? { status: 'committed', receipt } : { status: 'not_observed', receipt: null },
      )
    }
    if (init.method === 'POST' || init.method === 'PATCH') {
      const body = JSON.parse(String(init.body)),
        create = init.method === 'POST'
      writes.push({
        key: new Headers(init.headers).get('Idempotency-Key')!,
        method: init.method,
        body,
      })
      if (!create && body.expected_version !== current.version)
        return secretProblem(409, 'VERSION_CONFLICT')
      const request = body.request
      const changed =
        create ||
        Object.hasOwn(request, 'value') ||
        ('name' in request && request.name !== current.name) ||
        ('description' in request && request.description !== current.description)
      current = {
        ...current,
        ...(create
          ? { id: request.variable_id, version: '1' }
          : { version: String(BigInt(current.version) + (changed ? 1n : 0n)) }),
        ...('name' in request ? { name: request.name } : {}),
        ...('description' in request ? { description: request.description } : {}),
      }
      receipt = secretReceipt(create ? 'create' : 'update', current, changed)
      if (cut) {
        cut = false
        throw new TypeError('Controlled response unavailable')
      }
      return secretJSON(receipt)
    }
    if (url.pathname === secretBase) return secretJSON({ items: [current] })
    if (url.pathname === secretBase + '/' + current.id) return secretJSON(current)
    throw new Error('Unexpected controlled Secret write route')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[19] = {
    projectSecrets: createProjectSecretsAPI(fetch),
    projectSecretCommands: createProjectSecretCommandsAPI(fetch),
  }
  const auth = createSessionController(...args)
  return {
    auth,
    fetch,
    normal,
    writes,
    lookups,
    session,
    project,
    get current() {
      return current
    },
    setCurrent(value: ProjectSecretMetadata) {
      current = value
    },
    cutNext() {
      cut = true
    },
    intercept(value?: Fetch) {
      intercept = value
    },
  }
}
