import formalWorkSchema from '../../../api/openapi/work-planning.json'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, uuid7, type Fetch } from '../api/client'
import {
  captureWorkPlanningCommand,
  createWorkPlanningAPI,
  newWorkPlanningID,
  parseWorkPlanningReceipt,
  workPlanningLookup,
  type WorkPlanningCommand,
} from '../api/work-planning'

const id = (n: number) => `01900000-0000-7000-8000-${String(n).padStart(12, '0')}`
const project = id(1),
  milestoneID = id(2),
  sprintID = id(3),
  taskID = id(4),
  blockerID = id(5)
const at = '2026-10-09T10:00:00.000000Z'
const base = {
  id: milestoneID,
  project_id: project,
  title: '原始 标题',
  manual_rank: '8'.repeat(32),
  version: '1',
  created_at: at,
  updated_at: at,
}
const milestone = { ...base, description: '' }
const sprint = {
  ...base,
  id: sprintID,
  milestone_id: milestoneID,
  state: 'planned',
  description: '',
  started_at: null,
  started_by: null,
  completed_at: null,
  completed_by: null,
}
const task = {
  ...base,
  id: taskID,
  milestone_id: milestoneID,
  sprint_id: sprintID,
  type: 'task',
  priority: 'medium',
  state: 'backlog',
  assignee_agent_id: null,
  description: '',
  plan: '',
}
const actor = { type: 'human', user_id: id(10), source: 'task_domain' }
const blocker = {
  id: blockerID,
  project_id: project,
  task_id: taskID,
  type: 'waiting_for_human',
  description: '',
  metadata: {},
  created_at: at,
  created_by: actor,
  resolved_at: null,
  resolved_by: null,
  resolution_comment: null,
}
const signal = () => new AbortController().signal
const options = () => ({ signal: signal(), csrf: 'S'.repeat(43), key: 'original-key' })
const response = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
function receipt(command: WorkPlanningCommand): unknown {
  if (command.domain === 'structure') {
    const full = command.command.startsWith('work.milestone.') ? milestone : sprint
    const create = command.command.endsWith('.create')
    return {
      command: command.command,
      changed: true,
      milestone: command.command.startsWith('work.milestone.')
        ? { ...full, ...command.request, version: create ? '1' : '2' }
        : null,
      sprint: command.command.startsWith('work.sprint.')
        ? { ...full, ...command.request, version: create ? '1' : '2' }
        : null,
      event_id: id(20),
    }
  }
  if (command.domain === 'task')
    return {
      task: {
        ...task,
        ...command.request,
        version: command.command.endsWith('.create') ? '1' : '2',
      },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    }
  return {
    task: { ...task, version: '2' },
    blocker: command.command.endsWith('.resolve')
      ? { ...blocker, resolved_at: at, resolved_by: actor, resolution_comment: null }
      : blocker,
    task_event_id: id(21),
    event_ids: [id(22)],
  }
}
// Server projections do not include creation DTO IDs or reorder anchors.
function wireReceipt(command: WorkPlanningCommand): unknown {
  const raw = receipt(command) as Record<string, unknown>
  for (const field of ['milestone', 'sprint', 'task']) {
    const value = raw[field] as Record<string, unknown> | null
    if (!value) continue
    delete value.before_id
    if (field === 'milestone') delete value.milestone_id
    if (field === 'sprint') delete value.sprint_id
    if (field === 'task') delete value.task_id
  }
  return raw
}
const originals: WorkPlanningCommand[] = [
  {
    domain: 'structure',
    projectID: project,
    command: 'work.milestone.create',
    request: { milestone_id: milestoneID, title: base.title },
  },
  {
    domain: 'structure',
    projectID: project,
    command: 'work.milestone.update',
    targetID: milestoneID,
    expected_version: '1',
    request: { title: base.title },
  },
  {
    domain: 'structure',
    projectID: project,
    command: 'work.milestone.reorder',
    targetID: milestoneID,
    expected_version: '1',
    request: {},
  },
  {
    domain: 'structure',
    projectID: project,
    command: 'work.sprint.create',
    request: { sprint_id: sprintID, milestone_id: milestoneID, title: base.title },
  },
  {
    domain: 'structure',
    projectID: project,
    command: 'work.sprint.update',
    targetID: sprintID,
    expected_version: '1',
    request: { title: base.title },
  },
  {
    domain: 'structure',
    projectID: project,
    command: 'work.sprint.reorder',
    targetID: sprintID,
    expected_version: '1',
    request: { milestone_id: milestoneID },
  },
  {
    domain: 'task',
    projectID: project,
    command: 'work.task.create',
    request: {
      task_id: taskID,
      sprint_id: sprintID,
      title: base.title,
      type: 'task',
      priority: 'medium',
    },
  },
  {
    domain: 'task',
    projectID: project,
    command: 'work.task.update',
    targetID: taskID,
    expected_version: '1',
    request: { title: base.title },
  },
  {
    domain: 'task',
    projectID: project,
    command: 'work.task.reorder',
    targetID: taskID,
    expected_version: '1',
    request: {},
  },
  {
    domain: 'blocker',
    projectID: project,
    command: 'work.task.blocker.add',
    taskID,
    expected_version: '1',
    request: { blocker_id: blockerID, type: 'waiting_for_human', description: '', metadata: {} },
  },
  {
    domain: 'blocker',
    projectID: project,
    command: 'work.task.blocker.resolve',
    taskID,
    expected_version: '1',
    request: { blocker_id: blockerID, resolution_comment: null },
  },
]
afterEach(() => vi.restoreAllMocks())
describe('Work closed API and original intent', () => {
  it('generates resource UUIDv7 using cryptographic random bytes and refuses absent entropy', () => {
    const values = Array.from({ length: 32 }, () => newWorkPlanningID())
    expect(values.every((v) => uuid7.test(v))).toBe(true)
    expect(new Set(values).size).toBe(values.length)
    const timestamp = BigInt('0x' + values[0]!.replaceAll('-', '').slice(0, 12))
    expect(Number(timestamp)).toBeLessThanOrEqual(Date.now())
    vi.spyOn(crypto, 'getRandomValues').mockImplementation(() => {
      throw new Error('entropy unavailable')
    })
    expect(() => newWorkPlanningID()).toThrow()
  })
  it('captures an immutable independent original, preserving presence and exact Unicode', () => {
    const mutable = {
      ...originals[7]!,
      request: { description: '  字\n', plan: '' },
    } as WorkPlanningCommand
    const captured = captureWorkPlanningCommand(mutable)
    ;(mutable.request as { description: string }).description = 'changed'
    expect(captured.request).toEqual({ description: '  字\n', plan: '' })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(Object.isFrozen(captured.request)).toBe(true)
    const max = captureWorkPlanningCommand({
      ...originals[1]!,
      expected_version: '9223372036854775807',
    } as WorkPlanningCommand)
    expect('expected_version' in max && max.expected_version).toBe('9223372036854775807')
  })
  it.each([
    { request: {} },
    { request: { title: null } },
    { request: { title: '\u0085' } },
    { request: { title: 'a'.repeat(257) } },
    { request: { description: '字'.repeat(10923) } },
    { request: { description: '\ud800' } },
    { request: { plan: '\0' } },
    { expected_version: '01' },
    { expected_version: 1 },
    { expected_version: '9223372036854775808' },
    { request: { unknown: true } },
    { domain: 'structure' },
    { targetID: '01900000-0000-7000-8000-00000000000A' },
    { projectID: '../' },
  ])('rejects invalid original before transport %j', (change) => {
    expect(() =>
      captureWorkPlanningCommand({ ...originals[7]!, ...change } as WorkPlanningCommand),
    ).toThrow(AccountFailure)
  })
  it.each(['', ' ', '\t\n', '\u0085'])(
    'does not normalize a non-null resolution comment %j',
    (resolution_comment) => {
      expect(() =>
        captureWorkPlanningCommand({
          ...originals[10]!,
          request: { blocker_id: blockerID, resolution_comment },
        } as WorkPlanningCommand),
      ).toThrow(AccountFailure)
    },
  )
  it('has exactly 21 capabilities with the formal methods, bodies, keys and closed paths', async () => {
    const sent: { path: string; init: RequestInit }[] = []
    let returned: unknown
    const api = createWorkPlanningAPI(async (path, init) => {
      sent.push({ path, init })
      return response(returned)
    })
    expect(Object.keys(api)).toHaveLength(21)
    const { description: _md, ...ms } = milestone
    const {
      description: _sd,
      started_at: _sa,
      started_by: _sb,
      completed_at: _ca,
      completed_by: _cb,
      ...ss
    } = sprint
    const { description: _td, plan: _p, ...ts } = task
    returned = { items: [ms], next_cursor: 'opaque+/=字' }
    await api.listMilestones(project, { limit: 50, cursor: 'opaque+/=字' }, signal())
    returned = milestone
    await api.getMilestone(project, milestoneID, signal())
    returned = { items: [ss] }
    await api.listSprints(project, { milestone_id: milestoneID, limit: 50 }, signal())
    returned = sprint
    await api.getSprint(project, sprintID, signal())
    returned = { items: [ts] }
    await api.listTasks(
      project,
      { sprint_id: sprintID, state: 'backlog', assignee_agent_id: null, limit: 50 },
      signal(),
    )
    returned = task
    await api.getTask(project, taskID, signal())
    returned = { items: [blocker] }
    await api.listTaskBlockers(project, taskID, { limit: 50 }, signal())
    for (const c of originals) {
      returned = wireReceipt(c)
      const o = options()
      switch (c.command) {
        case 'work.milestone.create':
          await api.createMilestone(project, { request: c.request }, o)
          break
        case 'work.milestone.update':
          await api.updateMilestone(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.milestone.reorder':
          await api.reorderMilestone(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.sprint.create':
          await api.createSprint(project, { request: c.request }, o)
          break
        case 'work.sprint.update':
          await api.updateSprint(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.sprint.reorder':
          await api.reorderSprint(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.task.create':
          await api.createTask(project, { request: c.request }, o)
          break
        case 'work.task.update':
          await api.updateTask(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.task.reorder':
          await api.reorderTask(
            project,
            c.targetID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.task.blocker.add':
          await api.addTaskBlocker(
            project,
            taskID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
        case 'work.task.blocker.resolve':
          await api.resolveTaskBlocker(
            project,
            taskID,
            { expected_version: c.expected_version, request: c.request },
            o,
          )
          break
      }
    }
    for (const index of [0, 6, 9]) {
      const c = originals[index]!
      if (c.domain === 'structure') {
        returned = { state: 'committed', result: wireReceipt(c) }
        await api.lookupStructureCommand(project, workPlanningLookup(c), options())
      } else if (c.domain === 'task') {
        returned = { status: 'committed', receipt: wireReceipt(c) }
        await api.lookupTaskCommand(project, workPlanningLookup(c), options())
      } else {
        returned = { status: 'committed', receipt: wireReceipt(c) }
        await api.lookupTaskBlockerCommand(project, taskID, workPlanningLookup(c), options())
      }
    }
    expect(sent).toHaveLength(21)
    expect(sent[0]!.path).toContain('cursor=opaque%2B%2F%3D%E5%AD%97')
    expect(sent[4]!.path).toContain('assignee_agent_id=null')
    for (const { init } of sent) {
      expect(init).toMatchObject({
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      })
      if (init.method !== 'GET')
        expect(init.headers).toMatchObject({
          'X-CSRF-Token': 'S'.repeat(43),
          'Idempotency-Key': 'original-key',
        })
    }
    expect(sent.slice(7).map((x) => x.init.method)).toEqual([
      'POST',
      'PATCH',
      'POST',
      'POST',
      'PATCH',
      'POST',
      'POST',
      'PATCH',
      'POST',
      'POST',
      'POST',
      'POST',
      'POST',
      'POST',
    ])
    expect(JSON.parse(sent[7]!.init.body as string)).toEqual({ request: originals[0]!.request })
    expect(sent[20]!.path).toBe(
      `/api/v1/projects/${project}/tasks/${taskID}/blocker-commands/lookup`,
    )
    const schema = formalWorkSchema
    const operations = Object.values(
      schema.paths as Record<string, Record<string, unknown>>,
    ).flatMap((v) => Object.keys(v).filter((k) => ['get', 'post', 'patch'].includes(k)))
    expect(operations).toHaveLength(21)
  })
  it.each([
    { project_id: id(99) },
    { id: id(99) },
    { version: 1 },
    { version: '01' },
    { title: '\u0000' },
    { extra: true },
    { created_at: '2026-02-30T00:00:00.000000Z' },
    { updated_at: '2025-01-01T00:00:00.000000Z' },
  ])('rejects invalid full detail %j', async (change) => {
    await expect(
      createWorkPlanningAPI(async () => response({ ...task, ...change })).getTask(
        project,
        taskID,
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects the whole page on a bad final element, extra detail, wrong parent, duplicate or null cursor', async () => {
    const { description: _d, ...summary } = milestone
    for (const body of [
      { items: [summary, { ...summary, id: id(9), version: '01' }] },
      { items: [milestone] },
      { items: [summary, summary] },
      { items: [summary], next_cursor: null },
    ]) {
      await expect(
        createWorkPlanningAPI(async () => response(body)).listMilestones(
          project,
          { limit: 50 },
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    const {
      description: _sd,
      started_at: _sa,
      started_by: _sb,
      completed_at: _ca,
      completed_by: _cb,
      ...ss
    } = sprint
    await expect(
      createWorkPlanningAPI(async () => response({ items: [ss] })).listSprints(
        project,
        { milestone_id: id(99) },
        signal(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('ties every receipt to the original command, fields, version and target', () => {
    for (const command of originals) {
      expect(parseWorkPlanningReceipt(wireReceipt(command), command).domain).toBe(command.domain)
      const wrong = structuredClone(wireReceipt(command)) as Record<string, unknown>
      const field =
        command.domain === 'structure'
          ? command.command.startsWith('work.milestone')
            ? 'milestone'
            : 'sprint'
          : 'task'
      ;(wrong[field] as { version: string }).version = '42'
      expect(() => parseWorkPlanningReceipt(wrong, command)).toThrow(AccountFailure)
    }
    const raw = wireReceipt(originals[1]!) as { command: string }
    raw.command = 'work.milestone.reorder'
    expect(() => parseWorkPlanningReceipt(raw, originals[1]!)).toThrow(AccountFailure)
  })
  it('still rejects wrong resource IDs and parent IDs in valid creation projections', () => {
    for (const index of [0, 3, 6]) {
      const command = originals[index]!
      const field = index === 0 ? 'milestone' : index === 3 ? 'sprint' : 'task'
      for (const key of index === 0
        ? ['id']
        : index === 3
          ? ['id', 'milestone_id']
          : ['id', 'sprint_id']) {
        const raw = structuredClone(wireReceipt(command)) as Record<string, Record<string, unknown>>
        raw[field]![key] = id(99)
        expect(() => parseWorkPlanningReceipt(raw, command)).toThrow(AccountFailure)
      }
    }
  })
  it.each([
    { state: 'not_observed' },
    { status: 'not_observed', receipt: null },
    { state: 'not_observed', result: {} },
    { state: 'committed', result: null },
    { state: 'in_progress', result: null, extra: 1 },
  ])('rejects malformed/cross-domain lookup %j', async (value) => {
    const c = originals[0]!
    if (c.domain !== 'structure') throw new Error()
    await expect(
      createWorkPlanningAPI(async () => response(value)).lookupStructureCommand(
        project,
        workPlanningLookup(c),
        options(),
      ),
    ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it.each(['not_observed', 'in_progress'] as const)(
    'preserves explicit non-committed observation %s',
    async (status) => {
      const c = originals[6]!
      if (c.domain !== 'task') throw new Error()
      expect(
        await createWorkPlanningAPI(async () =>
          response({ status, receipt: null }),
        ).lookupTaskCommand(project, workPlanningLookup(c), options()),
      ).toEqual({ status, receipt: null })
    },
  )
})

describe('Work transport byte limits and actual EOF/cancel', () => {
  it.each(['old', 'detail', 'page'] as const)(
    'uses the isolated actual-byte cap for %s',
    async (kind) => {
      const cap = kind === 'old' ? 600_000 : kind === 'detail' ? 1024 * 1024 : 5 * 1024 * 1024
      const invoke = (count: number) => {
        const transport = accountTransport(
          async () =>
            new Response(' '.repeat(count - 2) + '{}', {
              headers: { 'Content-Type': 'application/json', 'Content-Length': '1' },
            }),
        )
        if (kind === 'old') return transport('bootstrap', (v) => v, { signal: signal() })
        if (kind === 'detail')
          return transport('workGetTask', (v) => v, {
            signal: signal(),
            projectID: project,
            target: taskID,
          })
        return transport('workListTasks', (v) => v, {
          signal: signal(),
          projectID: project,
          workQuery: {},
        })
      }
      expect(await invoke(cap)).toEqual({})
      await expect(invoke(cap + 1)).rejects.toMatchObject({ kind: 'invalid-response' })
    },
  )
  it('rejects request bytes above 1MiB before fetch without expanding old request caps', async () => {
    const fetcher = vi.fn<Fetch>(async () => response({})),
      transport = accountTransport(fetcher)
    await expect(
      transport('workCreateMilestone', (v) => v, {
        ...options(),
        projectID: project,
        body: { padding: 'x'.repeat(1024 * 1024) },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetcher).not.toHaveBeenCalled()
  })
  it('accepts maximum legal text and Unicode scalars without trimming or JS code-unit confusion', async () => {
    const title = '😀'.repeat(256),
      description = '\t'.repeat(32768),
      plan = '字'.repeat(10922) + 'aa'
    const c = captureWorkPlanningCommand({
      ...originals[6]!,
      request: {
        task_id: taskID,
        sprint_id: sprintID,
        title,
        description,
        plan,
        type: 'feature',
        priority: 'high',
      },
    } as WorkPlanningCommand)
    const raw = {
      task: { ...task, title, description, plan, type: 'feature', priority: 'high' },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    }
    expect(parseWorkPlanningReceipt(raw, c).domain).toBe('task')
    const api = createWorkPlanningAPI(async () => response(raw))
    if (c.command !== 'work.task.create') throw new Error()
    expect((await api.createTask(project, { request: c.request }, options())).task.title).toBe(
      title,
    )
  })
  it.each([
    new Uint8Array([0x7b, 0x22, 0x78, 0x22, 0x3a, 0x22, 0xff, 0x22, 0x7d]),
    new TextEncoder().encode('{"task":'),
  ])('rejects invalid UTF8 or truncated JSON at EOF', async (bytes) => {
    const api = createWorkPlanningAPI(
      async () => new Response(bytes, { headers: { 'Content-Type': 'application/json' } }),
    )
    await expect(api.getTask(project, taskID, signal())).rejects.toMatchObject({
      kind: 'invalid-response',
    })
  })
  it('does not publish a complete JSON prefix before EOF and joins the single cancel tail', async () => {
    let close!: () => void,
      release!: () => void,
      cancellations = 0,
      settled = false
    const tail = new Promise<void>((r) => {
        release = r
      }),
      abort = new AbortController()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(JSON.stringify(task)))
        close = () => controller.close()
      },
      cancel() {
        cancellations++
        return tail
      },
    })
    const api = createWorkPlanningAPI(
      async () => new Response(stream, { headers: { 'Content-Type': 'application/json' } }),
    )
    const pending = api
      .getTask(project, taskID, abort.signal)
      .finally(() => {
        settled = true
      })
      .catch((e) => e)
    await new Promise<void>((r) => setTimeout(r, 0))
    expect(settled).toBe(false)
    abort.abort()
    await new Promise<void>((r) => setTimeout(r, 0))
    expect(settled).toBe(false)
    expect(cancellations).toBe(1)
    release()
    expect(await pending).toMatchObject({ kind: 'cancelled' })
    expect(cancellations).toBe(1)
    expect(stream.locked).toBe(false)
    // A distinct complete stream proves actual EOF success using the same parser.
    let complete!: () => void
    const completeStream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode(JSON.stringify(task)))
        complete = () => c.close()
      },
    })
    const success = createWorkPlanningAPI(
      async () => new Response(completeStream, { headers: { 'Content-Type': 'application/json' } }),
    ).getTask(project, taskID, signal())
    complete()
    expect((await success).id).toBe(taskID)
    void close
  })
  it('checks actual summary and receipt projections against formal schema keys and nullable unions', () => {
    const schemas = formalWorkSchema.components.schemas
    const { description: _d, ...summary } = milestone
    expect(Object.keys(summary).sort()).toEqual([...schemas.MilestoneSummary.required].sort())
    expect(Object.keys(milestone).sort()).toEqual([...schemas.Milestone.required].sort())
    expect(Object.keys(task).sort()).toEqual([...schemas.Task.required].sort())
    expect(Object.keys(blocker).sort()).toEqual([...schemas.TaskBlocker.oneOf[0]!.required].sort())
    for (const c of originals) {
      const raw = wireReceipt(c) as object
      const required =
        c.domain === 'structure'
          ? schemas.StructureMutation.oneOf[0]!.required
          : c.domain === 'task'
            ? schemas.TaskMutation.oneOf[0]!.required
            : schemas.BlockerMutation.required
      expect(Object.keys(raw).sort()).toEqual([...required].sort())
      expect(() => parseWorkPlanningReceipt({ ...raw, unexpected: 'not-in-schema' }, c)).toThrow(
        AccountFailure,
      )
    }
    expect(schemas.StructureLookup.oneOf[1]!.properties.result).toEqual({ type: 'null' })
    expect(schemas.TaskLookup.oneOf[1]!.properties.receipt).toEqual({ type: 'null' })
    expect(schemas.BlockerLookup.oneOf[1]!.properties.receipt).toEqual({ type: 'null' })
  })
})
