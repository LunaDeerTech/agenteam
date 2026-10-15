import { describe, expect, it, vi } from 'vitest'
import { effectScope, reactive, ref, shallowRef } from 'vue'
import { AccountFailure } from '../api/client'
import type { SessionController } from '../composables/useSession'
import type { ProjectWorkspace } from '../composables/useProjectWorkspace'
import { useMeetingReferenceChoices } from '../composables/useMeetingReferenceChoices'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
function fixture() {
  const meeting = ref<string | null>(id(2)),
    context = shallowRef({
      projectID: id(1),
      identity: { userID: id(3), sessionID: id(4), epoch: 1 },
      generation: 1,
      readGeneration: 1,
    })
  const work = {
    milestones: vi.fn(async () => ({ items: [{ id: id(10), title: 'Milestone' }] })),
    sprints: vi.fn(async () => ({ items: [{ id: id(11), title: 'Sprint' }] })),
    tasks: vi.fn(async () => ({ items: [{ id: id(12), title: 'Task' }] })),
    abandonRead: vi.fn(),
  }
  const knowledge = {
    children: vi.fn(async () => ({ items: [{ id: id(20), title: 'Document' }] })),
    readContent: vi.fn(),
    abandon: vi.fn(),
  }
  const artifacts = {
    list: vi.fn(async () => ({
      items: [
        { artifact_ref: { project_id: id(1), artifact_id: id(30), file_id: id(31) }, name: 'File' },
      ],
      next_cursor: null as string | null,
    })),
    read: vi.fn(),
    abandon: vi.fn(),
  }
  const auth = {
    state: reactive({ busy: false }),
    workReview: work,
    knowledge,
    projectArtifacts: artifacts,
  } as unknown as SessionController
  const workspace = { currentReadContext: context } as unknown as ProjectWorkspace
  const scope = effectScope(),
    owner = scope.run(() => useMeetingReferenceChoices(() => meeting.value, auth, workspace))!
  return { scope, owner, meeting, context, work, knowledge, artifacts }
}
describe('Meeting reference selection read ownership', () => {
  it('uses the current Project read owner for a new Meeting draft without inventing a Meeting ID', async () => {
    const f = fixture()
    try {
      f.meeting.value = null
      await f.owner.changeKind('knowledge')
      expect(f.knowledge.children).toHaveBeenCalledWith(id(1), null, { limit: 50 })
      expect(f.owner.reference(f.owner.state.items[0]!)).toEqual({
        kind: 'knowledge',
        id: id(20),
        label: 'Document',
      })
    } finally {
      f.scope.stop()
    }
  })
  it('browses Task parents and captures a Task identity with the selected state', async () => {
    const f = fixture()
    try {
      await f.owner.changeKind('task')
      expect(f.owner.reference(f.owner.state.items[0]!)).toBeNull()
      await f.owner.browse(f.owner.state.items[0]!)
      expect(f.work.sprints).toHaveBeenCalledWith(id(1), id(10), { limit: 50 })
      await f.owner.browse(f.owner.state.items[0]!)
      expect(f.work.tasks).toHaveBeenCalledWith(id(1), {
        limit: 50,
        sprint_id: id(11),
        state: 'todo',
      })
      await f.owner.changeTaskState('done')
      expect(f.work.tasks).toHaveBeenLastCalledWith(id(1), {
        limit: 50,
        sprint_id: id(11),
        state: 'done',
      })
      expect(f.owner.reference(f.owner.state.items[0]!)).toEqual({
        kind: 'task',
        id: id(12),
        label: 'Task',
      })
      expect(f.owner.reference({ ...f.owner.state.items[0]!, id: id(99) })).toBeNull()
      await f.owner.back()
      expect(f.owner.state.sprint).toBeNull()
      expect(f.owner.state.milestone?.id).toBe(id(10))
    } finally {
      f.scope.stop()
    }
  })
  it('selects Document and stable Artifact identities without requesting their content', async () => {
    const f = fixture()
    try {
      await f.owner.changeKind('knowledge')
      expect(f.knowledge.children).toHaveBeenCalledWith(id(1), null, { limit: 50 })
      expect(f.owner.reference(f.owner.state.items[0]!)).toEqual({
        kind: 'knowledge',
        id: id(20),
        label: 'Document',
      })
      await f.owner.browse(f.owner.state.items[0]!)
      expect(f.knowledge.children).toHaveBeenLastCalledWith(id(1), id(20), { limit: 50 })
      await f.owner.changeKind('file')
      const original = f.owner.reference(f.owner.state.items[0]!)
      expect(original).toEqual({
        kind: 'file',
        id: id(30),
        label: 'File',
      })
      f.artifacts.list.mockResolvedValueOnce({
        items: [
          {
            artifact_ref: { project_id: id(1), artifact_id: id(30), file_id: id(32) },
            name: 'File',
          },
        ],
        next_cursor: null,
      })
      await f.owner.load()
      expect(f.owner.reference(f.owner.state.items[0]!)).toEqual(original)
      expect(f.owner.reference({ id: id(32), title: 'File', kind: 'file' })).toBeNull()
      expect(f.knowledge.readContent).not.toHaveBeenCalled()
      expect(f.artifacts.read).not.toHaveBeenCalled()
    } finally {
      f.scope.stop()
    }
  })
  it('abandons only its original read and drops late private items after Meeting scope changes', async () => {
    const f = fixture()
    let complete!: (value: Awaited<ReturnType<typeof f.artifacts.list>>) => void
    f.artifacts.list.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve
        }),
    )
    try {
      const pending = f.owner.changeKind('file')
      expect(f.owner.state.phase).toBe('loading')
      f.meeting.value = id(8)
      expect(f.artifacts.abandon).toHaveBeenCalledTimes(1)
      expect(f.work.abandonRead).not.toHaveBeenCalled()
      expect(f.knowledge.abandon).not.toHaveBeenCalled()
      complete({
        items: [
          {
            artifact_ref: { project_id: id(1), artifact_id: id(30), file_id: id(31) },
            name: 'Late private file',
          },
        ],
        next_cursor: null,
      })
      await pending
      expect(f.owner.state.items).toEqual([])
      expect(f.owner.state.kind).toBeNull()
      expect(f.owner.state.phase).toBe('idle')
    } finally {
      f.scope.stop()
    }
  })
  it('clears visible identities when a later current read is denied', async () => {
    const f = fixture()
    try {
      await f.owner.changeKind('file')
      f.artifacts.list.mockRejectedValueOnce(
        new AccountFailure('problem', {
          type: 'urn:agenteam:problem:forbidden',
          title: 'Forbidden',
          status: 403,
          detail: '',
          instance: '/',
          code: 'FORBIDDEN',
          request_id: id(40),
          commit_state: 'not_started',
        }),
      )
      await f.owner.load()
      expect(f.owner.state.items).toEqual([])
      expect(f.owner.state.message).toContain('无权访问')
      expect(f.owner.reference({ id: id(30), title: 'File', kind: 'file' })).toBeNull()
    } finally {
      f.scope.stop()
    }
  })
})
