import { flushPromises } from '@vue/test-utils'
import { shallowRef } from 'vue'
import { vi } from 'vitest'
import { createAccountAPI, type SessionView } from '../api/account'
import { createKnowledgeDeleteAPI } from '../api/knowledge-delete'
import { createKnowledgeMoveAPI } from '../api/knowledge-move'
import { createKnowledgeCommandsAPI } from '../api/knowledge-commands'
import { createKnowledgeTextAPI } from '../api/knowledge-text'
import { createKnowledgeOwnerAPI, compareKnowledgeDocuments } from '../api/knowledge-owner'
import { createProjectOwnerAPI, type Project } from '../api/project-owner'
import { type Fetch } from '../api/client'
import { createSessionController } from '../composables/useSession'
import { createProjectWorkspace } from '../composables/useProjectWorkspace'
import { useKnowledgeOwner } from '../composables/useKnowledgeOwner'
import { useKnowledgeMove } from '../composables/useKnowledgeMove'
import {
  titleID as id,
  titleDocument as document,
  titleJSON as json,
} from './knowledge-title-search.fixture'
export { id, document, json }
export const moveBase = `/api/v1/projects/${id(1)}/knowledge/documents`
const time = '2026-10-11T12:00:00.123456Z'
export async function moveFixture() {
  let session: SessionView = {
    user: {
      id: id(2),
      email: 'owner@example.test',
      username: 'owner',
      display_name: 'Owner',
      role: 'user',
      theme: 'system',
      version: '1',
      initial_password_suggestion: false,
    },
    session: { id: id(3), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
  let project: Project = {
    id: id(1),
    owner_user_id: id(2),
    name: 'Move',
    normalized_name: 'move',
    description: '',
    lifecycle: 'active',
    version: '1',
    current_sprint_id: null,
    created_at: time,
    updated_at: time,
    archived_at: null,
  }
  const documents = new Map(
    [
      document(10, 'A 来源'),
      document(11, 'B 当前文档', id(10)),
      document(12, 'C 目标'),
      document(13, 'D 后代', id(11)),
      document(14, 'E 并发位置'),
    ].map((d) => [d.id, d]),
  )
  let intercept: Fetch | undefined
  const normal: Fetch = async (path) => {
    const url = new URL(path, 'https://owned.invalid')
    if (url.pathname === '/api/v1/session') return json(session)
    if (url.pathname === '/api/v1/projects/resolve' || url.pathname === `/api/v1/projects/${id(1)}`)
      return json(project)
    if (url.pathname === `${moveBase}/children`) {
      const parent = url.searchParams.get('parent_document_id')
      return json({
        items: [...documents.values()]
          .filter((d) => d.parent_document_id === (parent === 'null' ? null : parent))
          .sort(compareKnowledgeDocuments),
      })
    }
    const target = url.pathname.slice(moveBase.length + 1).split('/')[0]!,
      value = documents.get(target)!
    if (!value) throw new Error('Unexpected controlled Move request')
    if (url.pathname.endsWith('/ancestors')) {
      const items = []
      let parent = value.parent_document_id
      while (parent) {
        const d = documents.get(parent)!
        items.unshift(d)
        parent = d.parent_document_id
      }
      return json({ items })
    }
    if (url.pathname.endsWith('/content'))
      return json({
        document: value,
        text: { text: '原正文', next_byte_offset: '9', truncated: false },
      })
    return json({ active: value })
  }
  const fetch = vi.fn<Fetch>((path, init) =>
    intercept ? intercept(path, init) : normal(path, init),
  )
  const args: Parameters<typeof createSessionController> = [createAccountAPI(fetch)]
  args[12] = createProjectOwnerAPI(fetch)
  args[15] = createKnowledgeOwnerAPI(fetch)
  args[19] = {
    knowledgeMove: createKnowledgeMoveAPI(fetch),
    knowledgeDelete: createKnowledgeDeleteAPI(fetch),
    knowledgeCommands: createKnowledgeCommandsAPI(fetch),
    knowledgeText: createKnowledgeTextAPI(fetch),
  }
  const auth = createSessionController(...args)
  await auth.restore()
  const workspace = createProjectWorkspace(auth)
  workspace.afterNavigation('/owner/move/knowledge')
  await flushPromises()
  const location = shallowRef({ projectPath: '/owner/move', documentID: id(11) })
  const owner = useKnowledgeOwner(auth, workspace, location)
  await flushPromises()
  const editor = useKnowledgeMove(owner, workspace, auth)
  return {
    auth,
    workspace,
    owner,
    editor,
    location,
    documents,
    fetch,
    normal,
    intercept(value?: Fetch) {
      intercept = value
    },
    archive() {
      project = { ...project, lifecycle: 'archived', archived_at: time }
    },
    session(value: SessionView) {
      session = value
    },
    async dispose() {
      editor.dispose()
      owner.dispose()
      workspace.dispose()
      auth.leave()
      await flushPromises()
    },
  }
}
export function moveProblem(status: number, code: string) {
  return new Response(
    JSON.stringify({
      type: 'urn:agenteam:problem:test',
      title: 'Rejected',
      detail: '',
      status,
      code,
      instance: moveBase,
      request_id: id(99),
      commit_state: 'not_started',
    }),
    { status, headers: { 'Content-Type': 'application/problem+json', 'X-Request-ID': id(99) } },
  )
}
