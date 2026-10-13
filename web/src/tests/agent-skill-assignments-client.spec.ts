import { describe, expect, it, vi } from 'vitest'
import { createAgentSkillAssignmentsAPI } from '../api/agent-skill-assignments'
import type { Fetch } from '../api/client'
import {
  agent,
  assignment,
  base,
  command,
  id,
  json,
  project,
  receipt,
} from './agent-skill-assignments.fixture'
const signal = () => new AbortController().signal
const write = { key: id(90), csrfToken: 'S'.repeat(43), signal: signal() }
describe('Agent Skills wire contract', () => {
  it('preserves disabled assignments and server pagination without inventing a total', async () => {
    const fetch = vi.fn<Fetch>(async () =>
      json({
        project_id: project,
        agent_id: agent,
        agent_version: '9007199254740993',
        assignment_sequence: '2',
        items: [assignment(30, false)],
        next_cursor: 'opaque+/=',
      }),
    )
    const api = createAgentSkillAssignmentsAPI(fetch)
    const page = await api.list(project, agent, { limit: 1 }, signal())
    expect(page.items[0]?.enabled).toBe(false)
    expect(page.agent_version).toBe('9007199254740993')
    expect(page).not.toHaveProperty('total')
    await api.list(project, agent, { limit: 1, cursor: 'different' }, signal())
    expect(fetch.mock.calls[1]?.[0]).toBe(`${base}?limit=1&cursor=different`)
    expect(Object.isFrozen(page.items[0])).toBe(true)
  })
  it('rejects wrong-scope, duplicate and nonterminal-null pagination observations', async () => {
    const page = {
      project_id: project,
      agent_id: agent,
      agent_version: '1',
      assignment_sequence: '1',
      items: [assignment()],
    }
    for (const body of [
      { ...page, agent_id: id(21) },
      { ...page, items: [assignment(), assignment()] },
      { ...page, next_cursor: null },
    ]) {
      const api = createAgentSkillAssignmentsAPI(async () => json(body))
      await expect(api.list(project, agent, {}, signal())).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
  })
  it('requires truthful enable runtime, original scope and version in direct and Lookup receipts', async () => {
    const c = command(),
      r = receipt(c)
    for (const wrong of [
      { ...r, runtime: null },
      { ...r, agent_version: '3' },
      { ...r, assignment: { ...r.assignment!, enabled: false } },
      { ...r, runtime: { change_id: id(50), queued: true, execution_id: null } },
    ]) {
      const direct = createAgentSkillAssignmentsAPI(async () => json(wrong))
      await expect(direct.execute(project, agent, c, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
      const lookup = createAgentSkillAssignmentsAPI(async () =>
        json({ state: 'committed', receipt: wrong }),
      )
      await expect(lookup.lookup(project, agent, c, write)).rejects.toMatchObject({
        kind: 'invalid-response',
      })
    }
    const fetch = vi.fn<Fetch>(async () => json(receipt(command(false))))
    await createAgentSkillAssignmentsAPI(fetch).execute(project, agent, command(false), write)
    expect(fetch.mock.calls[0]?.[0]).toBe(base + '/assign')
    expect(JSON.parse(fetch.mock.calls[0]![1].body as string)).toEqual(command(false).input)
  })
  it('accepts exact retired-grant removal no-op and rejects malformed requests before transport', async () => {
    const remove = {
      kind: 'remove' as const,
      input: { expected_version: '1', request: { assignment_id: id(1030) } },
    }
    const fetch = vi.fn<Fetch>(async () => json(receipt(remove, false)))
    const api = createAgentSkillAssignmentsAPI(fetch)
    expect(await api.execute(project, agent, remove, write)).toMatchObject({
      changed: false,
      assignment: null,
      runtime: null,
      agent_version: '1',
      assignment_sequence: '1',
    })
    expect(() => api.list(project, agent, { limit: 101 }, signal())).toThrow()
    expect(() =>
      api.execute(
        project,
        agent,
        { ...remove, input: { ...remove.input, expected_version: '01' } },
        write,
      ),
    ).toThrow()
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
