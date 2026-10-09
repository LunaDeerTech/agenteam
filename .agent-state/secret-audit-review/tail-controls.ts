function barrier() {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}
function heldRaw(raw: string, kind: 'reader' | 'stream') {
  const response = new Response(raw, { status: 200, headers: { 'Content-Type': 'application/json' } })
  const body = response.body!, get = body.getReader.bind(body), cancel = body.cancel.bind(body)
  const entered = barrier(), release = barrier(), counts = { reader: 0, stream: 0, unlock: 0 }
  Object.defineProperty(body, 'getReader', { value: () => {
    const reader = get(), original = reader.cancel.bind(reader), unlock = reader.releaseLock.bind(reader)
    Object.defineProperty(reader, 'cancel', { value: async (...args: unknown[]) => {
      counts.reader++
      await original(...args)
      if (kind === 'reader') { entered.resolve(); await release.promise }
    } })
    Object.defineProperty(reader, 'releaseLock', { value: () => { counts.unlock++; return unlock() } })
    return reader
  } })
  Object.defineProperty(body, 'cancel', { value: async (...args: unknown[]) => {
    counts.stream++
    await cancel(...args)
    if (kind === 'stream') { entered.resolve(); await release.promise }
  } })
  return { response, entered, release, counts }
}

describe('independent Secret Audit actual raw-consumer retirement', () => {
  for (const mode of ['valid', 'duplicate', 'cancelled'] as const) {
    for (const kind of ['reader', 'stream'] as const) {
      it(`${mode} cannot settle ahead of its original ${kind} cancellation`, async () => {
        const row = record('update')
        let raw = JSON.stringify(row)
        if (mode === 'duplicate') raw = raw.replace('"version":"2"', '"version":"1","vers\\u0069on":"2"')
        const held = heldRaw(raw, kind), controller = new AbortController()
        let calls = 0, settled = false
        const api = createProjectAuditAPI(async () => { calls++; return held.response })
        const result = api.get(project, row.audit_id, controller.signal).then(
          (value) => { settled = true; return { value, error: null } },
          (error) => { settled = true; return { value: null, error } },
        )
        let outcome: Awaited<typeof result>
        try {
          await held.entered.promise
          if (mode === 'cancelled') controller.abort()
          await Promise.resolve()
          expect(settled).toBe(false)
          expect(calls).toBe(1)
          expect(held.counts).toEqual({ reader: 1, stream: kind === 'stream' ? 1 : 0, unlock: kind === 'stream' ? 1 : 0 })
        } finally {
          held.release.resolve()
          outcome = await result
        }
        expect(held.counts).toEqual({ reader: 1, stream: 1, unlock: 1 })
        expect(calls).toBe(1)
        // Outer-finally begins after parse has already computed its result.
        // The unchanged transport joins this tail but does not revoke that
        // result for a later abort; the baseline control below binds this limit.
        if (mode === 'valid' || (mode === 'cancelled' && kind === 'stream')) {
          expect(outcome.error).toBeNull()
          expect(outcome.value?.metadata).toEqual(row.metadata)
        } else {
          expect(outcome.value).toBeNull()
          expect(outcome.error).toMatchObject({ kind: mode === 'duplicate' ? 'invalid-response' : 'cancelled' })
        }
      })
    }
  }
  it('formal 8cb ordinary API also keeps the precomputed result after a late outer abort', async () => {
    const row = { ...record('update'), action: 'project.variable.update' }
    const held = heldRaw(JSON.stringify(row), 'stream'), controller = new AbortController()
    let settled = false, calls = 0
    const api = baselineAPI(async () => { calls++; return held.response })
    const result = api.get(project, row.audit_id, controller.signal).then(
      (value) => { settled = true; return { value, error: null } },
      (error) => { settled = true; return { value: null, error } },
    )
    let outcome: Awaited<typeof result>
    try {
      await held.entered.promise
      controller.abort()
      await Promise.resolve()
      expect(settled).toBe(false)
      expect(held.counts).toEqual({ reader: 1, stream: 1, unlock: 1 })
    } finally {
      held.release.resolve()
      outcome = await result
    }
    expect(calls).toBe(1)
    expect(outcome.error).toBeNull()
    expect(outcome.value?.action).toBe('project.variable.update')
  })
})
