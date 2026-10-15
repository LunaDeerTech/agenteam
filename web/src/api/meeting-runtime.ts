import { AccountFailure, shape, string } from './client'
import type { MeetingContributionExecution } from './meetings'
import { workID, workVersion } from './work-review'

type Status = MeetingContributionExecution['status']
type ItemIdentity = Readonly<{
  id: string
  seq: string
  revision: string
  transcript_sequence: string
}>
export type MeetingRuntimeTool = Readonly<{
  call_id: string
  name: string
  result_available: boolean
  result_sequence: string
  is_error: boolean
  result_text: string
  result_text_bytes: string
  text_truncated: boolean
  image_parts: number
  file_parts: number
}>
export type MeetingRuntimeItem = ItemIdentity &
  Readonly<
    | { type: 'state'; state: Readonly<{ status: Status }> }
    | { type: 'waiting'; waiting: Readonly<{ reason: 'approval' | 'decision'; reference: string }> }
    | { type: 'text'; round_id: string; text: Readonly<{ content: string }> }
    | { type: 'tool'; round_id: string; tool: MeetingRuntimeTool }
  >
export type MeetingRuntimeProjection = Readonly<{
  schema: 'execution_public_runtime_v1'
  execution_id: string
  project_id: string
  execution_version: string
  status: Status
  transcript_through: string
  items: readonly MeetingRuntimeItem[]
}>

