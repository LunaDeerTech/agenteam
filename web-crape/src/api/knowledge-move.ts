import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureKnowledgeID,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'

export type KnowledgeMoveInput = Readonly<{
  expected_parent_id: string | null
  target_parent_id: string | null
}>
export type KnowledgeMoveResult = Readonly<{ document: KnowledgeDocument; changed: boolean }>
export type KnowledgeMoveLookup =
  | Readonly<{ state: 'not_observed' | 'in_progress'; receipt: null }>
  | Readonly<{ state: 'committed'; receipt: KnowledgeMoveResult & Readonly<{ command: 'move' }> }>
export interface KnowledgeMoveAPI {
  move(
    projectID: string,
    documentID: string,
    input: KnowledgeMoveInput,
    write: WriteOptions,
  ): Promise<KnowledgeMoveResult>
  lookup(
    projectID: string,
    documentID: string,
    input: KnowledgeMoveInput,
    write: WriteOptions,
  ): Promise<KnowledgeMoveLookup>
}
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
export function captureKnowledgeMove(value: unknown): KnowledgeMoveInput {
  try {
    const v = shape(value, ['expected_parent_id', 'target_parent_id'])
    return Object.freeze({
      expected_parent_id:
        v.expected_parent_id === null ? null : captureKnowledgeID(v.expected_parent_id),
      target_parent_id: v.target_parent_id === null ? null : captureKnowledgeID(v.target_parent_id),
    })
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
export function parseKnowledgeMoveResult(
  value: unknown,
  projectID: string,
  documentID: string,
  input: KnowledgeMoveInput,
): KnowledgeMoveResult {
  const v = shape(value, ['document', 'changed'])
  const document = parseKnowledgeDocument(v.document, projectID, documentID)
  requireValue(document.parent_document_id === input.target_parent_id)
  requireValue(
    typeof v.changed === 'boolean' &&
      v.changed === (input.expected_parent_id !== input.target_parent_id),
  )
  // Move guards the original parent, not content_version or title. The receipt
  // may contain content updated concurrently and is never a current read.
  return Object.freeze({ document, changed: v.changed })
}
export function createKnowledgeMoveAPI(fetcher?: Fetch): KnowledgeMoveAPI {
  const request = accountTransport(fetcher)
  return {
    async move(projectID, documentID, value, write) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID),
        input = captureKnowledgeMove(value)
      return request(
        'knowledgeMove',
        (value) => parseKnowledgeMoveResult(value, project, target, input),
        {
          ...options(write),
          projectID: project,
          target,
          body: input,
        },
      )
    },
    async lookup(projectID, documentID, value, write) {
      const project = captureKnowledgeID(projectID),
        target = captureKnowledgeID(documentID),
        input = captureKnowledgeMove(value)
      return request(
        'knowledgeMoveLookup',
        (value): KnowledgeMoveLookup => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'not_observed' || v.state === 'in_progress') {
            requireValue(v.receipt === null)
            return Object.freeze({ state: v.state, receipt: null })
          }
          requireValue(v.state === 'committed')
          const r = shape(v.receipt, ['command', 'document', 'changed'])
          requireValue(r.command === 'move')
          const result = parseKnowledgeMoveResult(
            { document: r.document, changed: r.changed },
            project,
            target,
            input,
          )
          return Object.freeze({
            state: 'committed',
            receipt: Object.freeze({ command: 'move', ...result }),
          })
        },
        {
          ...options(write),
          projectID: project,
          body: Object.freeze({ command: 'move', document_id: target, request: input }),
        },
      )
    },
  }
}
