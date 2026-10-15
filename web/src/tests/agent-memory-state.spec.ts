import { describe, expect, it, vi } from 'vitest'
import { effectScope, ref, shallowRef } from 'vue'
import { useAgentMemory } from '../composables/useAgentMemory'
import { AccountFailure } from '../api/client'
import type { MemoryPage } from '../api/agent-memory'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import type { SessionController } from '../composables/useSession'
const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const record = {
  namespace: { project_id: id(1), agent_id: id(2) },
  updated_at: '2026-10-14T00:00:00.000000Z',
  current: {
    memory_id: id(3),
    revision: '1',
    status: 'active' as const,
    type: 'fact' as const,
    importance: 'normal' as const,
    text: 'A private memory',
    tags: [],
    created_at: '2026-10-14T00:00:00.000000Z',
    provenance: { execution_id: id(4), operation_id: id(5), derived_from_memory_ids: [] },
  },
}
function setup(list: () => Promise<MemoryPage>) {
  const agent = ref<string | null>(id(2)),
    context = shallowRef({
      projectID: id(1),
      identity: { userID: id(6), sessionID: id(7), epoch: 1 },
      generation: 1,
      readGeneration: 1,
    })
  const memory = {
    list: vi.fn(list),
    history: vi.fn(),
    abandonRead: vi.fn(),
    progress: null,
    start: vi.fn(),
    lookup: vi.fn(),
  }
  const auth = { state: { busy: false }, agentMemory: memory } as unknown as SessionController
  const workspace = {
    currentReadContext: context,
    detail: { project: { lifecycle: 'active' } },
  } as unknown as ProjectWorkspace
  const scope = effectScope(),
    owner = scope.run(() => useAgentMemory(() => agent.value, workspace, auth))!
  return { agent, context, memory, owner, scope }
}
describe('Memory visible state ownership', () => {
  it('discards a complete old response after selecting another Agent', async () => {
    let resolve!: (page: MemoryPage) => void
    const f = setup(
      () =>
        new Promise((answer) => {
          resolve = answer
        }),
    )
    try {
      const loading = f.owner.load()
      f.agent.value = id(8)
      resolve({ memories: [record] })
      await loading
      expect(f.owner.state.records).toEqual([])
      expect(f.owner.state.phase).toBe('idle')
      expect(f.memory.abandonRead).toHaveBeenCalled()
    } finally {
      f.scope.stop()
    }
  })
  it('clears previously displayed content when history access is denied', async () => {
    const f = setup(async () => ({ memories: [record] }))
    try {
      await f.owner.load()
      f.memory.history.mockRejectedValue(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:forbidden',
          title: 'Forbidden',
          status: 403,
          detail: '',
          instance: '/',
          code: 'FORBIDDEN',
          request_id: id(9),
          commit_state: 'not_started',
        }),
      )
      await f.owner.history(record.current)
      expect(f.owner.state.records).toEqual([])
      expect(f.owner.state.selected).toBeNull()
      expect(f.owner.state.message).toContain('不可访问')
    } finally {
      f.scope.stop()
    }
  })
})