function check(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
export function runtimeProgress(value: unknown): string {
  const result = string(value, 1, 19)
  check(/^(0|[1-9][0-9]*)$/.test(result) && BigInt(result) <= 9223372036854775807n)
  return result
}
function text(value: unknown, maximum: number) {
  const result = string(value, 0, maximum)
  const bytes = new TextEncoder().encode(result)
  check(bytes.length <= maximum && new TextDecoder().decode(bytes) === result)
  return result
}
function status(value: unknown): Status {
  check(
    typeof value === 'string' &&
      ['created', 'preparing', 'running', 'waiting', 'succeeded', 'failed', 'cancelled'].includes(
        value,
      ),
  )
  return value as Status
}
function bool(value: unknown): boolean {
  check(typeof value === 'boolean')
  return value
}
function count(value: unknown): number {
  check(typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 256)
  return value
}
function tool(value: unknown, through: bigint): MeetingRuntimeTool {
  const v = shape(value, [
    'call_id',
    'name',
    'result_available',
    'result_sequence',
    'is_error',
    'result_text',
    'result_text_bytes',
    'text_truncated',
    'image_parts',
    'file_parts',
  ])
  const result = Object.freeze({
    call_id: text(v.call_id, 256),
    name: text(v.name, 256),
    result_available: bool(v.result_available),
    result_sequence: runtimeProgress(v.result_sequence),
    is_error: bool(v.is_error),
    result_text: text(v.result_text, 8192),
    result_text_bytes: runtimeProgress(v.result_text_bytes),
    text_truncated: bool(v.text_truncated),
    image_parts: count(v.image_parts),
    file_parts: count(v.file_parts),
  })
  const bytes = BigInt(new TextEncoder().encode(result.result_text).length)
  check(
    result.call_id.trim() !== '' &&
      result.name.trim() !== '' &&
      result.image_parts + result.file_parts <= 256,
  )
  check(
    BigInt(result.result_sequence) <= through &&
      BigInt(result.result_text_bytes) >= bytes &&
      result.text_truncated === BigInt(result.result_text_bytes) > bytes,
  )
  if (!result.result_available)
    check(
      result.result_sequence === '0' &&
        !result.is_error &&
        result.result_text === '' &&
        result.result_text_bytes === '0' &&
        !result.text_truncated &&
        result.image_parts === 0 &&
        result.file_parts === 0,
    )
  return result
}

// Public committed Execution facts only. Topic tuple authorization belongs to
// the original Meeting reader; neither these IDs nor a previous snapshot grant it.
export function parseMeetingRuntime(
  value: unknown,
  project: string,
  execution: string,
): MeetingRuntimeProjection {
  const v = shape(value, [
    'schema',
    'execution_id',
    'project_id',
    'execution_version',
    'status',
    'transcript_through',
    'items',
  ])
  check(
    v.schema === 'execution_public_runtime_v1' &&
      workID(v.project_id) === project &&
      workID(v.execution_id) === execution,
  )
  const version = workVersion(v.execution_version),
    state = status(v.status),
    progress = runtimeProgress(v.transcript_through),
    through = BigInt(progress)
  check(through <= 4096n && Array.isArray(v.items) && v.items.length > 0 && v.items.length <= 1024)
  const ids = new Set<string>()
  let previous = 0n,
    waiting = 0,
    bytes = 0
  const items = v.items.map((raw, index): MeetingRuntimeItem => {
    const x = shape(
      raw,
      ['id', 'seq', 'revision', 'type', 'transcript_sequence'],
      ['round_id', 'state', 'waiting', 'tool', 'text'],
    )
    const identity = {
      id: text(x.id, 512),
      seq: workVersion(x.seq),
      revision: workVersion(x.revision),
      transcript_sequence: runtimeProgress(x.transcript_sequence),
    }
    const seq = BigInt(identity.seq),
      transcript = BigInt(identity.transcript_sequence)
    check(identity.id !== '' && !ids.has(identity.id) && seq > previous && transcript <= through)
    ids.add(identity.id)
    previous = seq
    if (x.type === 'state') {
      shape(x, ['id', 'seq', 'revision', 'type', 'transcript_sequence', 'state'])
      const data = shape(x.state, ['status'])
      check(
        index === 0 &&
          identity.id === 'state' &&
          seq === 1n &&
          identity.revision === version &&
          transcript === 0n &&
          data.status === state,
      )
      return Object.freeze({ ...identity, type: 'state', state: Object.freeze({ status: state }) })
    }
    if (x.type === 'waiting') {
      shape(x, ['id', 'seq', 'revision', 'type', 'transcript_sequence', 'waiting'])
      const data = shape(x.waiting, ['reason', 'reference']),
        reference = workID(data.reference)
      check(
        index === 1 &&
          seq === 2n &&
          identity.revision === version &&
          state === 'waiting' &&
          transcript === 0n &&
          identity.id === 'waiting/' + reference &&
          (data.reason === 'approval' || data.reason === 'decision'),
      )
      waiting++
      return Object.freeze({
        ...identity,
        type: 'waiting',
        waiting: Object.freeze({ reason: data.reason, reference }),
      })
    }
    check(x.type === 'text' || x.type === 'tool')
    shape(x, ['id', 'seq', 'revision', 'type', 'transcript_sequence', 'round_id', x.type])
    const round = workID(x.round_id),
      prefix = 'round/' + round + '/',
      part = identity.id.slice(prefix.length)
    check(
      index > 0 &&
        identity.id.startsWith(prefix) &&
        /^(0|[1-9][0-9]*)$/.test(part) &&
        Number(part) < 256 &&
        transcript > 0n &&
        seq === transcript * 257n + BigInt(part) + 3n,
    )
    if (x.type === 'text') {
      const data = shape(x.text, ['content']),
        content = text(data.content, 16 << 20)
      check(identity.revision === '1')
      bytes += new TextEncoder().encode(content).length
      check(bytes <= 16 << 20)
      return Object.freeze({
        ...identity,
        type: 'text',
        round_id: round,
        text: Object.freeze({ content }),
      })
    }
    const data = tool(x.tool, through)
    check(
      (identity.revision === '1' || identity.revision === '2') &&
        (identity.revision === '2') === data.result_available &&
        (!data.result_available || BigInt(data.result_sequence) > transcript),
    )
    bytes += new TextEncoder().encode(data.result_text).length
    check(bytes <= 16 << 20)
    return Object.freeze({ ...identity, type: 'tool', round_id: round, tool: data })
  })
  check(items[0]!.type === 'state' && waiting === (state === 'waiting' ? 1 : 0))
  return Object.freeze({
    schema: 'execution_public_runtime_v1',
    project_id: project,
    execution_id: execution,
    execution_version: version,
    status: state,
    transcript_through: progress,
    items: Object.freeze(items),
  })
}
