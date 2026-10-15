import { createHash, webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMeetingRealtime } from '../composables/meeting-realtime'
import type { MeetingTopic } from '../api/meeting-realtime'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const topic: MeetingTopic = { kind: 'meeting.timeline', project_id: id(1), meeting_id: id(2) }
const execution: MeetingTopic = {
  kind: 'meeting.execution',
  project_id: id(1),
  meeting_id: id(2),
  contribution_id: id(3),
  generation: '2',
  attempt: '1',
  execution_id: id(4),
}
const at = '2026-10-15T00:00:00.000000Z'
function canonical(value: any): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value)
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']'
  return (
    '{' +
    Object.keys(value)
      .sort()
      .map((key) => JSON.stringify(key) + ':' + canonical(value[key]))
      .join(',') +
    '}'
  )
}
function snapshot(target: MeetingTopic, version = '1') {
  const content =
    target.kind === 'meeting.timeline'
      ? {
          session: {
            id: target.meeting_id,
            project_id: target.project_id,
            version,
            status: 'active',
            title: 'Original meeting',
            summary: null,
            deleting: false,
            created_at: at,
            updated_at: at,
          },
          participants: [],
          references: [],
          turns: [],
          decisions: [],
        }
      : {
          schema: 'execution_public_runtime_v1',
          execution_id: target.execution_id,
          project_id: target.project_id,
          execution_version: version,
          status: 'running',
          transcript_through: '0',
          items: [
            {
              id: 'state',
              seq: '1',
              revision: version,
              type: 'state',
              transcript_sequence: '0',
              state: { status: 'running' },
            },
          ],
        }
  return {
    topic: target,
    schema:
      target.kind === 'meeting.timeline' ? 'meeting_timeline_v1' : 'meeting_execution_runtime_v1',
    content,
    digest: 'sha256:' + createHash('sha256').update(canonical(content)).digest('hex'),
  }
}
class Socket {
  readyState = 0
  onopen: ((event: unknown) => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: unknown) => void) | null = null
  onerror: ((event: unknown) => void) | null = null
  sent: any[] = []
  send(raw: string) {
    this.sent.push(JSON.parse(raw))
  }
  close() {
    this.readyState = 3
  }
  open() {
    this.readyState = 1
    this.onopen?.({})
  }
  message(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) })
  }
  fail() {
    this.readyState = 3
    this.onclose?.({})
  }
}
const clear: (() => void)[] = []
beforeEach(() => vi.stubGlobal('crypto', webcrypto))
afterEach(() => {
  clear.splice(0).forEach((fn) => fn())
  vi.unstubAllGlobals()
})
function setup() {
  const identity = { userID: id(10), sessionID: id(11), epoch: 1 }
  let live = true
  const sockets: Socket[] = []
  const owner = createMeetingRealtime({
    identity: () => identity,
    current: (value) => live && value === identity,
    open: () => {
      const socket = new Socket()
      sockets.push(socket)
      return socket as unknown as WebSocket
    },
  })
  clear.push(owner.clear)
  return {
    owner,
    sockets,
    revoke: () => {
      live = false
    },
  }
}
const watermark = (sequence: string, epoch = id(20)) => ({ epoch, sequence })
const wait = (condition: () => unknown) =>
  vi.waitFor(() => expect(condition()).toBeTruthy(), { timeout: 1500, interval: 5 })
