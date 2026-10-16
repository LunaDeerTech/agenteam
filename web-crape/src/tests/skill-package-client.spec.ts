import { describe, expect, it, vi } from 'vitest'
import { accountTransport, skillPackageMaximum, type Fetch } from '../api/client'
import { createSkillPackageAPI } from '../api/skill-package'

const project = '01970000-0000-7000-8000-000000000001',
  target = '01970000-0000-7000-8000-000000000002',
  requestID = '01970000-0000-7000-8000-000000000003'
const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04, 1, 2, 3, 4])
const headers = () => ({
  'Content-Type': 'application/zip',
  'Content-Length': String(bytes.length),
  'Content-Disposition': 'attachment; filename="skill-package.zip"',
  'Cache-Control': 'no-store',
  'X-Content-Type-Options': 'nosniff',
  'X-Request-ID': requestID,
})
const signal = () => new AbortController().signal
describe('fixed Skill package download transport', () => {
  it('preserves the int64 revision and returns a complete bounded ZIP using the Cookie owner transport', async () => {
    const fetch = vi.fn<Fetch>(async () => new Response(bytes, { headers: headers() }))
    const progress = vi.fn(),
      blob = await createSkillPackageAPI(fetch).get(
        project,
        target,
        '9007199254740993',
        signal(),
        progress,
      )
    expect(blob.size).toBe(bytes.length)
    expect(blob.type).toBe('application/zip')
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0]![0]).toBe(
      `/api/v1/projects/${project}/skills/${target}/revisions/9007199254740993/package`,
    )
    expect(fetch.mock.calls[0]![1]).toMatchObject({
      method: 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
    })
    expect(fetch.mock.calls[0]![1].body).toBeUndefined()
    expect(new Headers(fetch.mock.calls[0]![1].headers).has('Range')).toBe(false)
    expect(new Headers(fetch.mock.calls[0]![1].headers).has('Idempotency-Key')).toBe(false)
    expect(progress).toHaveBeenLastCalledWith(bytes.length, bytes.length)
  })
  it('rejects bad revisions and extra caller capabilities before fetch', async () => {
    const fetch = vi.fn<Fetch>(),
      api = createSkillPackageAPI(fetch)
    for (const revision of ['0', '01', '1\n', '1/other', '9223372036854775808'])
      expect(() => api.get(project, target, revision, signal())).toThrow()
    await expect(
      accountTransport(fetch)('getSkillPackage', (v) => v, {
        signal: signal(),
        projectID: project,
        target,
        revision: '1',
        body: 'unexpected',
      } as never),
    ).rejects.toMatchObject({ kind: 'invalid-input' })
    expect(fetch).not.toHaveBeenCalled()
  })
  it('rejects unsafe headers and incomplete or excess bytes without returning a package', async () => {
    const edits: Record<string, string>[] = [
      { 'Content-Type': 'text/html' },
      { 'Content-Length': String(skillPackageMaximum + 1) },
      { 'Content-Disposition': 'inline' },
      { 'Cache-Control': 'public' },
      { 'X-Content-Type-Options': '' },
      { 'Content-Encoding': 'gzip' },
      { 'Content-Range': 'bytes 0-7/8' },
      { 'X-Request-ID': 'bad' },
    ]
    for (const edit of edits) {
      await expect(
        createSkillPackageAPI(
          async () => new Response(bytes, { headers: { ...headers(), ...edit } }),
        ).get(project, target, '1', signal()),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
    }
    for (const body of [
      bytes.slice(0, 7),
      new Uint8Array([...bytes, 9]),
      new Uint8Array(bytes.length),
    ])
      await expect(
        createSkillPackageAPI(async () => new Response(body, { headers: headers() })).get(
          project,
          target,
          '1',
          signal(),
        ),
      ).rejects.toMatchObject({ kind: 'invalid-response' })
  })
  it('waits for actual cancellation before releasing the download operation', async () => {
    let release!: () => void
    const retired = new Promise<void>((resolve) => {
        release = resolve
      }),
      cancelled = vi.fn(() => retired)
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(bytes.slice(0, 4))
      },
      cancel: cancelled,
    })
    const controller = new AbortController(),
      progress = vi.fn()
    const result = createSkillPackageAPI(
      async () => new Response(stream, { headers: headers() }),
    ).get(project, target, '1', controller.signal, progress)
    const rejected = expect(result).rejects.toMatchObject({ kind: 'cancelled' })
    await vi.waitFor(() => expect(progress).toHaveBeenLastCalledWith(4, 8))
    let settled = false
    void result.then(
      () => {
        settled = true
      },
      () => {
        settled = true
      },
    )
    controller.abort()
    await vi.waitFor(() => expect(cancelled).toHaveBeenCalledTimes(1))
    expect(settled).toBe(false)
    release()
    await rejected
  })
})
