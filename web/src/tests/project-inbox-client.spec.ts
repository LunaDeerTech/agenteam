import { describe, expect, it, vi } from 'vitest'
import { AccountFailure, accountTransport, type Fetch } from '../api/client'
import { captureInboxQuery, createProjectInboxAPI, decodeInboxPage } from '../api/project-inbox'
import {
  inboxBase,
  inboxDecision,
  inboxID,
  inboxItem,
  inboxJSON,
  inboxPage,
  inboxProject,
} from './project-inbox.fixture'

describe('project Inbox closed read contract', () => {
  it('reads a complete current-project page through the bounded credentialed GET and preserves versions', async () => {
    const fetch = vi.fn<Fetch>(async () => inboxJSON(inboxPage()))
    const result = await createProjectInboxAPI(fetch).list(
      inboxProject,
      {},
      new AbortController().signal,
    )
    expect(fetch.mock.calls[0]![0]).toBe(inboxBase + '?status=open&limit=50')
    expect(fetch.mock.calls[0]![1]).toMatchObject({ method: 'GET', credentials: 'same-origin' })
    expect(fetch.mock.calls[0]![1].body).toBeUndefined()
    expect(new Headers(fetch.mock.calls[0]![1].headers).has('X-CSRF-Token')).toBe(false)
    expect(result.items[0]!.source_version).toBe('9007199254740993')
    expect(Object.isFrozen(result.items)).toBe(true)
    expect(result.next_before).toBeNull()
    expect(decodeInboxPage({ items: [] }, inboxProject, captureInboxQuery({})).items).toEqual([])
  })
  it('rejects foreign source scope, malformed tails, duplicate navigation and pagination cycles before publishing', () => {
    const row = inboxItem(),
      query = captureInboxQuery({ limit: 1 })
    for (const patch of [
      { project_id: inboxID(9) },
      { source_type: 'approval' },
      { action_type: 'meeting.approve' },
      { dismissible: true },
      { status: 'resolved' },
      { resolved_at: row.created_at },
      { source_id: '../../elsewhere' },
      { source_version: 1 },
      { created_at: '2026-02-30T12:00:00Z' },
      { summary: '\ud800' },
      { hidden_token: 'never' },
    ])
      expect(() =>
        decodeInboxPage(inboxPage([{ ...row, ...patch }] as never), inboxProject, query),
      ).toThrow(AccountFailure)
    for (const value of [
      inboxPage([row, row]),
      inboxPage([], row.id),
      inboxPage([row], inboxID(99)),
    ])
      expect(() => decodeInboxPage(value, inboxProject, query)).toThrow(AccountFailure)
    expect(() =>
      decodeInboxPage(
        inboxPage([row], row.id),
        inboxProject,
        captureInboxQuery({ limit: 1, before: row.id }),
      ),
    ).toThrow(AccountFailure)
    const normalized = decodeInboxPage(
      inboxPage([{ ...row, created_at: '2026-10-11T12:00:00Z' }]),
      inboxProject,
      query,
    )
    expect(normalized.items[0]!.created_at).toBe('2026-10-11T12:00:00.000000Z')
  })
  it('rejects unregistered filters and forged paths at both API and transport boundaries without fetching', async () => {
    const fetch = vi.fn<Fetch>(),
      signal = new AbortController().signal
    for (const value of [
      { status: 'dismissed' },
      { limit: 0 },
      { limit: 101 },
      { before: '/other' },
      { target: inboxID(9) },
    ])
      expect(() => captureInboxQuery(value)).toThrow(AccountFailure)
    await expect(
      accountTransport(fetch)('listProjectInbox', (value) => value, {
        signal,
        projectID: '../session',
        inboxQuery: { status: 'open', limit: 50 },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    await expect(
      accountTransport(fetch)('listProjectInbox', (value) => value, {
        signal,
        projectID: inboxProject,
        inboxQuery: { status: 'open', limit: 50, before: '/bad' },
      }),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('routes review and original lookup only to Meeting with an explicit boolean decision', async () => {
    const fetch = vi.fn<Fetch>(async () => inboxJSON({ status: 'not_found' })),
      transport = accountTransport(fetch)
    const scope = {
      signal: new AbortController().signal,
      projectID: inboxProject,
      target: inboxID(1100),
    }
    await transport('meetingProposal', (value) => value, scope)
    const write = {
      ...scope,
      csrf: 'S'.repeat(43),
      key: 'proposal-original-review',
      body: { expected_version: '1', approve: false },
    }
    for (const endpoint of ['meetingProposalReview', 'meetingProposalReviewLookup'] as const)
      await transport(endpoint, (value) => value, write)
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      `/api/v1/projects/${inboxProject}/meetings/${scope.target}/proposal`,
      `/api/v1/projects/${inboxProject}/meetings/${scope.target}/proposal/review`,
      `/api/v1/projects/${inboxProject}/meetings/${scope.target}/proposal/review/lookup`,
    ])
    expect(fetch.mock.calls[1]![1].body).toBe('{"expected_version":"1","approve":false}')
    expect(fetch.mock.calls[2]![1].body).toBe(fetch.mock.calls[1]![1].body)
    for (const body of [
      { expected_version: '1' },
      { expected_version: '1', approve: null },
      { expected_version: '0', approve: false },
      { expected_version: '1', approve: false, source_id: scope.target },
    ])
      await expect(
        transport('meetingProposalReview', (value) => value, { ...write, body }),
      ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).toHaveBeenCalledTimes(3)
  })
  it('accepts only paired source contexts and preserves mixed blocking order', () => {
    const proposal = inboxItem(),
      decision = inboxDecision()
    const query = captureInboxQuery({})
    const page = decodeInboxPage(inboxPage([decision, proposal]), inboxProject, query)
    expect(page.items.map((item) => item.source_type)).toEqual(['decision_request', 'meeting'])
    expect(Object.isFrozen(page.items[0]!.source_context)).toBe(true)
    for (const context of [
      {},
      null,
      { meeting_id: '/other' },
      { meeting_id: inboxID(2200), url: '/other' },
    ]) {
      expect(() =>
        decodeInboxPage(
          inboxPage([{ ...decision, source_context: context }] as never),
          inboxProject,
          query,
        ),
      ).toThrow(AccountFailure)
    }
    expect(() =>
      decodeInboxPage(
        inboxPage([{ ...proposal, source_context: decision.source_context }] as never),
        inboxProject,
        query,
      ),
    ).toThrow(AccountFailure)
    expect(() =>
      decodeInboxPage(
        inboxPage([{ ...decision, action_type: 'meeting.review_proposal' }] as never),
        inboxProject,
        query,
      ),
    ).toThrow(AccountFailure)
    expect(() => decodeInboxPage(inboxPage([proposal, decision]), inboxProject, query)).toThrow(
      AccountFailure,
    )
    expect(() =>
      decodeInboxPage(inboxPage([{ ...decision, blocking: false }]), inboxProject, query),
    ).toThrow(AccountFailure)
    // Canonical source identity is the closed pair, not a global bare UUID.
    expect(
      decodeInboxPage(
        inboxPage([{ ...decision, source_id: proposal.source_id }, proposal]),
        inboxProject,
        query,
      ).items,
    ).toHaveLength(2)
  })
})
