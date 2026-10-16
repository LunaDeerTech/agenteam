import { createHash, webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  captureMeetingTopic,
  decodeMeetingRealtime,
  meetingRealtimeClientMessage,
  newMeetingSubscriptionID,
} from '../api/meeting-realtime'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const topic = { kind: 'meeting.timeline' as const, project_id: id(1), meeting_id: id(2) }
const watermark = { epoch: id(3), sequence: '9007199254740993' }
const at = '2026-10-15T00:00:00.000000Z'
// Fixed canonical-v1 bytes, including Go's HTML and line-separator escapes.
const content =
  '{"decisions":[],"participants":[],"references":[],"session":{"created_at":"' +
  at +
  '","deleting":false,"id":"' +
  id(2) +
  '","project_id":"' +
  id(1) +
  '","status":"active","summary":null,"title":"\\u003c\\u0026\\u003e\\u2028","updated_at":"' +
  at +
  '","version":"1"},"turns":[]}'
const snapshot = {
  topic,
  schema: 'meeting_timeline_v1',
  content: JSON.parse(content),
  digest: 'sha256:' + createHash('sha256').update(content).digest('hex'),
}
beforeEach(() => vi.stubGlobal('crypto', webcrypto))
afterEach(() => vi.unstubAllGlobals())
describe('Meeting realtime closed wire', () => {
  it('constructs exact UUIDv7 subscription messages and omits every unused field', () => {
    const subscription = newMeetingSubscriptionID()
    expect(subscription).toMatch(/^[0-9a-f-]{14}7[0-9a-f-]{4}[89ab][0-9a-f-]{16}$/)
    expect(JSON.parse(meetingRealtimeClientMessage('subscribe', subscription, topic))).toEqual({
      type: 'subscribe',
      subscription_id: subscription,
      topic,
    })
    expect(JSON.parse(meetingRealtimeClientMessage('snapshot', subscription))).toEqual({
      type: 'snapshot',
      subscription_id: subscription,
    })
    expect(() => meetingRealtimeClientMessage('unsubscribe', subscription, topic)).toThrow()
    expect(() => captureMeetingTopic({ ...topic, generation: '0' })).toThrow()
    expect(() => captureMeetingTopic({ ...topic, kind: 'meeting.execution' })).toThrow()
  })
  it('validates the original Detail snapshot and its canonical bytes independently of the transport watermark', async () => {
    const message = await decodeMeetingRealtime(
      JSON.stringify({ type: 'snapshot', subscription_id: id(4), topic, watermark, snapshot }),
    )
    expect(message.type).toBe('snapshot')
    if (message.type !== 'snapshot') throw new Error('wrong message')
    expect(message.watermark.sequence).toBe('9007199254740993')
    expect(message.snapshot.content).toMatchObject({
      session: { version: '1', title: '<&>\u2028' },
    })
  })
  it.each([
    {
      type: 'subscribed',
      subscription_id: id(4),
      topic: { ...topic, Project_id: id(1) },
      watermark,
    },
    { type: 'subscribed', subscription_id: id(4), topic, watermark: { ...watermark, sequence: 1 } },
    { type: 'unsubscribed', subscription_id: id(4), topic: null },
    { type: 'resync', subscription_id: id(4), topic, watermark, reason: 'retry-forever' },
    {
      type: 'snapshot',
      subscription_id: id(4),
      topic,
      watermark,
      snapshot: { ...snapshot, digest: 'sha256:' + '0'.repeat(64) },
    },
    {
      type: 'snapshot',
      subscription_id: id(4),
      topic,
      watermark,
      snapshot: { ...snapshot, topic: { ...topic, meeting_id: id(8) } },
    },
  ])('rejects malformed envelope, scope or digest', async (message) => {
    await expect(decodeMeetingRealtime(JSON.stringify(message))).rejects.toThrow()
  })
  it('rejects duplicate decoded members before a discriminator can be replaced', async () => {
    await expect(
      decodeMeetingRealtime(
        `{"type":"unsubscribed","type":"unsubscribed","subscription_id":"${id(4)}"}`,
      ),
    ).rejects.toThrow()
  })
})
