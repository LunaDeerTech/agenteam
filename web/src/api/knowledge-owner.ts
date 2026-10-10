import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type KnowledgeCreator =
  | Readonly<{ kind: 'human'; user_id: string }>
  | Readonly<{ kind: 'agent_run'; project_id: string; agent_id: string; execution_id: string }>
export type KnowledgeDocument = Readonly<{
  id: string
  project_id: string
  parent_document_id: string | null
  title: string
  content_version: string
  source_kind: 'text' | 'file'
  media_type: string
  status: 'active'
  indexing_status: 'pending' | 'processing' | 'ready' | 'failed'
  created_by: KnowledgeCreator
  created_at: string
  updated_at: string
}>
export type KnowledgeHead =
  | Readonly<{ active: KnowledgeDocument }>
  | Readonly<{
      deleted: Readonly<{
        id: string
        project_id: string
        content_version: string
        deleted_at: string
      }>
    }>
export type KnowledgePage = Readonly<{
  items: readonly KnowledgeDocument[]
  next_cursor?: string
}>
export type KnowledgeQuery = Readonly<{ limit: number; cursor?: string }>
export type KnowledgeReadRequest = Readonly<{ byte_offset?: string; max_bytes?: number }>
export type KnowledgeContent = Readonly<{ document: KnowledgeDocument }> &
  (
    | Readonly<{ text: Readonly<{ text: string; next_byte_offset: string; truncated: boolean }> }>
    | Readonly<{ unavailable: 'dependency_unbound' }>
  )
export interface KnowledgeOwnerAPI {
  children(
    projectID: string,
    parentID: string | null,
    query: KnowledgeQuery,
    signal: AbortSignal,
  ): Promise<KnowledgePage>
  get(projectID: string, documentID: string, signal: AbortSignal): Promise<KnowledgeHead>
  ancestors(
    projectID: string,
    documentID: string,
    signal: AbortSignal,
  ): Promise<readonly KnowledgeDocument[]>
  readContent(
    projectID: string,
    documentID: string,
    request: KnowledgeReadRequest,
    signal: AbortSignal,
  ): Promise<KnowledgeContent>
}

