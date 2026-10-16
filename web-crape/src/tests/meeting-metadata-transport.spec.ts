import { describe, expect, it, vi } from 'vitest'
import { accountTransport, type Fetch } from '../api/client'
import { createMeetingsAPI, type MeetingCommand } from '../api/meetings'

const id = (n: number) => `01990000-0000-7000-8000-${n.toString(16).padStart(12, '0')}`
const project = id(1)
const meeting = id(2)
const receipt = { meeting_id: meeting, version: '9007199254740994', replayed: false }
const response = (body: unknown) =>
  new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(99) },
  })
const write = () => ({
  csrfToken: 'S'.repeat(43),
  key: id(5),
  signal: new AbortController().signal,
})

const cases: ReadonlyArray<{ name: string; command: MeetingCommand; path: string }> = [
  {
    name: 'participants',
    path: 'participants',
    command: {
      kind: 'participants',
      meeting,
      input: { expected_version: '9007199254740993', agents: [id(3), id(4)] },
    },
  },
  {
    name: 'empty participants',
    path: 'participants',
    command: { kind: 'participants', meeting, input: { expected_version: '2', agents: [] } },
  },
  {
    name: 'references',
    path: 'references',
    command: {
      kind: 'references',
      meeting,
      input: {
        expected_version: '9007199254740993',
        references: [
          { kind: 'task', id: id(3), label: '原任务' },
          { kind: 'knowledge', id: id(4) },
          { kind: 'file', id: id(6) },
          { kind: 'link', id: 'https://example.test/docs?a=1#part' },
        ],
      },
    },
  },
  {
    name: 'empty references',
    path: 'references',
    command: { kind: 'references', meeting, input: { expected_version: '2', references: [] } },
  },
]

describe('Meeting metadata transport', () => {
  it.each(cases)(
    '$name uses the real API and original passive lookup',
    async ({ command, path }) => {
      const fetcher = vi.fn<Fetch>(async (url) =>
        response(url.endsWith('/lookup') ? { status: 'committed', receipt } : receipt),
      )
      const api = createMeetingsAPI(fetcher)
      const options = write()
      await expect(api.command(project, command, options)).resolves.toEqual(receipt)
      await expect(api.lookup(project, command, options)).resolves.toEqual({
        status: 'committed',
        receipt,
      })
      expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
        `/api/v1/projects/${project}/meetings/${meeting}/${path}`,
        `/api/v1/projects/${project}/meetings/${meeting}/${path}/lookup`,
      ])
      for (const [, request] of fetcher.mock.calls) {
        expect(request.method).toBe('POST')
        expect(JSON.parse(String(request.body))).toEqual(command.input)
        expect(new Headers(request.headers).get('Idempotency-Key')).toBe(options.key)
        expect(new Headers(request.headers).get('X-CSRF-Token')).toBe(options.csrfToken)
        expect(request.signal).toBe(options.signal)
      }
    },
  )

  it.each(['meetingParticipants', 'meetingParticipantsLookup'] as const)(
    '%s rejects malformed participants before fetch',
    async (endpoint) => {
      const fetcher = vi.fn<Fetch>(async () => response(receipt))
      const call = accountTransport(fetcher)
      for (const body of [
        { expected_version: '1' },
        { expected_version: '1', agents: null },
        { expected_version: '0', agents: [] },
        { expected_version: '1', agents: [id(3), id(3)] },
        { expected_version: '1', agents: ['other'] },
        { expected_version: '1', agents: Array.from({ length: 33 }, (_, n) => id(n + 10)) },
        { expected_version: '1', agents: [], references: [] },
      ]) {
        await expect(
          call(endpoint, (v) => v, {
            projectID: project,
            target: meeting,
            signal: new AbortController().signal,
            csrf: 'S'.repeat(43),
            key: id(5),
            body,
          }),
        ).rejects.toMatchObject({ kind: 'invalid-input' })
      }
      expect(fetcher).not.toHaveBeenCalled()
    },
  )

  it.each(['meetingReferences', 'meetingReferencesLookup'] as const)(
    '%s rejects malformed references and foreign fields before fetch',
    async (endpoint) => {
      const fetcher = vi.fn<Fetch>(async () => response(receipt))
      const call = accountTransport(fetcher)
      for (const references of [
        null,
        [{ kind: 'agent', id: id(3) }],
        [{ kind: 'file', id: 'https://example.test' }],
        [{ kind: 'link', id: 'javascript:alert(1)' }],
        [{ kind: 'link', id: 'https://user:pass@example.test' }],
        [{ kind: 'link', id: 'https://@example.test' }],
        [{ kind: 'link', id: ' https://example.test' }],
        [{ kind: 'task', id: id(3), label: '\0' }],
        [{ kind: 'task', id: id(3), extra: true }],
        [
          { kind: 'task', id: id(3) },
          { kind: 'task', id: id(3) },
        ],
        Array.from({ length: 257 }, (_, n) => ({ kind: 'task', id: id(n + 10) })),
      ]) {
        await expect(
          call(endpoint, (v) => v, {
            projectID: project,
            target: meeting,
            signal: new AbortController().signal,
            csrf: 'S'.repeat(43),
            key: id(5),
            body: { expected_version: '1', references },
          }),
        ).rejects.toMatchObject({ kind: 'invalid-input' })
      }
      expect(fetcher).not.toHaveBeenCalled()
    },
  )
})
