import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureKnowledgeID,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'

export const knowledgeFileMaximumBytes = 1048576
const bodyLimit = 1414488
const encoder = new TextEncoder()
export type KnowledgeFileMedia =
  | 'text/plain'
  | 'text/markdown'
  | 'application/pdf'
  | 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
export type KnowledgeFileCreate = Readonly<{
  parent_document_id: string | null
  title: string
  media_type: KnowledgeFileMedia
  bytes_base64: string
}>
export type KnowledgeFileReplace = Readonly<{
  expected_version: string
  media_type: KnowledgeFileMedia
  bytes_base64: string
}>
export type KnowledgeFileIntent =
  | Readonly<{ command: 'create'; document_id: string; request: KnowledgeFileCreate }>
  | Readonly<{ command: 'update'; document_id: string; request: KnowledgeFileReplace }>
export type KnowledgeFileResult = Readonly<{ document: KnowledgeDocument }>
export type KnowledgeFileLookup =
  | Readonly<{ state: 'in_progress' | 'not_observed'; receipt: null }>
  | Readonly<{
      state: 'committed'
      receipt: Readonly<{
        command: 'create' | 'update'
        document: KnowledgeDocument
        changed: boolean
      }>
    }>
export type KnowledgeSource = Readonly<{ document: KnowledgeDocument; bytes: Uint8Array }>
export interface KnowledgeFileAPI {
  write(
    projectID: string,
    intent: KnowledgeFileIntent,
    options: WriteOptions,
  ): Promise<KnowledgeFileResult>
  lookup(
    projectID: string,
    intent: KnowledgeFileIntent,
    options: WriteOptions,
  ): Promise<KnowledgeFileLookup>
  source(
    projectID: string,
    documentID: string,
    version: string,
    signal: AbortSignal,
  ): Promise<KnowledgeSource>
}
const requireValue: (value: unknown) => asserts value = (value) => {
  if (!value) throw new AccountFailure('invalid-response')
}
const mediaValues: readonly string[] = [
  'text/plain',
  'text/markdown',
  'application/pdf',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
]
function version(value: unknown) {
  const result = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(result) && BigInt(result) <= 9223372036854775807n)
  return result
}
function decodeBytes(value: unknown): Uint8Array {
  requireValue(typeof value === 'string' && value.length <= 1398104)
  let binary: string
  try {
    binary = atob(value)
  } catch {
    throw new AccountFailure('invalid-response')
  }
  requireValue(binary.length <= knowledgeFileMaximumBytes && btoa(binary) === value)
  return Uint8Array.from(binary, (c) => c.charCodeAt(0))
}
export function encodeKnowledgeFile(bytes: Uint8Array): string {
  if (!(bytes instanceof Uint8Array) || bytes.length > knowledgeFileMaximumBytes)
    throw new AccountFailure('invalid-input')
  let binary = ''
  for (let start = 0; start < bytes.length; start += 8192)
    binary += String.fromCharCode(...bytes.subarray(start, start + 8192))
  return btoa(binary)
}
export function captureKnowledgeFile(value: unknown): KnowledgeFileIntent {
  try {
    const v = shape(value, ['command', 'document_id', 'request'])
    const target = captureKnowledgeID(v.document_id)
    requireValue(v.command === 'create' || v.command === 'update')
    const r = shape(
      v.request,
      v.command === 'create'
        ? ['parent_document_id', 'title', 'media_type', 'bytes_base64']
        : ['expected_version', 'media_type', 'bytes_base64'],
    )
    requireValue(mediaValues.includes(r.media_type as string))
    decodeBytes(r.bytes_base64)
    const media_type = r.media_type as KnowledgeFileMedia,
      bytes_base64 = r.bytes_base64 as string
    let intent: KnowledgeFileIntent
    if (v.command === 'create') {
      const parent_document_id =
        r.parent_document_id === null ? null : captureKnowledgeID(r.parent_document_id)
      const title = string(r.title, 1, 512)
      requireValue(
        parent_document_id !== target &&
          !/[\u0000-\u001f\u007f-\u009f]/u.test(title) &&
          !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(title),
      )
      intent = {
        command: 'create',
        document_id: target,
        request: Object.freeze({ parent_document_id, title, media_type, bytes_base64 }),
      }
    } else {
      intent = {
        command: 'update',
        document_id: target,
        request: Object.freeze({
          expected_version: version(r.expected_version),
          media_type,
          bytes_base64,
        }),
      }
    }
    requireValue(
      encoder.encode(JSON.stringify(intent.request)).length <= bodyLimit &&
        encoder.encode(JSON.stringify(intent)).length <= bodyLimit + 84,
    )
    return Object.freeze(intent)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function writeOptions(value: WriteOptions) {
  try {
    const v = shape(value, ['csrfToken', 'key', 'signal']),
      csrf = string(v.csrfToken, 43, 128),
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
function document(value: unknown, project: string, intent: KnowledgeFileIntent, changed?: boolean) {
  const result = parseKnowledgeDocument(value, project, intent.document_id)
  requireValue(result.media_type === intent.request.media_type)
  if (intent.command === 'create') {
    requireValue(
      result.content_version === '1' &&
        result.title === intent.request.title &&
        result.parent_document_id === intent.request.parent_document_id &&
        changed !== false,
    )
  } else {
    const expected = BigInt(intent.request.expected_version),
      actual = BigInt(result.content_version)
    requireValue(
      changed === undefined
        ? actual === expected || actual === expected + 1n
        : actual === expected + (changed ? 1n : 0n),
    )
  }
  return result
}
export function createKnowledgeFileAPI(fetcher?: Fetch): KnowledgeFileAPI {
  const request = accountTransport(fetcher)
  return {
    write(projectID, input, options) {
      const project = captureKnowledgeID(projectID),
        original = captureKnowledgeFile(input)
      return request(
        original.command === 'create' ? 'knowledgeFileCreate' : 'knowledgeFileReplace',
        (value) => {
          const v = shape(value, ['document'])
          return Object.freeze({ document: document(v.document, project, original) })
        },
        {
          ...writeOptions(options),
          projectID: project,
          target: original.document_id,
          body: original.request,
        },
      )
    },
    lookup(projectID, input, options) {
      const project = captureKnowledgeID(projectID),
        original = captureKnowledgeFile(input)
      return request(
        'knowledgeFileLookup',
        (value): KnowledgeFileLookup => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'in_progress' || v.state === 'not_observed') {
            requireValue(v.receipt === null)
            return Object.freeze({ state: v.state, receipt: null })
          }
          requireValue(v.state === 'committed')
          const receipt = shape(v.receipt, ['command', 'document', 'changed'])
          requireValue(receipt.command === original.command && typeof receipt.changed === 'boolean')
          return Object.freeze({
            state: 'committed',
            receipt: Object.freeze({
              command: original.command,
              document: document(receipt.document, project, original, receipt.changed),
              changed: receipt.changed,
            }),
          })
        },
        { ...writeOptions(options), projectID: project, body: original },
      )
    },
    source(projectID, documentID, expected, signal) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID)
      let revision: string
      try {
        revision = version(expected)
      } catch {
        throw new AccountFailure('invalid-input')
      }
      return request(
        'knowledgeSource',
        (value) => {
          const v = shape(value, ['document', 'bytes_base64']),
            d = parseKnowledgeDocument(v.document, project, target)
          requireValue(d.content_version === revision)
          return Object.freeze({ document: d, bytes: decodeBytes(v.bytes_base64) })
        },
        { projectID: project, target, revision, signal },
      )
    },
  }
}
