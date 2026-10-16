import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  captureKnowledgeID,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'
import { parseSystemInstant } from './system-account'

export type KnowledgeDeleteInput = Readonly<{ confirmation_token: string }>
export type KnowledgeDeletePreview = Readonly<{
  root_id: string
  nodes: readonly KnowledgeDocument[]
  scope_digest: string
  confirmation_token: string
  expires_at: string
}>
export type KnowledgeDeleteReceipt = Readonly<{
  root_id: string
  deleted_ids: readonly string[]
  cleanup_pending: boolean
}>
export type KnowledgeDeleteLookup =
  | Readonly<{ state: 'not_observed' | 'in_progress'; receipt: null }>
  | Readonly<{
      state: 'committed'
      receipt: KnowledgeDeleteReceipt & Readonly<{ command: 'delete-subtree'; changed: true }>
    }>
export interface KnowledgeDeleteAPI {
  preview(
    projectID: string,
    rootID: string,
    options: Omit<WriteOptions, 'key'>,
  ): Promise<KnowledgeDeletePreview>
  remove(
    projectID: string,
    rootID: string,
    input: KnowledgeDeleteInput,
    options: WriteOptions,
  ): Promise<KnowledgeDeleteReceipt>
  lookup(
    projectID: string,
    rootID: string,
    input: KnowledgeDeleteInput,
    options: WriteOptions,
  ): Promise<KnowledgeDeleteLookup>
}
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function token(value: unknown) {
  const result = string(value, 1, 8192)
  requireValue(/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{43}$/.test(result))
  return result
}
export function captureKnowledgeDelete(value: unknown): KnowledgeDeleteInput {
  try {
    const v = shape(value, ['confirmation_token'])
    return Object.freeze({ confirmation_token: token(v.confirmation_token) })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function options(value: Omit<WriteOptions, 'key'> | WriteOptions, write: boolean) {
  try {
    const v = shape(value, ['csrfToken', 'signal', ...(write ? ['key'] : [])])
    const csrf = string(v.csrfToken, 43, 128)
    requireValue(/^[A-Za-z0-9_-]{43,128}$/.test(csrf) && v.signal instanceof AbortSignal)
    if (!write) return { csrf, signal: v.signal as AbortSignal }
    const key = string(v.key, 1, 128)
    requireValue(/^[A-Za-z0-9._:/-]{1,128}$/.test(key))
    return { csrf, signal: v.signal as AbortSignal, key }
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function preview(value: unknown, project: string, root: string): KnowledgeDeletePreview {
  const v = shape(value, ['root_id', 'nodes', 'scope_digest', 'confirmation_token', 'expires_at'])
  requireValue(v.root_id === root && Array.isArray(v.nodes) && v.nodes.length > 0)
  const nodes = v.nodes.map((node) => parseKnowledgeDocument(node, project))
  const byID = new Map(nodes.map((node) => [node.id, node]))
  requireValue(byID.size === nodes.length && byID.has(root))
  const rootParent = byID.get(root)!.parent_document_id
  requireValue(rootParent === null || !byID.has(rootParent))
  const connected = new Set([root])
  for (const node of nodes) {
    const path = new Set<string>()
    let cursor = node.id
    while (!connected.has(cursor)) {
      requireValue(!path.has(cursor))
      path.add(cursor)
      const parent = byID.get(cursor)?.parent_document_id
      requireValue(typeof parent === 'string' && byID.has(parent))
      cursor = parent
    }
    for (const id of path) connected.add(id)
  }
  const scope_digest = string(v.scope_digest, 71, 71)
  requireValue(/^sha256:[0-9a-f]{64}$/.test(scope_digest))
  return Object.freeze({
    root_id: root,
    nodes: Object.freeze(nodes),
    scope_digest,
    confirmation_token: token(v.confirmation_token),
    expires_at: parseSystemInstant(v.expires_at),
  })
}
async function verifyScope(value: KnowledgeDeletePreview, project: string, signal: AbortSignal) {
  // Contract's canonical JSON sorts object keys and subtree nodes by ID. Titles
  // and private content are deliberately absent from the scope digest.
  const nodes = [...value.nodes]
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((node) => ({
      content_version: node.content_version,
      id: node.id,
      parent_id: node.parent_document_id,
      status: 'active',
    }))
  const bytes = new TextEncoder().encode(
    'agenteam.knowledge.subtree.v1\0' +
      JSON.stringify({ nodes, project_id: project, root_id: value.root_id }),
  )
  const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))
  if (signal.aborted) throw new AccountFailure('cancelled')
  requireValue(
    'sha256:' + [...hash].map((v) => v.toString(16).padStart(2, '0')).join('') ===
      value.scope_digest,
  )
  return value
}
function receipt(value: unknown, root: string, committed: true): KnowledgeDeleteReceipt & Readonly<{ command: 'delete-subtree'; changed: true }>
function receipt(value: unknown, root: string): KnowledgeDeleteReceipt
function receipt(value: unknown, root: string, committed = false) {
  const v = shape(value, [
    'root_id',
    'deleted_ids',
    'cleanup_pending',
    ...(committed ? ['command', 'changed'] : []),
  ])
  requireValue(
    v.root_id === root &&
      typeof v.cleanup_pending === 'boolean' &&
      Array.isArray(v.deleted_ids) &&
      v.deleted_ids.length > 0,
  )
  const ids: string[] = v.deleted_ids.map((id) => {
    try {
      return captureKnowledgeID(id as string)
    } catch {
      throw new AccountFailure('invalid-response')
    }
  })
  requireValue(ids.includes(root) && ids.every((id, index) => index === 0 || ids[index - 1]! < id))
  const result = {
    root_id: root,
    deleted_ids: Object.freeze(ids),
    cleanup_pending: v.cleanup_pending,
  }
  if (!committed) return Object.freeze(result)
  requireValue(v.command === 'delete-subtree' && v.changed === true)
  return Object.freeze({ ...result, command: 'delete-subtree' as const, changed: true as const })
}
export function createKnowledgeDeleteAPI(fetcher?: Fetch): KnowledgeDeleteAPI {
  const request = accountTransport(fetcher)
  return {
    async preview(projectID, rootID, value) {
      const project = captureKnowledgeID(projectID),
        root = captureKnowledgeID(rootID)
      const read = options(value, false)
      const result = await request('knowledgeDeletePreview', (v) => preview(v, project, root), {
        csrf: read.csrf,
        signal: read.signal,
        projectID: project,
        target: root,
        body: {},
      })
      return verifyScope(result, project, read.signal)
    },
    async remove(projectID, rootID, value, write) {
      const project = captureKnowledgeID(projectID),
        root = captureKnowledgeID(rootID),
        input = captureKnowledgeDelete(value)
      const authorized = options(write, true)
      return request('knowledgeDelete', (v) => receipt(v, root), {
        ...authorized,
        key: authorized.key!,
        projectID: project,
        target: root,
        body: input,
      })
    },
    async lookup(projectID, rootID, value, write) {
      const project = captureKnowledgeID(projectID),
        root = captureKnowledgeID(rootID),
        input = captureKnowledgeDelete(value)
      const authorized = options(write, true)
      return request(
        'knowledgeDeleteLookup',
        (value): KnowledgeDeleteLookup => {
          const v = shape(value, ['state', 'receipt'])
          if (v.state === 'not_observed' || v.state === 'in_progress') {
            requireValue(v.receipt === null)
            return Object.freeze({ state: v.state, receipt: null })
          }
          requireValue(v.state === 'committed')
          const result = receipt(v.receipt, root, true)
          requireValue('command' in result)
          return Object.freeze({ state: 'committed', receipt: result })
        },
        {
          ...authorized,
          key: authorized.key!,
          projectID: project,
          body: Object.freeze({ command: 'delete-subtree', document_id: root, request: input }),
        },
      )
    },
  }
}
