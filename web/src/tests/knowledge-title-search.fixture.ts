import type { KnowledgeDocument } from '../api/knowledge-owner'
import { createKnowledgeOwnerAPI } from '../api/knowledge-owner'
import { createKnowledgeTitleSearchAPI } from '../api/knowledge-title-search'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
import { createAccountAPI, type SessionView } from '../api/account'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { createSessionController } from '../composables/useSession'
import type { Fetch } from '../api/client'
import { vi } from 'vitest'
export const titleID = (n: number) => `019a0000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
export const titleProject = titleID(1)
export const titleDocument = (
  n = 10,
  title = '说明 %_! 中文',
  parent: string | null = null,
): KnowledgeDocument => ({
  id: titleID(n),
  project_id: titleProject,
  parent_document_id: parent,
  title,
  content_version: '9007199254740993',
  source_kind: 'text',
  media_type: 'text/markdown',
  status: 'active',
  indexing_status: 'pending',
  created_by: { kind: 'human', user_id: titleID(2) },
  created_at: '2026-10-11T12:00:00.123456Z',
  updated_at: '2026-10-11T12:00:00.123456Z',
})
export function titleDeferred<T>() {
  let resolve!: (value: T) => void, reject!: (value: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const time = '2026-10-11T12:00:00.123456Z'
export const titleBase = `/api/v1/projects/${titleProject}/knowledge/documents`
export const titleRoot = () => titleDocument(10, '父文档')
export const titleChild = () => titleDocument(11, '子 %_! 文档', titleID(10))
export const titleJSON = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': titleID(99) },
  })
export function titleSessionFixture() {
  let session: SessionView = {
    user: {
      id: titleID(2),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: { id: titleID(3), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
  let project: Project = {
    id: titleProject,
    owner_user_id: titleID(2),
    name: 'Title-Search',
    normalized_name: 'title-search',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: time,
    updated_at: time,
    archived_at: null,
  }
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return titleJSON(session)
    if (
      url.pathname === '/api/v1/projects/resolve' ||
      url.pathname === `/api/v1/projects/${project.id}`
    )
      return titleJSON(project)
    if (url.pathname === `${titleBase}/children`)
      return titleJSON({
        items:
          url.searchParams.get('parent_document_id') === 'null' ? [titleRoot()] : [titleChild()],
      })
    if (url.pathname === `${titleBase}/search-titles`)
      return titleJSON({
        items: titleChild().title.includes(url.searchParams.get('title_query')!)
          ? [{ document: titleChild(), ancestors: [titleRoot()] }]
          : [],
      })
    const document = url.pathname.includes(titleID(11)) ? titleChild() : titleRoot()
    if (url.pathname.endsWith('/ancestors'))
      return titleJSON({ items: document.parent_document_id ? [titleRoot()] : [] })
    if (url.pathname.endsWith('/content')) {
      const text = document.id === titleID(11) ? '当前子文档正文 %_!' : '当前父文档正文'
      return titleJSON({
        document,
        text: {
          text,
          next_byte_offset: String(new TextEncoder().encode(text).byteLength),
          truncated: false,
        },
      })
    }
    if (url.pathname === `${titleBase}/${document.id}`) return titleJSON({ active: document })
    throw new Error('Unexpected controlled title-search request')
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  args[19] = {
    knowledgeTitleSearch: createKnowledgeTitleSearchAPI(fetch),
    knowledgeCommands: createKnowledgeCommandsAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }
  const auth = createSessionController(...args)
  return {
    auth,
    fetch,
    normal,
    intercept(value?: Fetch) {
      intercept = value
    },
    session(value: SessionView) {
      session = value
    },
    project(value: Project) {
      project = value
    },
  }
}
