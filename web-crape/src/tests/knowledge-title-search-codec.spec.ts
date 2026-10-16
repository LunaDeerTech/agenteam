import { describe, expect, it } from 'vitest'
import {
  captureKnowledgeTitleQuery as capture,
  encodeKnowledgeTitleQuery as encode,
  parseKnowledgeTitlePage as parse,
} from '../api/knowledge-title-search'
import {
  titleDocument as doc,
  titleID as id,
  titleProject as project,
} from './knowledge-title-search.fixture'
const query = capture({ title_query: '%_!', limit: 2 })
describe('Knowledge full-project title query and complete hit representation', () => {
  it('preserves whitespace, case, literal SQL wildcard characters and UTF-8', () => {
    const q = capture({ title_query: ' A %_! 中文🙂 ', limit: 50, cursor: 'opaque/_-.' })
    expect(new URLSearchParams(encode(q)).get('title_query')).toBe(' A %_! 中文🙂 ')
    expect(q).toEqual({ title_query: ' A %_! 中文🙂 ', limit: 50, cursor: 'opaque/_-.' })
    expect(Object.isFrozen(q)).toBe(true)
    expect(capture({ title_query: 'a'.repeat(513), limit: 200 }).title_query).toHaveLength(513)
  })
  it('bounds the complete encoded query at 32KiB, not the decoded character count', () => {
    const overhead = encode({ title_query: '', limit: 50 }).length
    expect(encode(capture({ title_query: 'a'.repeat(32768 - overhead), limit: 50 }))).toHaveLength(
      32768,
    )
    expect(() => capture({ title_query: 'a'.repeat(32769 - overhead), limit: 50 })).toThrow(
      'invalid-input',
    )
    expect(() => capture({ title_query: '中'.repeat(4000), limit: 50 })).toThrow('invalid-input')
  })
  it.each(['', 'line\nfeed', '\u0000', '\ud800', '\udfff'])(
    'rejects invalid query %j',
    (title_query) => {
      expect(() => capture({ title_query, limit: 50 })).toThrow('invalid-input')
    },
  )
  it('rejects invalid limits, extra fields and overlong cursor bytes', () => {
    for (const limit of [0, 201, 1.5, '50'])
      expect(() => capture({ title_query: 'x', limit })).toThrow('invalid-input')
    expect(() => capture({ ...query, extra: true })).toThrow('invalid-input')
    expect(() => capture({ ...query, cursor: '中'.repeat(2731) })).toThrow('invalid-input')
  })
  it('publishes an immutable complete ancestor chain with full int64 versions', () => {
    const root = doc(10, '根'),
      parent = doc(11, '父', id(10)),
      child = doc(12, '说明 %_! 中文', id(11))
    const page = parse({ items: [{ document: child, ancestors: [root, parent] }] }, project, query)
    expect(page.items[0]?.document.content_version).toBe('9007199254740993')
    expect(page.items[0]?.ancestors.map((a) => a.id)).toEqual([id(10), id(11)])
    expect(
      [page, page.items, page.items[0], page.items[0]?.ancestors, page.items[0]?.document].every(
        Object.isFrozen,
      ),
    ).toBe(true)
  })
  it('rejects omitted, foreign, cyclic and disconnected ancestor paths', () => {
    const child = doc(12, '%_!', id(11)),
      root = doc(10, '根'),
      parent = doc(11, '父', id(10))
    for (const ancestors of [
      [],
      [parent],
      [root],
      [root, { ...parent, project_id: id(3) }],
      [root, parent, child],
    ]) {
      expect(() => parse({ items: [{ document: child, ancestors }] }, project, query)).toThrow(
        'invalid-response',
      )
    }
  })
  it('rejects a bad final hit, duplicate IDs, hidden fields and wildcard-only matches', () => {
    const hit = { document: doc(), ancestors: [] }
    for (const bad of [
      hit,
      { document: { ...doc(11), object_id: id(99) }, ancestors: [] },
      { document: doc(11, '说明 XYZ 中文'), ancestors: [] },
      { document: { ...doc(11), project_id: id(3) }, ancestors: [] },
    ])
      expect(() => parse({ items: [hit, bad] }, project, query)).toThrow('invalid-response')
  })
  it('enforces page-local UTF-8 C ordering and matching cursor semantics', () => {
    const q = capture({ title_query: 'x', limit: 2, cursor: 'prior' })
    const first = { document: doc(10, 'x\ue000'), ancestors: [] },
      last = { document: doc(11, 'x🙂'), ancestors: [] }
    expect(parse({ items: [first, last], next_cursor: 'next' }, project, q).next_cursor).toBe(
      'next',
    )
    expect(() => parse({ items: [last, first] }, project, q)).toThrow('invalid-response')
    expect(() => parse({ items: [first], next_cursor: 'next' }, project, q)).toThrow(
      'invalid-response',
    )
    expect(() => parse({ items: [first, last], next_cursor: 'prior' }, project, q)).toThrow(
      'invalid-response',
    )
    expect(() => parse({ items: [first, last], next_cursor: '' }, project, q)).toThrow(
      'invalid-response',
    )
  })
})
