import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureKnowledgeID,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'

export type KnowledgeTextMedia = 'text/plain' | 'text/markdown'
export type KnowledgeTextCreate = Readonly<{
  parent_document_id: string | null
  title: string
  media_type: KnowledgeTextMedia
  text: string
}>
export type KnowledgeTextReplace = Readonly<{
  expected_version: string
  media_type: KnowledgeTextMedia
  text: string
}>
export type KnowledgeTextIntent =
  | Readonly<{ command: 'create'; document_id: string; request: KnowledgeTextCreate }>
  | Readonly<{ command: 'update'; document_id: string; request: KnowledgeTextReplace }>
export type KnowledgeTextResult = Readonly<{ document: KnowledgeDocument }>
export type KnowledgeTextLookup =
  | Readonly<{ state: 'in_progress' | 'not_observed'; receipt: null }>
  | Readonly<{
      state: 'committed'
      receipt: Readonly<{
        command: 'create' | 'update'
        document: KnowledgeDocument
        changed: boolean
      }>
    }>
export interface KnowledgeTextAPI {
  write(
    projectID: string,
    intent: KnowledgeTextIntent,
    options: WriteOptions,
  ): Promise<KnowledgeTextResult>
  lookup(
    projectID: string,
    intent: KnowledgeTextIntent,
    options: WriteOptions,
  ): Promise<KnowledgeTextLookup>
}
const maximum = 1048576
const encoder = new TextEncoder()
const surrogate = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u
const requireValue: (value: unknown) => asserts value = (value) => {
  if (!value) throw new AccountFailure('invalid-response')
}
export function captureKnowledgeText(value: unknown): KnowledgeTextIntent {
  try {
    const v = shape(value, ['command', 'document_id', 'request'])
    const target = captureKnowledgeID(v.document_id)
    requireValue(v.command === 'create' || v.command === 'update')
    const r = shape(
      v.request,
      v.command === 'create'
        ? ['parent_document_id', 'title', 'media_type', 'text']
        : ['expected_version', 'media_type', 'text'],
    )
    requireValue(r.media_type === 'text/plain' || r.media_type === 'text/markdown')
    const text = string(r.text, 0, maximum)
    requireValue(!surrogate.test(text))
    const media_type = r.media_type as KnowledgeTextMedia
    let intent: KnowledgeTextIntent
    if (v.command === 'create') {
      const parent = r.parent_document_id === null ? null : captureKnowledgeID(r.parent_document_id)
      const title = string(r.title, 1, 512)
      requireValue(
        parent !== target && !surrogate.test(title) && !/[\u0000-\u001f\u007f-\u009f]/u.test(title),
      )
      intent = {
        command: 'create',
        document_id: target,
        request: Object.freeze({ parent_document_id: parent, title, media_type, text }),
      }
    } else {
      const expected_version = string(r.expected_version, 1, 19)
      requireValue(
        /^[1-9][0-9]*$/.test(expected_version) && BigInt(expected_version) <= 9223372036854775807n,
      )
      intent = {
        command: 'update',
        document_id: target,
        request: Object.freeze({ expected_version, media_type, text }),
      }
    }
    // Both the initial body and its full original-intent Lookup must be sendable.
    requireValue(
      encoder.encode(JSON.stringify(intent.request)).length <= maximum &&
        encoder.encode(JSON.stringify(intent)).length <= maximum + 84,
    )
    return Object.freeze(intent)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function options(value: WriteOptions) {
  try {
    const v = shape(value, ['csrfToken', 'key', 'signal'])
    const csrf = string(v.csrfToken, 43, 128),
      key = string(v.key, 1, 128)
    requireValue(
      /^[A-Za-z0-9_-]{43,128}$/.test(csrf) &&
        /^[A-Za-z0-9._:/-]{1,128}$/.test(key) &&
        v.signal instanceof AbortSignal,
    )
    return { csrf, key, signal: v.signal as AbortSignal }
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function document(
  value: unknown,
  project: string,
  original: KnowledgeTextIntent,
  changed?: boolean,
) {
  const result = parseKnowledgeDocument(value, project, original.document_id)
  requireValue(result.source_kind === 'text' && result.media_type === original.request.media_type)
  if (original.command === 'create') {
    requireValue(
      result.content_version === '1' &&
        result.title === original.request.title &&
        result.parent_document_id === original.request.parent_document_id &&
        changed !== false,
    )
  } else {
    const expected = BigInt(original.request.expected_version),
      actual = BigInt(result.content_version)
    requireValue(
      changed === undefined
        ? actual === expected || actual === expected + 1n
        : actual === expected + (changed ? 1n : 0n),
    )
  }
  return result
}
export function createKnowledgeTextAPI(fetcher?: Fetch): KnowledgeTextAPI {
  const request = accountTransport(fetcher)
  return {
    write(projectID, value, write) {
      const project = captureKnowledgeID(projectID),
        original = captureKnowledgeText(value)
      return request(
        original.command === 'create' ? 'knowledgeTextCreate' : 'knowledgeTextReplace',
        (value) => {
          const v = shape(value, ['document'])
          return Object.freeze({ document: document(v.document, project, original) })
        },
        {
          ...options(write),
          projectID: project,
          target: original.document_id,
          body: original.request,
        },
      )
    },
    lookup(projectID, value, write) {
      const project = captureKnowledgeID(projectID),
        original = captureKnowledgeText(value)
      return request(
        'knowledgeTextLookup',
        (value): KnowledgeTextLookup => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'in_progress' || v.state === 'not_observed') {
            requireValue(v.receipt === null)
            return Object.freeze({ state: v.state, receipt: null })
          }
          requireValue(v.state === 'committed')
          const r = shape(v.receipt, ['command', 'document', 'changed'])
          requireValue(r.command === original.command && typeof r.changed === 'boolean')
          return Object.freeze({
            state: 'committed',
            receipt: Object.freeze({
              command: original.command,
              changed: r.changed,
              document: document(r.document, project, original, r.changed),
            }),
          })
        },
        { ...options(write), projectID: project, body: original },
      )
    },
  }
}
