import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import {
  captureKnowledgeQuery,
  captureKnowledgeID,
  compareKnowledgeDocuments,
  parseKnowledgeDocument,
  type KnowledgeDocument,
} from './knowledge-owner'

export type KnowledgeTitleQuery = Readonly<{
  title_query: string
  limit: number
  cursor?: string
}>
export type KnowledgeTitleHit = Readonly<{
  document: KnowledgeDocument
  ancestors: readonly KnowledgeDocument[]
}>
export type KnowledgeTitlePage = Readonly<{
  items: readonly KnowledgeTitleHit[]
  next_cursor?: string
}>
export interface KnowledgeTitleSearchPort {
  searchTitles(projectID: string, query: KnowledgeTitleQuery): Promise<KnowledgeTitlePage>
  abandon(): void
}
export interface KnowledgeTitleSearchAPI {
  searchTitles(
    projectID: string,
    query: KnowledgeTitleQuery,
    signal: AbortSignal,
  ): Promise<KnowledgeTitlePage>
}
const encoder = new TextEncoder()
function requireValue(value: boolean): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}

// Search is a literal, case-sensitive substring. Preserve whitespace and the
// characters %, _ and !; the server owns SQL escaping, never the browser.
export function captureKnowledgeTitleQuery(value: unknown): KnowledgeTitleQuery {
  try {
    const v = shape(value, ['title_query', 'limit'], ['cursor'])
    const title_query = string(v.title_query, 1, 32768)
    requireValue(
      !/[\u0000-\u001f\u007f-\u009f]/u.test(title_query) &&
        !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(
          title_query,
        ),
    )
    const page = captureKnowledgeQuery({
      limit: v.limit,
      ...(Object.hasOwn(v, 'cursor') ? { cursor: v.cursor } : {}),
    })
    const result = Object.freeze({ title_query, ...page })
    requireValue(encoder.encode(encodeKnowledgeTitleQuery(result)).byteLength <= 32768)
    return result
  } catch {
    throw new AccountFailure('invalid-input')
  }
}

// Called only with an already captured value, including by the shared transport.
export function encodeKnowledgeTitleQuery(query: KnowledgeTitleQuery): string {
  const params = new URLSearchParams({ title_query: query.title_query, limit: String(query.limit) })
  if (query.cursor !== undefined) params.set('cursor', query.cursor)
  return params.toString()
}

export function parseKnowledgeTitlePage(
  value: unknown,
  projectID: string,
  query: KnowledgeTitleQuery,
): KnowledgeTitlePage {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= query.limit)
  const ids = new Set<string>()
  let previous: KnowledgeDocument | undefined
  const items = v.items.map((value): KnowledgeTitleHit => {
    const hit = shape(value, ['document', 'ancestors'])
    const document = parseKnowledgeDocument(hit.document, projectID)
    requireValue(
      document.title.includes(query.title_query) &&
        !ids.has(document.id) &&
        (!previous || compareKnowledgeDocuments(previous, document) < 0),
    )
    ids.add(document.id)
    previous = document
    requireValue(Array.isArray(hit.ancestors))
    const seen = new Set([document.id])
    let parent: string | null = null
    const ancestors = hit.ancestors.map((value) => {
      const ancestor = parseKnowledgeDocument(value, projectID)
      requireValue(!seen.has(ancestor.id) && ancestor.parent_document_id === parent)
      seen.add(ancestor.id)
      parent = ancestor.id
      return ancestor
    })
    requireValue(document.parent_document_id === parent)
    return Object.freeze({ document, ancestors: Object.freeze(ancestors) })
  })
  let next: string | undefined
  if (Object.hasOwn(v, 'next_cursor')) {
    try {
      next = captureKnowledgeQuery({ limit: query.limit, cursor: v.next_cursor }).cursor
    } catch {
      throw new AccountFailure('invalid-response')
    }
  }
  requireValue(next === undefined || (items.length === query.limit && next !== query.cursor))
  return Object.freeze({
    items: Object.freeze(items),
    ...(next === undefined ? {} : { next_cursor: next }),
  })
}

export function createKnowledgeTitleSearchAPI(fetcher?: Fetch): KnowledgeTitleSearchAPI {
  const request = accountTransport(fetcher)
  return {
    async searchTitles(projectID, query, signal) {
      const project = captureKnowledgeID(projectID),
        captured = captureKnowledgeTitleQuery(query)
      return request(
        'knowledgeTitleSearch',
        (value) => parseKnowledgeTitlePage(value, project, captured),
        {
          signal,
          projectID: project,
          knowledgeTitleQuery: captured,
        },
      )
    },
  }
}
