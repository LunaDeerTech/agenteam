import { describe, expect, it, vi } from 'vitest'
import type { Fetch } from '../api/client'
import { createTaskExecutionOutputAPI } from '../api/task-execution-output'
import { execution, id, json, p, problem, t } from './task-execution-fixture'
import { finalExecution, output, outputPath } from './task-execution-output-fixture'
describe('Task Execution final output wire', () => {
  it('reads the exact no-query GET and preserves full text above the small summary limit', async () => {
    const content = '中🙂 <script>literal</script>\n' + 'a'.repeat(1024 * 1024)
    const value = output(content),
      fetch = vi.fn<Fetch>(async () => json(value))
    const result = await createTaskExecutionOutputAPI(fetch).get(
      p,
      t,
      id(100),
      new AbortController().signal,
    )
    expect(result).toEqual(value)
    expect(result.snapshot?.items[0]?.data.content).toBe(content)
    expect(fetch.mock.calls[0]?.[0]).toBe(outputPath)
    const init = fetch.mock.calls[0]![1]
    expect(init.method).toBe('GET')
    expect(init.body).toBeUndefined()
    expect(init.credentials).toBe('same-origin')
    expect(new Headers(init.headers).has('Idempotency-Key')).toBe(false)
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
  })
  it('distinguishes pending, missing historical projection, successful empty text and terminal empty items', async () => {
    const values = [
      { execution: execution(), availability: 'pending', snapshot: null },
      {
        execution: finalExecution({ started_at: null, reason: null }),
        availability: 'not_recorded',
        snapshot: null,
      },
      output(''),
      output('', finalExecution({ status: 'failed', reason: 'model_failed' })),
      output('', finalExecution({ status: 'cancelled', reason: 'cancelled' })),
    ]
    for (const value of values)
      expect(
        await createTaskExecutionOutputAPI(async () => json(value)).get(
          p,
          t,
          id(100),
          new AbortController().signal,
        ),
      ).toEqual(value)
  })
  it('rejects mismatched identities, versions, timestamps and undeclared or partial projection material', async () => {
    const original = output(),
      snapshot = original.snapshot!,
      item = snapshot.items[0]!
    const values = [
      { ...original, execution: finalExecution({ project_id: id(90) }) },
      { ...original, execution: finalExecution({ task_id: id(91) }) },
      { ...original, execution: finalExecution({ id: id(92) }) },
      { ...original, execution: finalExecution({ started_at: null }) },
      { ...original, execution: finalExecution({ reason: null }) },
      output('', finalExecution({ status: 'failed', reason: 'model_failed', started_at: null })),
      output('', finalExecution({ status: 'cancelled', reason: null })),
      {
        ...original,
        execution: finalExecution({
          started_at: '2026-10-13T10:00:00.000002Z',
          completed_at: '2026-10-13T10:00:00.000004Z',
        }),
        snapshot: {
          ...snapshot,
          items: [
            {
              ...item,
              started_at: '2026-10-13T10:00:00.000003Z',
              updated_at: '2026-10-13T10:00:00.000004Z',
              completed_at: '2026-10-13T10:00:00.000004Z',
            },
          ],
        },
      },
      { ...original, availability: 'pending', snapshot: null },
      { ...original, execution: execution(), availability: 'not_recorded', snapshot: null },
      { ...original, snapshot: null },
      ...[
        { schema: 'live' },
        { generation: id(201) },
        { execution_version: '2' },
        { included_update_progress: 1 },
        { items: [] },
        { items: [item, item] },
      ].map((patch) => ({ ...original, snapshot: { ...snapshot, ...patch } })),
      ...[
        { execution_id: id(101) },
        { revision: '2' },
        { seq: '2' },
        { status: 'streaming' },
        { updated_at: '2026-10-13T10:00:00.000002Z' },
        { completed_at: null },
        { source_reference: { kind: 'execution_round', id: id(301) } },
        { data: { content: 'visible', reasoning: 'private' } },
        { data: { content: '\ud800' } },
      ].map((patch) => ({
        ...original,
        snapshot: { ...snapshot, items: [{ ...item, ...patch }] },
      })),
      { ...original, execution: finalExecution({ status: 'failed', reason: 'model_failed' }) },
      { ...original, prompt: 'private' },
    ]
    for (const value of values)
      await expect(
        createTaskExecutionOutputAPI(async () => json(value)).get(
          p,
          t,
          id(100),
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('rejects oversized or incomplete encoded bodies, invalid IDs and safe permission failures without publishing partial output', async () => {
    let response = new Response('{"execution":', {
      headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(999) },
    })
    const fetch = vi.fn<Fetch>(async () => response),
      api = createTaskExecutionOutputAPI(fetch),
      signal = new AbortController().signal
    await expect(api.get(p, t, '../foreign', signal)).rejects.toMatchObject({
      kind: 'invalid-input',
    })
    expect(fetch).not.toHaveBeenCalled()
    await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    const cancel = vi.fn()
    response = new Response(
      new ReadableStream({
        start(controller) {
          controller.enqueue(new Uint8Array(16 * 1024 * 1024 + 64 * 1024 + 1))
        },
        cancel,
      }),
      { headers: { 'Content-Type': 'application/json', 'X-Request-ID': id(999) } },
    )
    await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({ kind: 'invalid-response' })
    expect(cancel).toHaveBeenCalledTimes(1)
    response = problem('PERMISSION_DENIED', 403)
    await expect(api.get(p, t, id(100), signal)).rejects.toMatchObject({
      kind: 'problem',
      problem: { status: 403, code: 'PERMISSION_DENIED' },
    })
  })
})
