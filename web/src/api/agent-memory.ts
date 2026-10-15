import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { workID, workVersion } from './work-review'
import { parseSystemInstant } from './system-account'

export type MemoryRevision = Readonly<{
  memory_id: string
  revision: string
  status: 'active' | 'superseded' | 'deleted'
  type: 'fact' | 'experience' | 'procedure' | 'observation'
  text: string
  tags: readonly string[]
  importance: 'low' | 'normal' | 'high'
  created_at: string
  provenance: Readonly<{
    execution_id: string
    operation_id: string
    derived_from_memory_ids: readonly string[]
  }>
}>
export type MemoryRecord = Readonly<{
  namespace: Readonly<{ project_id: string; agent_id: string }>
  current: MemoryRevision
  updated_at: string
}>
export type MemoryPage = Readonly<{ memories: readonly MemoryRecord[]; next_cursor?: string }>
export type MemoryHistory = Readonly<{ revisions: readonly MemoryRevision[]; next_cursor?: string }>
export type MemoryDelete = Readonly<{ memory_id: string; expected_revision: string }>
export type MemoryReceipt = Readonly<{ memory_id: string; revision: string; deleted: true }>
export type MemoryLookup = Readonly<
  { status: 'committed'; receipt: MemoryReceipt } | { status: 'not_found' }
>
export type MemoryQuery = Readonly<{ limit?: number; cursor?: string }>
export interface AgentMemoryAPI {
  list(project: string, agent: string, query: MemoryQuery, signal: AbortSignal): Promise<MemoryPage>
  history(
    project: string,
    agent: string,
    memory: string,
    query: MemoryQuery,
    signal: AbortSignal,
  ): Promise<MemoryHistory>
  delete(
    project: string,
    agent: string,
    input: MemoryDelete,
    write: WriteOptions,
  ): Promise<MemoryReceipt>
  lookup(
    project: string,
    agent: string,
    input: MemoryDelete,
    write: WriteOptions,
  ): Promise<MemoryLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function list<T>(value: unknown, maximum: number, parse: (v: unknown) => T): readonly T[] {
  requireValue(Array.isArray(value) && value.length <= maximum)
  return Object.freeze(value.map(parse))
}
export function captureMemoryDelete(value: unknown): MemoryDelete {
  try {
    const v = shape(value, ['memory_id', 'expected_revision'])
    return Object.freeze({
      memory_id: workID(v.memory_id),
      expected_revision: workVersion(v.expected_revision),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function revision(value: unknown): MemoryRevision {
  const v = shape(value, [
    'memory_id',
    'revision',
    'status',
    'type',
    'text',
    'tags',
    'importance',
    'provenance',
    'created_at',
  ])
  requireValue(
    ['active', 'superseded', 'deleted'].includes(String(v.status)) &&
      ['fact', 'experience', 'procedure', 'observation'].includes(String(v.type)) &&
      ['low', 'normal', 'high'].includes(String(v.importance)),
  )
  const p = shape(v.provenance, ['execution_id', 'operation_id', 'derived_from_memory_ids']),
    tags = list(v.tags, 16, (v) => string(v, 1, 64)),
    derived = list(p.derived_from_memory_ids, 32, workID)
  requireValue(new Set(tags).size === tags.length && new Set(derived).size === derived.length)
  const text = string(v.text, 1, 8192)
  requireValue(new TextEncoder().encode(text).byteLength <= 8192)
  return Object.freeze({
    memory_id: workID(v.memory_id),
    revision: workVersion(v.revision),
    status: v.status as MemoryRevision['status'],
    type: v.type as MemoryRevision['type'],
    text,
    tags,
    importance: v.importance as MemoryRevision['importance'],
    created_at: parseSystemInstant(v.created_at),
    provenance: Object.freeze({
      execution_id: workID(p.execution_id),
      operation_id: workID(p.operation_id),
      derived_from_memory_ids: derived,
    }),
  })
}
function receipt(value: unknown, input: MemoryDelete): MemoryReceipt {
  const v = shape(value, ['memory_id', 'revision', 'deleted'])
  requireValue(
    workID(v.memory_id) === input.memory_id &&
      workVersion(v.revision) === input.expected_revision &&
      v.deleted === true,
  )
  return Object.freeze({
    memory_id: input.memory_id,
    revision: input.expected_revision,
    deleted: true,
  })
}
export function createAgentMemoryAPI(fetcher?: Fetch): AgentMemoryAPI {
  const call = accountTransport(fetcher)
  const ids = (p: string, a: string) => ({ projectID: workID(p), target: workID(a) })
  const query = (q: MemoryQuery) => ({
    limit: q.limit ?? 50,
    ...(q.cursor ? { cursor: q.cursor } : {}),
  })
  return {
    list(project, agent, q, signal) {
      return call(
        'agentMemoryList',
        (value) => {
          const v = shape(value, ['memories'], ['next_cursor'])
          const memories = list(v.memories, q.limit ?? 50, (value) => {
            const row = shape(value, ['namespace', 'current', 'updated_at']),
              ns = shape(row.namespace, ['project_id', 'agent_id']),
              current = revision(row.current)
            requireValue(
              workID(ns.project_id) === project &&
                workID(ns.agent_id) === agent &&
                current.status === 'active',
            )
            return Object.freeze({
              namespace: Object.freeze({ project_id: project, agent_id: agent }),
              current,
              updated_at: parseSystemInstant(row.updated_at),
            })
          })
          requireValue(new Set(memories.map((v) => v.current.memory_id)).size === memories.length)
          return Object.freeze({
            memories,
            ...(v.next_cursor === undefined ? {} : { next_cursor: workID(v.next_cursor) }),
          })
        },
        { ...ids(project, agent), memoryQuery: query(q), signal },
      )
    },
    history(project, agent, memory, q, signal) {
      return call(
        'agentMemoryHistory',
        (value) => {
          const v = shape(value, ['revisions'], ['next_cursor']),
            revisions = list(v.revisions, q.limit ?? 50, revision)
          requireValue(
            revisions.every((v) => v.memory_id === memory) &&
              new Set(revisions.map((v) => v.revision)).size === revisions.length,
          )
          return Object.freeze({
            revisions,
            ...(v.next_cursor === undefined ? {} : { next_cursor: workVersion(v.next_cursor) }),
          })
        },
        { ...ids(project, agent), memoryID: workID(memory), memoryQuery: query(q), signal },
      )
    },
    delete(project, agent, value, write) {
      const input = captureMemoryDelete(value)
      return call('agentMemoryDelete', (value) => receipt(value, input), {
        ...ids(project, agent),
        memoryID: input.memory_id,
        body: { expected_revision: input.expected_revision },
        csrf: write.csrfToken,
        key: write.key,
        signal: write.signal ?? new AbortController().signal,
      })
    },
    lookup(project, agent, value, write) {
      const input = captureMemoryDelete(value)
      return call(
        'agentMemoryDeleteLookup',
        (value) => {
          const v = shape(value, ['status'], ['receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed' as const,
              receipt: receipt(v.receipt, input),
            })
          requireValue(v.status === 'not_found' && !Object.hasOwn(v, 'receipt'))
          return Object.freeze({ status: 'not_found' as const })
        },
        {
          ...ids(project, agent),
          memoryID: input.memory_id,
          body: { expected_revision: input.expected_revision },
          csrf: write.csrfToken,
          key: write.key,
          signal: write.signal ?? new AbortController().signal,
        },
      )
    },
  }
}
