import { readonly, shallowReactive } from 'vue'
import { AccountFailure } from '../api/client'
import {
  captureMeetingTopic,
  decodeMeetingRealtime,
  meetingRealtimeClientMessage,
  meetingRealtimeLimits,
  newMeetingSubscriptionID,
  sameMeetingTopic,
  type MeetingRealtimeMessage,
  type MeetingRealtimeSnapshot,
  type MeetingTopic,
  type MeetingWatermark,
} from '../api/meeting-realtime'
import type { PersonalIdentity } from './useSession'

type Socket = Pick<
  WebSocket,
  'readyState' | 'send' | 'close' | 'onopen' | 'onclose' | 'onerror' | 'onmessage'
>
type Reason = '' | 'disconnected' | 'unavailable' | 'invalid' | 'overflow' | 'context'
export type MeetingSubscriptionState = {
  phase: 'connecting' | 'loading' | 'ready' | 'stale' | 'closed'
  reason: Reason
  snapshot: MeetingRealtimeSnapshot | null
}
export type MeetingSubscription = {
  state: Readonly<MeetingSubscriptionState>
  refresh(): void
  dispose(): void
}
type Entry = {
  id: string
  identity: PersonalIdentity
  topic: MeetingTopic
  state: MeetingSubscriptionState
  active: boolean
  acknowledged: boolean
  awaiting: boolean
  again: boolean
  resnapshots: number
  latest: MeetingWatermark | null
  requested: MeetingWatermark | null
  timer?: ReturnType<typeof setTimeout>
}
type Connection = {
  socket: Socket
  identity: PersonalIdentity
  queue: string[]
  queuedBytes: number
  processing: boolean
  epoch: string | null
  timer?: ReturnType<typeof setTimeout>
}

