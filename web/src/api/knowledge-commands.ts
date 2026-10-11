import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureKnowledgeID,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'

export type KnowledgeRenameInput = Readonly<{ expected_version: string; title: string }>
export type KnowledgeRenameResult = Readonly<{ document: KnowledgeDocument }>
export type KnowledgeRenameLookup =
  | Readonly<{ state: 'in_progress' | 'not_observed'; receipt: null }>
  | Readonly<{
      state: 'committed'
      receipt: Readonly<{ command: 'update'; document: KnowledgeDocument; changed: boolean }>
    }>
export interface KnowledgeCommandsAPI {
  rename(
    projectID: string,
    documentID: string,
    input: KnowledgeRenameInput,
    write: WriteOptions,
  ): Promise<KnowledgeRenameResult>
  lookup(
    projectID: string,
    documentID: string,
    input: KnowledgeRenameInput,
    write: WriteOptions,
  ): Promise<KnowledgeRenameLookup>
}
const maximumVersion = 9223372036854775807n
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
export function captureKnowledgeRename(value: unknown): KnowledgeRenameInput {
  try {
    const v = shape(value, ['expected_version', 'title'])
    const expected_version = string(v.expected_version, 1, 19),
      title = string(v.title, 1, 512)
    requireValue(
      /^[1-9][0-9]*$/.test(expected_version) && BigInt(expected_version) <= maximumVersion,
    )
    requireValue(
      !/[\u0000-\u001f\u007f-\u009f]/u.test(title) &&
        !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(title),
    )
    return Object.freeze({ expected_version, title })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function options(write: WriteOptions) {
  try {
    const v = shape(write, ['csrfToken', 'key', 'signal'])
    const csrf = string(v.csrfToken, 43, 128),
      key = string(v.key, 1, 128)
    requireValue(/^[A-Za-z0-9_-]{43,128}$/.test(csrf) && /^[A-Za-z0-9._:/-]{1,128}$/.test(key))
    requireValue(v.signal instanceof AbortSignal)
    return { csrf, key, signal: v.signal as AbortSignal }
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function document(
  value: unknown,
  project: string,
  target: string,
  input: KnowledgeRenameInput,
  changed?: boolean,
) {
  const result = parseKnowledgeDocument(value, project, target)
  const expected = BigInt(input.expected_version),
    actual = BigInt(result.content_version)
  requireValue(result.title === input.title)
  requireValue(
    changed === undefined
      ? actual === expected || actual === expected + 1n
      : actual === expected + (changed ? 1n : 0n),
  )
  // Move has no content version. The receipt's actual parent may differ from
  // every earlier metadata observation and is never checked against old input.
  return result
}
export function createKnowledgeCommandsAPI(fetcher?: Fetch): KnowledgeCommandsAPI {
  const request = accountTransport(fetcher)
  return {
    async rename(projectID, documentID, value, write) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID),
        input = captureKnowledgeRename(value)
      return request(
        'knowledgeRename',
        (value) => {
          const v = shape(value, ['document'])
          return Object.freeze({ document: document(v.document, project, target, input) })
        },
        { ...options(write), projectID: project, target, body: input },
      )
    },
    async lookup(projectID, documentID, value, write) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID),
        input = captureKnowledgeRename(value)
      return request(
        'knowledgeRenameLookup',
        (value): KnowledgeRenameLookup => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'not_observed' || v.state === 'in_progress') {
            requireValue(v.receipt === null)
            return Object.freeze({ state: v.state, receipt: null })
          }
          requireValue(v.state === 'committed')
          const r = shape(v.receipt, ['command', 'document', 'changed'])
          requireValue(r.command === 'update' && typeof r.changed === 'boolean')
          return Object.freeze({
            state: 'committed',
            receipt: Object.freeze({
              command: 'update',
              changed: r.changed,
              document: document(r.document, project, target, input, r.changed),
            }),
          })
        },
        {
          ...options(write),
          projectID: project,
          body: Object.freeze({ command: 'update', document_id: target, request: input }),
        },
      )
    },
  }
}
