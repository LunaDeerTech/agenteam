import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createAccountAPI } from '../api/account'
import { createAgentDirectoryAPI } from '../api/agent-directory'

const project = '01990000-0000-7000-8000-000000000001'
const encode = (value: string) => new TextEncoder().encode(value)
function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((done) => (resolve = done))
  return { promise, resolve }
}
function observed<T>(pending: Promise<T>) {
  let settled = false
  const result = pending.then(
    (value) => {
      settled = true
      return { value, error: undefined }
    },
    (error: unknown) => {
      settled = true
      return { value: undefined, error }
    },
  )
  return { result, settled: () => settled }
}
function watchBody(stream: ReadableStream<Uint8Array>) {
  const readers: ReadableStreamDefaultReader<Uint8Array>[] = []
  const getReader = stream.getReader.bind(stream)
  const bodyCancel = vi.spyOn(stream, 'cancel')
  const acquire = vi.spyOn(stream, 'getReader').mockImplementation(() => {
    const reader = getReader()
    // A closed stream's underlying cancel callback is a no-op. Observe the
    // actual methods too, retaining their native behavior and return promises.
    vi.spyOn(reader, 'cancel')
    readers.push(reader)
    return reader
  })
  return { readers, bodyCancel, acquire }
}
const directory = (response: Response, signal = new AbortController().signal) =>
  createAgentDirectoryAPI(async () => response).list(project, {}, signal)
const response = (stream: ReadableStream<Uint8Array>, media = 'application/json') =>
  new Response(stream, { headers: { 'Content-Type': media } })
function completedBody(bytes?: Uint8Array) {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      if (bytes) controller.enqueue(bytes)
      controller.close()
    },
  })
}
function expectEOF(stream: ReadableStream<Uint8Array>, calls: ReturnType<typeof watchBody>) {
  expect(calls.readers).toHaveLength(1)
  expect(calls.readers[0]!.cancel).not.toHaveBeenCalled()
  expect(calls.bodyCancel).not.toHaveBeenCalled()
  expect(stream.locked).toBe(false)
}

afterEach(() => vi.restoreAllMocks())

describe('public API response body ownership', () => {
  it('does not cancel after JSON EOF, including JSON and DTO rejection', async () => {
    for (const text of ['{"items":[]}', '{', '{"items":null}']) {
      const stream = completedBody(encode(text))
      const calls = watchBody(stream)
      const result = await observed(directory(response(stream))).result
      if (text === '{"items":[]}') {
        expect(result.error).toBeUndefined()
        expect(result.value).toEqual({ items: [] })
      } else {
        expect(result.error).toMatchObject({ kind: 'invalid-response' })
        expect(result.value).toBeUndefined()
      }
      expectEOF(stream, calls)
    }
  })

  it('cancels a pre-EOF decoding failure once and awaits the actual cancellation', async () => {
    const release = deferred()
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([0xff]))
      },
      cancel() {
        return release.promise
      },
    })
    const calls = watchBody(stream)
    const abort = new AbortController()
    const job = observed(directory(response(stream), abort.signal))
    try {
      await vi.waitFor(() => expect(calls.readers[0]?.cancel).toHaveBeenCalledOnce())
      await flushPromises()
      expect(job.settled()).toBe(false)
      expect(stream.locked).toBe(true)
      expect(calls.readers[0]!.cancel).toHaveBeenCalledOnce()
      expect(calls.bodyCancel).not.toHaveBeenCalled()
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'invalid-response' })
      expect(calls.readers[0]!.cancel).toHaveBeenCalledOnce()
      expect(calls.bodyCancel).not.toHaveBeenCalled()
      expect(stream.locked).toBe(false)
    } finally {
      release.resolve()
      abort.abort()
      await job.result
    }
  })

  it('keeps an aborted read owned until its one reader cancellation returns', async () => {
    const release = deferred()
    const stream = new ReadableStream<Uint8Array>({
      cancel() {
        return release.promise
      },
    })
    const calls = watchBody(stream)
    const abort = new AbortController()
    const job = observed(directory(response(stream), abort.signal))
    try {
      await vi.waitFor(() => expect(calls.readers).toHaveLength(1))
      abort.abort()
      await vi.waitFor(() => expect(calls.readers[0]!.cancel).toHaveBeenCalledOnce())
      await flushPromises()
      expect(job.settled()).toBe(false)
      expect(stream.locked).toBe(true)
      expect(calls.readers[0]!.cancel).toHaveBeenCalledOnce()
      expect(calls.bodyCancel).not.toHaveBeenCalled()
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'cancelled' })
      expect(calls.readers[0]!.cancel).toHaveBeenCalledOnce()
      expect(calls.bodyCancel).not.toHaveBeenCalled()
      expect(stream.locked).toBe(false)
    } finally {
      release.resolve()
      abort.abort()
      await job.result
    }
  })

  it('awaits unread-body cancellation when media rejection precedes reader acquisition', async () => {
    const release = deferred()
    const stream = new ReadableStream<Uint8Array>({
      cancel() {
        return release.promise
      },
    })
    const calls = watchBody(stream)
    const abort = new AbortController()
    const job = observed(directory(response(stream, 'text/html'), abort.signal))
    try {
      await vi.waitFor(() => expect(calls.bodyCancel).toHaveBeenCalledOnce())
      await flushPromises()
      expect(job.settled()).toBe(false)
      expect(calls.acquire).not.toHaveBeenCalled()
      expect(calls.bodyCancel).toHaveBeenCalledOnce()
      release.resolve()
      expect((await job.result).error).toMatchObject({ kind: 'invalid-response' })
      expect(calls.bodyCancel).toHaveBeenCalledOnce()
      expect(stream.locked).toBe(false)
    } finally {
      release.resolve()
      abort.abort()
      await job.result
    }
  })

  it('accepts an actual empty 204 stream at EOF without cancelling it', async () => {
    const stream = completedBody()
    const calls = watchBody(stream)
    const result = response(stream)
    // Response's constructor forbids a 204 stream; Fetch implementations may
    // expose one. Preserve the native stream while modelling that status.
    Object.defineProperty(result, 'status', { value: 204 })
    await createAccountAPI(async () => result).completePasswordReset(
      {
        token: project + '.' + 'T'.repeat(43),
        new_password: 'Valid password 12345',
        confirmation: 'Valid password 12345',
      },
      { csrfToken: 'S'.repeat(43), key: 'original-reset-key' },
    )
    expectEOF(stream, calls)
  })

  it('does not cancel avatar EOF on either a complete body or a size mismatch', async () => {
    for (const length of ['4', '5']) {
      const stream = completedBody(new Uint8Array([137, 80, 78, 71]))
      const calls = watchBody(stream)
      const result = new Response(stream, {
        headers: {
          'Content-Type': 'image/png',
          'Content-Length': length,
          ETag: '"sha256:' + 'a'.repeat(64) + '"',
        },
      })
      const outcome = await observed(createAccountAPI(async () => result).readAvatar()).result
      if (length === '4') {
        expect(outcome.error).toBeUndefined()
        expect(outcome.value?.blob.size).toBe(4)
        expect(outcome.value?.metadata.byte_size).toBe('4')
      } else {
        expect(outcome.error).toMatchObject({ kind: 'invalid-response' })
        expect(outcome.value).toBeUndefined()
      }
      expectEOF(stream, calls)
    }
  })
})