async function acknowledge(socket: Socket, index = 0, sequence = '5', epoch = id(20)) {
  const request = socket.sent.filter((message) => message.type === 'subscribe')[index]
  socket.message({ ...request, type: 'subscribed', watermark: watermark(sequence, epoch) })
  await wait(() =>
    socket.sent.some(
      (message) =>
        message.type === 'snapshot' && message.subscription_id === request.subscription_id,
    ),
  )
  return request
}
function publish(socket: Socket, request: any, sequence: string, version = '1', epoch = id(20)) {
  socket.message({
    ...request,
    type: 'snapshot',
    watermark: watermark(sequence, epoch),
    snapshot: snapshot(request.topic, version),
  })
}
describe('Meeting realtime original session ownership', () => {
  it('shares one connection, requests a snapshot only after acknowledgement, and retires an exact subscription', async () => {
    const f = setup(),
      timeline = f.owner.subscribe(topic),
      runtime = f.owner.subscribe(execution)
    expect(f.sockets).toHaveLength(1)
    const socket = f.sockets[0]!
    expect(socket.sent).toEqual([])
    socket.open()
    expect(socket.sent.map((message) => message.type)).toEqual(['subscribe', 'subscribe'])
    const first = await acknowledge(socket),
      second = await acknowledge(socket, 1)
    expect(second.topic).toEqual(execution)
    publish(socket, first, '5')
    publish(socket, second, '5')
    await wait(() => timeline.state.phase === 'ready' && runtime.state.phase === 'ready')
    timeline.dispose()
    expect(socket.readyState).toBe(1)
    expect(socket.sent.at(-1)).toEqual({
      type: 'unsubscribe',
      subscription_id: first.subscription_id,
    })
    publish(socket, first, '6', '2') // In-flight old read cannot resurrect the closed view.
    socket.message({ type: 'unsubscribed', subscription_id: first.subscription_id })
    await wait(() => timeline.state.phase === 'closed')
    expect(timeline.state.snapshot).toBeNull()
    runtime.dispose()
    expect(socket.readyState).toBe(3)
  })
  it('rereads a snapshot overtaken by a committed invalidation without confusing durable versions and transport sequences', async () => {
    const f = setup(),
      view = f.owner.subscribe(topic),
      socket = f.sockets[0]!
    socket.open()
    const request = await acknowledge(socket, 0, '9007199254740993')
    socket.message({ ...request, type: 'invalidated', watermark: watermark('9007199254740995') })
    publish(socket, request, '9007199254740994')
    await wait(() => socket.sent.filter((message) => message.type === 'snapshot').length === 2)
    expect(view.state.phase).toBe('loading')
    publish(socket, request, '9007199254740995', '2')
    await wait(() => view.state.phase === 'ready')
    expect(view.state.snapshot?.content).toMatchObject({ session: { version: '2' } })
  })
  it('keeps unavailable or disconnected reads explicit, then reconnects with fresh subscription IDs and a fresh snapshot', async () => {
    const f = setup(),
      view = f.owner.subscribe(topic),
      socket = f.sockets[0]!
    socket.open()
    const request = await acknowledge(socket)
    socket.message({ ...request, type: 'resync', watermark: watermark('5'), reason: 'unavailable' })
    await wait(() => view.state.phase === 'stale')
    expect(socket.sent.filter((message) => message.type === 'snapshot')).toHaveLength(1)
    view.refresh()
    publish(socket, request, '5')
    await wait(() => view.state.phase === 'ready')
    const late = socket.onmessage!
    socket.fail()
    expect(view.state.phase).toBe('stale')
    expect(view.state.snapshot).not.toBeNull()
    expect(f.sockets).toHaveLength(1)
    view.refresh()
    const replacement = f.sockets[1]!
    expect(view.state.snapshot).toBeNull()
    replacement.open()
    const next = await acknowledge(replacement, 0, '0', id(21))
    expect(next.subscription_id).not.toBe(request.subscription_id)
    late({ data: JSON.stringify({ ...request, type: 'invalidated', watermark: watermark('99') }) })
    publish(replacement, next, '0', '3', id(21))
    await wait(() => view.state.phase === 'ready')
    expect(view.state.snapshot?.content).toMatchObject({ session: { version: '3' } })
  })
  it('erases observations when the original session is no longer current, including queued late callbacks', async () => {
    const f = setup(),
      view = f.owner.subscribe(topic),
      socket = f.sockets[0]!
    socket.open()
    const request = await acknowledge(socket)
    publish(socket, request, '5')
    await wait(() => view.state.phase === 'ready')
    f.revoke()
    socket.message({ ...request, type: 'invalidated', watermark: watermark('6') })
    expect(view.state.phase).toBe('closed')
    expect(view.state.snapshot).toBeNull()
    expect(socket.readyState).toBe(3)
    expect(() => f.owner.subscribe(topic)).toThrow()
  })
  it('closes on a foreign tuple and on queue overflow instead of silently discarding required updates', async () => {
    const f = setup(),
      view = f.owner.subscribe(topic),
      socket = f.sockets[0]!
    socket.open()
    const request = socket.sent[0]
    socket.message({
      ...request,
      type: 'subscribed',
      topic: { ...topic, meeting_id: id(99) },
      watermark: watermark('5'),
    })
    await wait(() => view.state.reason === 'invalid')
    view.refresh()
    const replacement = f.sockets[1]!
    replacement.open()
    const next = replacement.sent[0]
    for (let n = 0; n < 70; n++)
      replacement.message({ ...next, type: 'subscribed', watermark: watermark('5') })
    expect(view.state.reason).toBe('overflow')
    expect(replacement.readyState).toBe(3)
  })
  it('bounds open subscriptions and repeated invalidation catch-up', async () => {
    const f = setup(),
      view = f.owner.subscribe(topic),
      socket = f.sockets[0]!
    for (let n = 1; n < 16; n++) f.owner.subscribe({ ...topic, meeting_id: id(100 + n) })
    expect(() => f.owner.subscribe({ ...topic, meeting_id: id(200) })).toThrow()
    socket.open()
    const request = await acknowledge(socket, 0, '0')
    for (let n = 0; n < 9; n++) {
      socket.message({ ...request, type: 'invalidated', watermark: watermark(String(n + 1)) })
      publish(socket, request, String(n))
      await wait(
        () =>
          view.state.reason === 'overflow' ||
          socket.sent.filter((message) => message.type === 'snapshot').length === n + 2,
      )
    }
    expect(view.state.reason).toBe('overflow')
    expect(socket.readyState).toBe(3)
  })
})