// knowledge/contract.DefaultReadRequest and the public content query contract.
export const knowledgeDefaultReadBytes = 65536
export const knowledgeMaximumReadBytes = 1048576
const maximumInteger = 9223372036854775807n
const encoder = new TextEncoder()
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(read: () => T): T {
  try {
    return read()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function id(value: unknown): string {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function unicode(value: unknown, minimum: number, maximum: number): string {
  const result = string(value, minimum, maximum)
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
function decimal(value: unknown, positive = false): string {
  const result = string(value, 1, 19)
  requireValue(
    /^(0|[1-9][0-9]*)$/.test(result) &&
      BigInt(result) <= maximumInteger &&
      (!positive || result !== '0'),
  )
  return result
}
function cursor(value: unknown): string {
  const result = unicode(value, 1, 8192)
  requireValue(
    encoder.encode(result).byteLength <= 8192 && !/[\u0000-\u001f\u007f-\u009f]/u.test(result),
  )
  return result
}
export const captureKnowledgeID = (value: unknown): string => input(() => id(value))
export function captureKnowledgeQuery(value: unknown): KnowledgeQuery {
  return input(() => {
    const v = shape(value, ['limit'], ['cursor']),
      limit = v.limit
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 200)
    return Object.freeze({
      limit,
      ...(Object.hasOwn(v, 'cursor') ? { cursor: cursor(v.cursor) } : {}),
    })
  })
}
export function captureKnowledgeReadRequest(value: unknown): Required<KnowledgeReadRequest> {
  return input(() => {
    const v = shape(value, [], ['byte_offset', 'max_bytes'])
    const byte_offset = Object.hasOwn(v, 'byte_offset') ? decimal(v.byte_offset) : '0'
    const max_bytes = Object.hasOwn(v, 'max_bytes') ? v.max_bytes : knowledgeDefaultReadBytes
    requireValue(
      typeof max_bytes === 'number' &&
        Number.isInteger(max_bytes) &&
        max_bytes >= 1 &&
        max_bytes <= knowledgeMaximumReadBytes,
    )
    return Object.freeze({ byte_offset, max_bytes })
  })
}
function creator(value: unknown, projectID: string): KnowledgeCreator {
  const v = shape(value, ['kind'], ['user_id', 'project_id', 'agent_id', 'execution_id'])
  if (v.kind === 'human') {
    shape(v, ['kind', 'user_id'])
    return Object.freeze({ kind: 'human', user_id: id(v.user_id) })
  }
  requireValue(v.kind === 'agent_run')
  shape(v, ['kind', 'project_id', 'agent_id', 'execution_id'])
  requireValue(id(v.project_id) === projectID)
  return Object.freeze({
    kind: 'agent_run',
    project_id: projectID,
    agent_id: id(v.agent_id),
    execution_id: id(v.execution_id),
  })
}
export function parseKnowledgeDocument(
  value: unknown,
  projectID: string,
  target?: string,
): KnowledgeDocument {
  const v = shape(value, [
    'id',
    'project_id',
    'parent_document_id',
    'title',
    'content_version',
    'source_kind',
    'media_type',
    'status',
    'indexing_status',
    'created_by',
    'created_at',
    'updated_at',
  ])
  const documentID = id(v.id),
    parent = v.parent_document_id === null ? null : id(v.parent_document_id)
  requireValue(
    id(v.project_id) === projectID &&
      (target === undefined || documentID === target) &&
      parent !== documentID,
  )
  const title = unicode(v.title, 1, 512)
  requireValue(!/[\u0000-\u001f\u007f-\u009f]/u.test(title))
  const source = v.source_kind,
    media = v.media_type
  requireValue(
    (source === 'text' && (media === 'text/plain' || media === 'text/markdown')) ||
      (source === 'file' &&
        (media === 'application/pdf' ||
          media === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document')),
  )
  requireValue(
    v.status === 'active' &&
      ['pending', 'processing', 'ready', 'failed'].includes(v.indexing_status as string),
  )
  const created = parseSystemInstant(v.created_at),
    updated = parseSystemInstant(v.updated_at)
  requireValue(created <= updated)
  return Object.freeze({
    id: documentID,
    project_id: projectID,
    parent_document_id: parent,
    title,
    content_version: decimal(v.content_version, true),
    source_kind: source as 'text' | 'file',
    media_type: media as string,
    status: 'active',
    indexing_status: v.indexing_status as KnowledgeDocument['indexing_status'],
    created_by: creator(v.created_by, projectID),
    created_at: created,
    updated_at: updated,
  })
}
// PostgreSQL title COLLATE C compares UTF-8 bytes, not UTF-16 code units or locale.
export function compareKnowledgeDocuments(a: KnowledgeDocument, b: KnowledgeDocument): number {
  const left = encoder.encode(a.title),
    right = encoder.encode(b.title)
  for (let i = 0; i < Math.min(left.length, right.length); i++) {
    if (left[i] !== right[i]) return left[i]! - right[i]!
  }
  return left.length - right.length || (a.id < b.id ? -1 : a.id === b.id ? 0 : 1)
}
function page(
  value: unknown,
  projectID: string,
  parentID: string | null,
  query: KnowledgeQuery,
): KnowledgePage {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= query.limit)
  const seen = new Set<string>()
  let previous: KnowledgeDocument | undefined
  const items = v.items.map((item) => {
    const document = parseKnowledgeDocument(item, projectID)
    requireValue(
      document.parent_document_id === parentID &&
        !seen.has(document.id) &&
        (!previous || compareKnowledgeDocuments(previous, document) < 0),
    )
    seen.add(document.id)
    previous = document
    return document
  })
  const next = Object.hasOwn(v, 'next_cursor') ? cursor(v.next_cursor) : undefined
  requireValue(next === undefined || (items.length === query.limit && next !== query.cursor))
  return Object.freeze({
    items: Object.freeze(items),
    ...(next === undefined ? {} : { next_cursor: next }),
  })
}
function head(value: unknown, projectID: string, target: string): KnowledgeHead {
  const v = shape(value, [], ['active', 'deleted'])
  requireValue(Object.keys(v).length === 1)
  if (Object.hasOwn(v, 'active'))
    return Object.freeze({ active: parseKnowledgeDocument(v.active, projectID, target) })
  const d = shape(v.deleted, ['id', 'project_id', 'content_version', 'deleted_at'])
  requireValue(id(d.id) === target && id(d.project_id) === projectID)
  return Object.freeze({
    deleted: Object.freeze({
      id: target,
      project_id: projectID,
      content_version: decimal(d.content_version, true),
      deleted_at: parseSystemInstant(d.deleted_at),
    }),
  })
}
function ancestors(
  value: unknown,
  projectID: string,
  target: string,
): readonly KnowledgeDocument[] {
  const v = shape(value, ['items'])
  requireValue(Array.isArray(v.items))
  const seen = new Set([target])
  let parent: string | null = null
  return Object.freeze(
    v.items.map((item) => {
      const document = parseKnowledgeDocument(item, projectID)
      requireValue(!seen.has(document.id) && document.parent_document_id === parent)
      seen.add(document.id)
      parent = document.id
      return document
    }),
  )
}
function content(
  value: unknown,
  projectID: string,
  target: string,
  request: Required<KnowledgeReadRequest>,
): KnowledgeContent {
  const v = shape(value, ['document'], ['text', 'unavailable'])
  requireValue(Object.keys(v).length === 2)
  const document = parseKnowledgeDocument(v.document, projectID, target)
  if (Object.hasOwn(v, 'unavailable')) {
    requireValue(document.source_kind === 'file' && v.unavailable === 'dependency_unbound')
    return Object.freeze({ document, unavailable: 'dependency_unbound' as const })
  }
  requireValue(document.source_kind === 'text')
  const t = shape(v.text, ['text', 'next_byte_offset', 'truncated'])
  const text = unicode(t.text, 0, request.max_bytes),
    next = decimal(t.next_byte_offset)
  const bytes = encoder.encode(text).byteLength
  requireValue(
    bytes <= request.max_bytes &&
      BigInt(next) === BigInt(request.byte_offset) + BigInt(bytes) &&
      typeof t.truncated === 'boolean',
  )
  return Object.freeze({
    document,
    text: Object.freeze({ text, next_byte_offset: next, truncated: t.truncated }),
  })
}
export function createKnowledgeOwnerAPI(fetcher?: Fetch): KnowledgeOwnerAPI {
  const request = accountTransport(fetcher)
  return {
    async children(projectID, parentID, query, signal) {
      const project = captureKnowledgeID(projectID),
        parent = parentID === null ? null : captureKnowledgeID(parentID),
        captured = captureKnowledgeQuery(query)
      return request('knowledgeChildren', (value) => page(value, project, parent, captured), {
        signal,
        projectID: project,
        knowledgeChildren: { parent_document_id: parent, ...captured },
      })
    },
    async get(projectID, documentID, signal) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID)
      return request('knowledgeDocument', (value) => head(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    async ancestors(projectID, documentID, signal) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID)
      return request('knowledgeAncestors', (value) => ancestors(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    async readContent(projectID, documentID, input, signal) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID),
        captured = captureKnowledgeReadRequest(input)
      return request('knowledgeContent', (value) => content(value, project, target, captured), {
        signal,
        projectID: project,
        target,
        knowledgeContent: captured,
      })
    },
  }
}