// One transport belongs to the original account session. A subscription is a
// read observation, never a replacement for command/approval authorization.
export function createMeetingRealtime(ports: {
  identity(): PersonalIdentity
  current(identity: PersonalIdentity): boolean
  open(): Socket
}): { subscribe(topic: MeetingTopic): MeetingSubscription; clear(): void } {
  const entries = new Map<string, Entry>()
  let connection: Connection | null = null
  function stopTimer(entry: Entry) {
    if (entry.timer !== undefined) clearTimeout(entry.timer)
    entry.timer = undefined
  }
  function current(own: Connection) {
    if (connection !== own) return false
    if (!ports.current(own.identity)) {
      clear()
      return false
    }
    return true
  }
  function disconnect(reason: Exclude<Reason, ''>, erase = false) {
    const previous = connection
    connection = null
    if (previous) {
      if (previous.timer !== undefined) clearTimeout(previous.timer)
      previous.queue.length = 0
      previous.queuedBytes = 0
      previous.socket.onopen =
        previous.socket.onmessage =
        previous.socket.onerror =
        previous.socket.onclose =
          null
      previous.socket.close()
    }
    for (const [id, entry] of entries) {
      stopTimer(entry)
      entry.awaiting = entry.acknowledged = false
      if (erase || !entry.active) {
        entry.active = false
        entry.state.phase = 'closed'
        entry.state.snapshot = null
        entries.delete(id)
      } else entry.state.phase = 'stale'
      entry.state.reason = reason
    }
  }
  function clear() {
    disconnect('context', true)
  }
  function deadline(entry: Entry) {
    stopTimer(entry)
    entry.timer = setTimeout(() => disconnect('unavailable'), 10_000)
  }
  function send(own: Connection, type: 'subscribe' | 'snapshot' | 'unsubscribe', entry: Entry) {
    if (!current(own) || own.socket.readyState !== 1) return false
    try {
      own.socket.send(
        meetingRealtimeClientMessage(
          type,
          entry.id,
          type === 'subscribe' ? entry.topic : undefined,
        ),
      )
      return true
    } catch {
      disconnect('disconnected')
      return false
    }
  }
  function requestSnapshot(own: Connection, entry: Entry) {
    if (!entry.active || !entry.acknowledged || !entry.latest) return
    if (entry.awaiting) {
      entry.again = true
      return
    }
    entry.awaiting = true
    entry.again = false
    entry.requested = entry.latest
    entry.state.phase = 'loading'
    entry.state.reason = ''
    if (send(own, 'snapshot', entry)) deadline(entry)
  }
  function apply(own: Connection, message: MeetingRealtimeMessage) {
    if (!current(own)) return
    const entry = entries.get(message.subscription_id)
    if (!entry) {
      disconnect('invalid')
      return
    }
    if (message.type === 'unsubscribed') {
      if (entry.active) {
        disconnect('invalid')
        return
      }
      stopTimer(entry)
      entries.delete(entry.id)
      return
    }
    if (!sameMeetingTopic(entry.topic, message.topic)) {
      disconnect('invalid')
      return
    }
    if (!entry.active) return // Already retiring; queued old observations cannot render.
    const watermark = message.watermark
    if (own.epoch !== null && own.epoch !== watermark.epoch) {
      disconnect('disconnected')
      return
    }
    own.epoch = watermark.epoch
    if (message.type === 'subscribed') {
      if (entry.acknowledged) {
        disconnect('invalid')
        return
      }
      stopTimer(entry)
      entry.acknowledged = true
      entry.latest = watermark
      requestSnapshot(own, entry)
      return
    }
    if (!entry.acknowledged || !entry.latest) {
      disconnect('invalid')
      return
    }
    if (message.type === 'resync') {
      if (message.reason !== 'unavailable') {
        disconnect(message.reason === 'overflow' ? 'overflow' : 'disconnected')
        return
      }
      stopTimer(entry)
      entry.awaiting = false
      entry.state.phase = 'stale'
      entry.state.reason = 'unavailable'
      return
    }
    if (message.type === 'invalidated') {
      if (BigInt(watermark.sequence) <= BigInt(entry.latest.sequence)) return
      entry.latest = watermark
      if (!entry.awaiting) requestSnapshot(own, entry)
      return
    }
    if (message.type !== 'snapshot') {
      disconnect('invalid')
      return
    }
    if (
      !entry.awaiting ||
      !entry.requested ||
      BigInt(watermark.sequence) < BigInt(entry.requested.sequence)
    ) {
      disconnect('invalid')
      return
    }
    stopTimer(entry)
    entry.awaiting = false
    entry.state.snapshot = message.snapshot
    if (BigInt(watermark.sequence) < BigInt(entry.latest.sequence) || entry.again) {
      // Catch invalidations that arrived while the canonical reader was running.
      // A continuously changing source yields to an explicit user retry.
      if (++entry.resnapshots > 8) {
        disconnect('overflow')
        return
      }
      requestSnapshot(own, entry)
    } else {
      entry.latest = watermark
      entry.resnapshots = 0
      entry.state.phase = 'ready'
      entry.state.reason = ''
    }
  }
  async function drain(own: Connection) {
    if (own.processing) return
    own.processing = true
    try {
      while (current(own) && own.queue.length) {
        const raw = own.queue.shift()!
        own.queuedBytes -= new TextEncoder().encode(raw).length
        const message = await decodeMeetingRealtime(raw)
        if (current(own)) apply(own, message)
      }
    } catch {
      if (current(own)) disconnect('invalid')
    } finally {
      own.processing = false
    }
  }
  function connect() {
    if (connection || ![...entries.values()].some((entry) => entry.active)) return
    let identity: PersonalIdentity
    try {
      identity = ports.identity()
      if (
        !ports.current(identity) ||
        [...entries.values()].some((entry) => entry.active && !ports.current(entry.identity))
      ) {
        clear()
        return
      }
    } catch {
      clear()
      return
    }
    let own: Connection
    try {
      own = {
        socket: ports.open(),
        identity,
        queue: [],
        queuedBytes: 0,
        processing: false,
        epoch: null,
      }
    } catch {
      disconnect('unavailable')
      return
    }
    connection = own
    const active = [...entries.values()].filter((entry) => entry.active)
    entries.clear()
    for (const entry of active) {
      entry.id = newMeetingSubscriptionID()
      entry.acknowledged = entry.awaiting = entry.again = false
      entry.latest = entry.requested = null
      entry.resnapshots = 0
      entry.state.phase = 'connecting'
      entry.state.reason = ''
      entry.state.snapshot = null
      entries.set(entry.id, entry)
    }
    own.timer = setTimeout(() => {
      if (current(own)) disconnect('unavailable')
    }, 10_000)
    own.socket.onopen = () => {
      if (!current(own)) return
      if (own.timer !== undefined) clearTimeout(own.timer)
      own.timer = undefined
      for (const entry of entries.values())
        if (entry.active && send(own, 'subscribe', entry)) deadline(entry)
    }
    own.socket.onclose = own.socket.onerror = () => {
      if (current(own)) disconnect('disconnected')
    }
    own.socket.onmessage = (event) => {
      if (!current(own)) return
      if (typeof event.data !== 'string') {
        disconnect('invalid')
        return
      }
      const bytes = new TextEncoder().encode(event.data).length
      if (
        bytes > meetingRealtimeLimits.snapshotBytes + 8192 ||
        own.queue.length >= meetingRealtimeLimits.queued ||
        own.queuedBytes + bytes >
          meetingRealtimeLimits.snapshotBytes + 8192 * meetingRealtimeLimits.queued
      ) {
        disconnect('overflow')
        return
      }
      own.queue.push(event.data)
      own.queuedBytes += bytes
      void drain(own)
    }
  }
  return {
    clear,
    subscribe(value) {
      const topic = captureMeetingTopic(value),
        identity = ports.identity()
      if (!ports.current(identity)) throw new AccountFailure('cancelled')
      if (connection && !current(connection)) throw new AccountFailure('cancelled')
      if ([...entries.values()].some((entry) => !ports.current(entry.identity))) clear()
      if (entries.size >= meetingRealtimeLimits.subscriptions) throw new AccountFailure('busy')
      const entry: Entry = {
        id: newMeetingSubscriptionID(),
        identity,
        topic,
        active: true,
        acknowledged: false,
        awaiting: false,
        again: false,
        resnapshots: 0,
        latest: null,
        requested: null,
        state: shallowReactive({ phase: 'connecting', reason: '', snapshot: null }),
      }
      entries.set(entry.id, entry)
      if (!connection) connect()
      else if (connection.socket.readyState === 1 && send(connection, 'subscribe', entry))
        deadline(entry)
      return {
        state: readonly(entry.state),
        refresh() {
          if (!entry.active) return
          if (!connection) connect()
          else if (current(connection)) requestSnapshot(connection, entry)
        },
        dispose() {
          if (!entry.active) return
          entry.active = false
          stopTimer(entry)
          entry.state.phase = 'closed'
          entry.state.snapshot = null
          if (![...entries.values()].some((value) => value.active)) disconnect('disconnected', true)
          else if (connection?.socket.readyState === 1 && send(connection, 'unsubscribe', entry))
            deadline(entry)
          else entries.delete(entry.id)
        },
      }
    },
  }
}
