import { createHash, webcrypto } from 'node:crypto'
import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createKnowledgeOwnerAPI } from '../api/knowledge-owner'
import { createKnowledgeDeleteAPI } from '../api/knowledge-delete'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'
export const id = (n: number) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const time = '2026-10-10T12:00:00.123456Z'
export const token = 'e30.e30.' + 'A'.repeat(43)
export const base = `/api/v1/projects/${id(10)}/knowledge/documents`
export const session: SessionView = {
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
}
export const project: Project = {
  id: id(10),
  owner_user_id: id(1),
  name: 'Delete',
  normalized_name: 'delete',
  description: '',
  lifecycle: 'active',
  version: '1',
  current_sprint_id: null,
  created_at: time,
  updated_at: time,
  archived_at: null,
}
export function row(n = 20, parent: string | null = n === 21 ? id(20) : null) {
  return {
    id: id(n),
    project_id: id(10),
    parent_document_id: parent,
    title: n === 20 ? '待删除根' : n === 21 ? '子文档' : '保留旁支',
    content_version: '9007199254740993',
    source_kind: 'text' as const,
    media_type: 'text/plain',
    status: 'active' as const,
    indexing_status: 'pending' as const,
    created_by: { kind: 'human' as const, user_id: id(1) },
    created_at: time,
    updated_at: time,
  }
}
export function preview() {
  const nodes = [row(), row(21)]
  const scope = {
    nodes: nodes.map((node) => ({
      content_version: node.content_version,
      id: node.id,
      parent_id: node.parent_document_id,
      status: 'active',
    })),
    project_id: id(10),
    root_id: id(20),
  }
  return {
    root_id: id(20),
    nodes,
    scope_digest:
      'sha256:' +
      createHash('sha256')
        .update('agenteam.knowledge.subtree.v1\0' + JSON.stringify(scope))
        .digest('hex'),
    confirmation_token: token,
    expires_at: '2099-10-10T12:00:00.123456Z',
  }
}
export const receipt = () => ({
  root_id: id(20),
  deleted_ids: [id(20), id(21)],
  cleanup_pending: true,
})
export const lookup = () => ({
  state: 'committed',
  receipt: { ...receipt(), command: 'delete-subtree', changed: true },
})
export const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(99),
    },
  })
export const problem = (status: number, code: string) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: '',
      detail: '',
      status,
      code,
      instance: base,
      request_id: id(99),
      commit_state: 'not_started',
    },
    status,
  )
export function fixture() {
  vi.stubGlobal('crypto', webcrypto)
  let deleted = false,
    intercept: Fetch | undefined,
    currentProject = project,
    currentSession = session
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://test.invalid')
    if (path === '/api/v1/session') return json(currentSession)
    if (path.startsWith('/api/v1/projects/resolve?') || path === `/api/v1/projects/${id(10)}`)
      return json(currentProject)
    if (url.pathname.endsWith('/delete-preview')) return json(preview())
    if (url.pathname.endsWith('/delete-subtree')) {
      deleted = true
      return json(receipt())
    }
    if (url.pathname.endsWith('/commands/lookup')) return json(lookup())
    if (url.pathname === `${base}/children`)
      return json({
        items:
          url.searchParams.get('parent_document_id') === 'null'
            ? deleted
              ? [row(22)]
              : [row(), row(22)]
            : [row(21)],
      })
    const n = url.pathname.includes(id(22)) ? 22 : url.pathname.includes(id(21)) ? 21 : 20
    if (url.pathname.endsWith('/ancestors')) return json({ items: n === 21 ? [row()] : [] })
    if (url.pathname.endsWith('/content'))
      return json({
        document: row(n),
        text: { text: '正文', next_byte_offset: '6', truncated: false },
      })
    if (deleted && n !== 22)
      return json({
        deleted: {
          id: id(n),
          project_id: id(10),
          content_version: row(n).content_version,
          deleted_at: time,
        },
      })
    return json({ active: row(n) })
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  args[19] = {
    knowledgeDelete: createKnowledgeDeleteAPI(fetch),
    knowledgeCommands: createKnowledgeCommandsAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }
  return {
    auth: createSessionController(...args),
    fetch,
    normal,
    intercept(value: Fetch | undefined) {
      intercept = value
    },
    setDeleted() {
      deleted = true
    },
    setProject(value: Project) {
      currentProject = value
    },
    setSession(value: SessionView) {
      currentSession = value
    },
  }
}
