function heldCancellation(value: unknown, kind: 'reader' | 'stream') {
  const started = barrier(), release = barrier()
  const response = json(value), body = response.body!
  const get = body.getReader.bind(body), cancel = body.cancel.bind(body)
  const counts = { reader: 0, stream: 0, released: 0 }
  Object.defineProperty(body, 'getReader', { value: () => {
    const reader = get(), originalCancel = reader.cancel.bind(reader), unlock = reader.releaseLock.bind(reader)
    Object.defineProperty(reader, 'cancel', { value: async (...args: unknown[]) => {
      counts.reader++
      await originalCancel(...args)
      if (kind === 'reader') { started.resolve(); await release.promise }
    } })
    Object.defineProperty(reader, 'releaseLock', { value: () => { counts.released++; return unlock() } })
    return reader
  } })
  Object.defineProperty(body, 'cancel', { value: async (...args: unknown[]) => {
    counts.stream++
    await cancel(...args)
    if (kind === 'stream') { started.resolve(); await release.promise }
  } })
  return { response, started, release, counts }
}

describe('independent Owner requalification actual cancellation tails', () => {
  for (const entry of ['session', 'owner'] as const) {
    for (const kind of ['reader', 'stream'] as const) {
      it(`${entry} ${kind} tail blocks Owner/Variables until actual release`, async () => {
        const f = await fixture()
        await f.page.select(target)
        f.page.draft.value = 'retained under original identity'
        const archived = { ...project(), lifecycle: 'archived' as const, archived_at: at }
        const held = heldCancellation(entry === 'session' ? view() : archived, kind)
        const ownerCalls = vi.fn<Fetch>(async () => entry === 'owner' ? held.response : json(archived))
        f.ownerCall(ownerCalls)
        if (entry === 'session') f.sessionCall(async () => held.response)
        const before = variableCalls(f.fetcher).length
        const restore = f.auth.restore()
        try {
          await held.started.promise
          await settle()
          expect(f.auth.state.busy).toBe(true)
          expect(f.page.visible.value).toBe(false)
          expect(f.page.canMutate.value).toBe(false)
          expect(f.page.canSave.value).toBe(false)
          expect(ownerCalls).toHaveBeenCalledTimes(entry === 'session' ? 0 : 1)
          expect(await f.page.createNew()).toBe(false)
          await f.page.readOwner()
          await f.page.firstPage()
          await f.page.select(target)
          expect(variableCalls(f.fetcher)).toHaveLength(before)
          expect(ownerCalls).toHaveBeenCalledTimes(entry === 'session' ? 0 : 1)
          expect(f.page.draft.value).toBe('retained under original identity')
          expect(held.counts).toEqual({ reader: 1, stream: kind === 'stream' ? 1 : 0, released: kind === 'stream' ? 1 : 0 })
        } finally {
          held.release.resolve()
          await restore
          await settle()
        }
        expect(held.counts).toEqual({ reader: 1, stream: 1, released: 1 })
        expect(ownerCalls).toHaveBeenCalledTimes(1)
        expect(f.auth.state.busy).toBe(false)
        expect(f.page.visible.value).toBe(true)
        expect(f.page.canMutate.value).toBe(false)
        expect(f.page.currentProject.value?.lifecycle).toBe('archived')
        expect(f.page.draft.value).toBe('retained under original identity')
        expect(variableCalls(f.fetcher)).toHaveLength(before)
      })
    }
  }
})
