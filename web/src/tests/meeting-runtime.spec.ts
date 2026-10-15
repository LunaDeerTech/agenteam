import { describe, expect, it } from 'vitest'
import { parseMeetingRuntime } from '../api/meeting-runtime'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
function projection() {
  return {
    schema: 'execution_public_runtime_v1',
    execution_id: id(1),
    project_id: id(2),
    execution_version: '9007199254740993',
    status: 'running',
    transcript_through: '4',
    items: [
      {
        id: 'state',
        seq: '1',
        revision: '9007199254740993',
        type: 'state',
        transcript_sequence: '0',
        state: { status: 'running' },
      },
      {
        id: `round/${id(3)}/0`,
        seq: '260',
        revision: '2',
        type: 'tool',
        round_id: id(3),
        transcript_sequence: '1',
        tool: {
          call_id: 'original-call',
          name: 'list-meetings',
          result_available: true,
          result_sequence: '2',
          is_error: false,
          result_text: '中',
          result_text_bytes: '6',
          text_truncated: true,
          image_parts: 1,
          file_parts: 0,
        },
      },
      {
        id: `round/${id(4)}/3`,
        seq: '1034',
        revision: '1',
        type: 'text',
        round_id: id(4),
        transcript_sequence: '4',
        text: { content: 'Committed result.' },
      },
    ],
  }
}
describe('Meeting public committed runtime projection', () => {
  it('retains large versions, sparse sequences and committed Tool/text identities', () => {
    const result = parseMeetingRuntime(projection(), id(2), id(1))
    expect(result.execution_version).toBe('9007199254740993')
    expect(result.items.map((item) => item.seq)).toEqual(['1', '260', '1034'])
    expect(result.transcript_through).toBe('4')
    expect(result.items[1]).toMatchObject({
      tool: { result_text_bytes: '6', text_truncated: true },
    })
    expect(Object.isFrozen(result.items)).toBe(true)
  })
  it('binds waiting to the same current Execution state without inventing stream progress', () => {
    const value = projection()
    const pending = {
      ...value,
      status: 'waiting',
      transcript_through: '0',
      items: [
        { ...value.items[0], state: { status: 'waiting' } },
        {
          id: 'waiting/' + id(5),
          seq: '2',
          revision: value.execution_version,
          type: 'waiting',
          transcript_sequence: '0',
          waiting: { reason: 'decision', reference: id(5) },
        },
      ],
    }
    expect(parseMeetingRuntime(pending, id(2), id(1)).items[1]).toMatchObject({ type: 'waiting' })
    expect(() => parseMeetingRuntime({ ...pending, status: 'running' }, id(2), id(1))).toThrow()
  })
  it.each([
    [
      'foreign execution',
      (v: any) => {
        v.execution_id = id(9)
      },
    ],
    [
      'foreign project',
      (v: any) => {
        v.project_id = id(9)
      },
    ],
    [
      'uncommitted token',
      (v: any) => {
        v.items[2].type = 'token'
      },
    ],
    [
      'private metadata',
      (v: any) => {
        v.items[1].tool.arguments = 'private'
      },
    ],
    [
      'rounded version',
      (v: any) => {
        v.execution_version = 9007199254740992
      },
    ],
    [
      'duplicate identity',
      (v: any) => {
        v.items.push(v.items[2])
      },
    ],
    [
      'fabricated contiguous seq',
      (v: any) => {
        v.items[2].seq = '3'
      },
    ],
    [
      'future transcript',
      (v: any) => {
        v.transcript_through = '3'
      },
    ],
    [
      'result before call',
      (v: any) => {
        v.items[1].tool.result_sequence = '1'
      },
    ],
    [
      'mismatched Tool revision',
      (v: any) => {
        v.items[1].revision = '1'
      },
    ],
    [
      'false truncation',
      (v: any) => {
        v.items[1].tool.text_truncated = false
      },
    ],
    [
      'multiple item shapes',
      (v: any) => {
        v.items[2].state = { status: 'running' }
      },
    ],
  ])('rejects %s', (_, change) => {
    const value = projection()
    change(value)
    expect(() => parseMeetingRuntime(value, id(2), id(1))).toThrow()
  })
})
