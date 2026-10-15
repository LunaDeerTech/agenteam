import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type ArtifactRef = Readonly<{ project_id: string; artifact_id: string; file_id: string }>
export type ArtifactKind = 'generated' | 'user_upload'
export type ArtifactMetadata = Readonly<{
  artifact_ref: ArtifactRef
  kind: ArtifactKind
  name: string
  description?: string
  object: Readonly<{ media_type: string; byte_size: string; sha256: string }>
  source_execution_id?: string
  source_operation_id?: string
  created_by_user_id?: string
  created_by_agent_id?: string
  created_at: string
  version: string
}>
export type ArtifactQuery = Readonly<{
  execution_id?: string
  kind?: ArtifactKind
  media_type?: string
  name_query?: string
  limit?: number
  cursor?: string
}>
export type CapturedArtifactQuery = Readonly<ArtifactQuery & { limit: number }>
export type ArtifactPage = Readonly<{
  items: readonly ArtifactMetadata[]
  next_cursor: string | null
}>
export type ArtifactReadQuery = Readonly<{ offset?: string; limit?: number }>
export type CapturedArtifactReadQuery = Readonly<{ offset: string; limit: number }>
export type ArtifactContent = Readonly<{
  artifact: ArtifactMetadata
  projection: 'text' | 'image_ref' | 'file_ref'
  text: string
  offset: string
  next_offset: string
  truncated: boolean
  invalid_utf8: boolean
}>
export interface ProjectArtifactsAPI {
  list(projectID: string, query: ArtifactQuery, signal: AbortSignal): Promise<ArtifactPage>
  read(ref: ArtifactRef, query: ArtifactReadQuery, signal: AbortSignal): Promise<ArtifactContent>
}
export interface ProjectArtifactMetadataAPI {
  metadata(projectID: string, artifactID: string, signal: AbortSignal): Promise<ArtifactMetadata>
}
const encoder = new TextEncoder()
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function id(value: unknown) {
  const result = string(value, 36, 36)
  requireValue(uuid7.test(result))
  return result
}
function text(value: unknown, maximum: number, minimum = 1) {
  const result = string(value, minimum, maximum)
  requireValue(
    encoder.encode(result).byteLength <= maximum &&
      !result.includes('\0') &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
function mediaType(value: unknown) {
  const result = text(value, 256)
  requireValue(
    !/[\u0000-\u001f\u007f]/.test(result) &&
      /^[!#$%&'*+.^_`|~{}0-9a-z-]+(?:\/[!#$%&'*+.^_`|~{}0-9a-z-]+)?(?:; .+)?$/.test(result),
  )
  return result
}
function integer(value: unknown, maximum = 9223372036854775807n, minimum = 0n) {
  const result = string(value, 1, 19)
  requireValue(
    /^(0|[1-9][0-9]*)$/.test(result) && BigInt(result) <= maximum && BigInt(result) >= minimum,
  )
  return result
}
function kind(value: unknown): ArtifactKind {
  requireValue(value === 'generated' || value === 'user_upload')
  return value
}
function ref(value: unknown): ArtifactRef {
  const raw = shape(value, ['project_id', 'artifact_id', 'file_id'])
  return Object.freeze({
    project_id: id(raw.project_id),
    artifact_id: id(raw.artifact_id),
    file_id: id(raw.file_id),
  })
}
export const sameArtifactRef = (a: ArtifactRef, b: ArtifactRef) =>
  a.project_id === b.project_id && a.artifact_id === b.artifact_id && a.file_id === b.file_id
export function captureArtifactID(value: unknown) {
  try {
    return id(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureArtifactRef(value: unknown) {
  try {
    return ref(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureArtifactQuery(value: unknown): CapturedArtifactQuery {
  try {
    const raw = shape(
      value,
      [],
      ['execution_id', 'kind', 'media_type', 'name_query', 'limit', 'cursor'],
    )
    const limit = Object.hasOwn(raw, 'limit') ? raw.limit : 50
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 200)
    return Object.freeze({
      limit,
      ...(Object.hasOwn(raw, 'execution_id') ? { execution_id: id(raw.execution_id) } : {}),
      ...(Object.hasOwn(raw, 'kind') ? { kind: kind(raw.kind) } : {}),
      ...(Object.hasOwn(raw, 'media_type') ? { media_type: mediaType(raw.media_type) } : {}),
      ...(Object.hasOwn(raw, 'name_query') ? { name_query: text(raw.name_query, 256) } : {}),
      ...(Object.hasOwn(raw, 'cursor') ? { cursor: text(raw.cursor, 8192) } : {}),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function captureArtifactReadQuery(value: unknown): CapturedArtifactReadQuery {
  try {
    const raw = shape(value, [], ['offset', 'limit'])
    const limit = Object.hasOwn(raw, 'limit') ? raw.limit : 8192
    requireValue(
      typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 65536,
    )
    return Object.freeze({
      offset: Object.hasOwn(raw, 'offset') ? integer(raw.offset, 1073741824n) : '0',
      limit,
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function decodeArtifactMetadata(value: unknown): ArtifactMetadata {
  const optional = [
    'description',
    'source_execution_id',
    'source_operation_id',
    'created_by_user_id',
    'created_by_agent_id',
  ] as const
  const raw = shape(
    value,
    ['artifact_ref', 'kind', 'name', 'object', 'created_at', 'version'],
    optional,
  )
  const object = shape(raw.object, ['media_type', 'byte_size', 'sha256'])
  const sha = string(object.sha256, 71, 71)
  requireValue(/^sha256:[0-9a-f]{64}$/.test(sha))
  const ids = Object.fromEntries(
    optional
      .filter((key) => key !== 'description' && Object.hasOwn(raw, key))
      .map((key) => [key, id(raw[key])]),
  )
  requireValue(
    Object.hasOwn(ids, 'created_by_user_id') !== Object.hasOwn(ids, 'created_by_agent_id'),
  )
  requireValue(
    (!Object.hasOwn(ids, 'created_by_agent_id') && !Object.hasOwn(ids, 'source_operation_id')) ||
      Object.hasOwn(ids, 'source_execution_id'),
  )
  const name = text(raw.name, 255)
  requireValue(!/[\p{Cc}/\\]/u.test(name))
  return Object.freeze({
    artifact_ref: ref(raw.artifact_ref),
    kind: kind(raw.kind),
    name,
    ...(Object.hasOwn(raw, 'description') ? { description: text(raw.description, 4096) } : {}),
    object: Object.freeze({
      media_type: mediaType(object.media_type),
      byte_size: integer(object.byte_size, 1073741824n),
      sha256: sha,
    }),
    ...ids,
    created_at: parseSystemInstant(raw.created_at),
    version: integer(raw.version, undefined, 1n),
  })
}
export function compareArtifacts(a: ArtifactMetadata, b: ArtifactMetadata) {
  if (a.created_at !== b.created_at) return a.created_at > b.created_at ? -1 : 1
  return a.artifact_ref.artifact_id > b.artifact_ref.artifact_id
    ? -1
    : a.artifact_ref.artifact_id === b.artifact_ref.artifact_id
      ? 0
      : 1
}
export function decodeArtifactPage(
  value: unknown,
  projectID: string,
  query: CapturedArtifactQuery,
): ArtifactPage {
  const raw = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(raw.items) && raw.items.length <= query.limit)
  const items = raw.items.map(decodeArtifactMetadata)
  const ids = new Set<string>()
  items.forEach((item, index) => {
    requireValue(
      item.artifact_ref.project_id === projectID &&
        !ids.has(item.artifact_ref.artifact_id) &&
        (!query.kind || item.kind === query.kind) &&
        (!query.execution_id || item.source_execution_id === query.execution_id) &&
        (!query.media_type || item.object.media_type === query.media_type) &&
        (!query.name_query || item.name.includes(query.name_query)) &&
        (index === 0 || compareArtifacts(items[index - 1]!, item) < 0),
    )
    ids.add(item.artifact_ref.artifact_id)
  })
  const next = Object.hasOwn(raw, 'next_cursor') ? text(raw.next_cursor, 8192) : null
  requireValue(next === null || (items.length === query.limit && next !== query.cursor))
  return Object.freeze({ items: Object.freeze(items), next_cursor: next })
}
export function decodeArtifactContent(
  value: unknown,
  target: ArtifactRef,
  query: CapturedArtifactReadQuery,
): ArtifactContent {
  const raw = shape(
    value,
    ['artifact', 'projection', 'offset', 'next_offset', 'truncated'],
    ['text', 'invalid_utf8'],
  )
  const artifact = decodeArtifactMetadata(raw.artifact)
  const offset = integer(raw.offset, 1073741824n),
    next = integer(raw.next_offset, 1073741824n)
  requireValue(
    sameArtifactRef(artifact.artifact_ref, target) &&
      BigInt(query.offset) <= BigInt(artifact.object.byte_size) &&
      typeof raw.truncated === 'boolean' &&
      (!Object.hasOwn(raw, 'invalid_utf8') || raw.invalid_utf8 === true),
  )
  const content = Object.hasOwn(raw, 'text') ? string(raw.text, 1, 65536) : ''
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(content),
  )
  const bytes = encoder.encode(content).byteLength
  const projection = raw.projection
  requireValue(projection === 'text' || projection === 'image_ref' || projection === 'file_ref')
  if (projection === 'text') {
    requireValue(
      offset === query.offset &&
        BigInt(next) >= BigInt(offset) &&
        BigInt(next) <= BigInt(artifact.object.byte_size) &&
        BigInt(bytes) === BigInt(next) - BigInt(offset) &&
        bytes <= query.limit &&
        !raw.invalid_utf8 &&
        raw.truncated === BigInt(next) < BigInt(artifact.object.byte_size),
    )
  } else {
    requireValue(
      !Object.hasOwn(raw, 'text') &&
        offset === '0' &&
        next === '0' &&
        !raw.truncated &&
        (projection !== 'image_ref' || !raw.invalid_utf8),
    )
  }
  return Object.freeze({
    artifact,
    projection,
    text: content,
    offset,
    next_offset: next,
    truncated: raw.truncated,
    invalid_utf8: raw.invalid_utf8 === true,
  })
}
export function createProjectArtifactsAPI(fetcher?: Fetch): ProjectArtifactsAPI {
  const transport = accountTransport(fetcher)
  return {
    list(projectID, query, signal) {
      const project = captureArtifactID(projectID),
        captured = captureArtifactQuery(query)
      return transport(
        'listProjectArtifacts',
        (value) => decodeArtifactPage(value, project, captured),
        { projectID: project, artifactQuery: captured, signal },
      )
    },
    read(target, query, signal) {
      const capturedRef = captureArtifactRef(target),
        captured = captureArtifactReadQuery(query)
      return transport(
        'readProjectArtifact',
        (value) => decodeArtifactContent(value, capturedRef, captured),
        {
          projectID: capturedRef.project_id,
          artifactID: capturedRef.artifact_id,
          fileID: capturedRef.file_id,
          artifactRead: captured,
          signal,
        },
      )
    },
  }
}

// A stable Meeting file reference carries only ArtifactID. This authorized
// point read resolves its original full P/A/F identity before content is read.
export function createProjectArtifactMetadataAPI(fetcher?: Fetch): ProjectArtifactMetadataAPI {
  const transport = accountTransport(fetcher)
  return {
    metadata(projectID, artifactID, signal) {
      const project = captureArtifactID(projectID),
        artifact = captureArtifactID(artifactID)
      return transport(
        'getProjectArtifactMetadata',
        (value) => {
          const result = decodeArtifactMetadata(shape(value, ['artifact']).artifact)
          requireValue(
            result.artifact_ref.project_id === project &&
              result.artifact_ref.artifact_id === artifact,
          )
          return result
        },
        { projectID: project, artifactID: artifact, signal },
      )
    },
  }
}
