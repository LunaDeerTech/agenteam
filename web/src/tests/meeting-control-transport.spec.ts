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

describe.each(['cancel-contribution', 'retry-contribution', 'regenerate-contribution'] as const)(
  'Meeting %s transport',
  (action) => {
    const input = {
      action,
      expected_version: '9007199254740993',
      turn_id: id(3),
      contribution_id: id(4),
    }
    const command: MeetingCommand = { kind: 'control', meeting, input }
    it('dispatches the actual API and original lookup with the same scoped input and key', async () => {
      const fetcher = vi.fn<Fetch>(async (url) =>
        response(url.endsWith('/lookup') ? { status: 'committed', receipt } : receipt),
      )
      const api = createMeetingsAPI(fetcher)
      const signal = new AbortController().signal
      const write = { csrfToken: 'S'.repeat(43), key: id(5), signal }
      await expect(api.command(project, command, write)).resolves.toEqual(receipt)
      await expect(api.lookup(project, command, write)).resolves.toEqual({
        status: 'committed',
        receipt,
      })
      expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
        `/api/v1/projects/${project}/meetings/${meeting}/controls`,
        `/api/v1/projects/${project}/meetings/${meeting}/controls/lookup`,
      ])
      for (const [, request] of fetcher.mock.calls) {
        expect(request.method).toBe('POST')
        expect(JSON.parse(String(request.body))).toEqual(input)
        expect(new Headers(request.headers).get('Idempotency-Key')).toBe(write.key)
        expect(new Headers(request.headers).get('X-CSRF-Token')).toBe(write.csrfToken)
      }
    })

    it.each(['meetingControl', 'meetingControlLookup'] as const)(
      '%s rejects missing or foreign-shaped scope before fetch',
      async (endpoint) => {
        const fetcher = vi.fn<Fetch>(async () => response(receipt))
        const call = accountTransport(fetcher)
        const { turn_id: _turn, ...withoutTurn } = input
        const { contribution_id: _contribution, ...withoutContribution } = input
        for (const body of [
          withoutTurn,
          withoutContribution,
          { ...input, turn_id: 'not-an-id' },
          { ...input, contribution_id: 'not-an-id' },
          { ...input, action: 'cancel-turn' },
          { ...input, action: 'archive' },
          { ...input, action: 'cancel-everything' },
        ]) {
          await expect(
            call(endpoint, (value) => value, {
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
  },
)
