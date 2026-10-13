import { afterEach, describe, expect, it, vi } from 'vitest'
import { randomUUID } from '../utils/random-uuid'

afterEach(() => vi.unstubAllGlobals())

describe('secure random UUID generation', () => {
  it('uses the native UUID generator when available', () => {
    const native = vi.fn(() => '01234567-89ab-4cde-8fab-0123456789ab')
    const getRandomValues = vi.fn()
    vi.stubGlobal('crypto', { randomUUID: native, getRandomValues })

    expect(randomUUID()).toBe('01234567-89ab-4cde-8fab-0123456789ab')
    expect(native).toHaveBeenCalledOnce()
    expect(getRandomValues).not.toHaveBeenCalled()
  })

  it.each([
    [0x00, '00000000-0000-4000-8000-000000000000'],
    [0x55, '55555555-5555-4555-9555-555555555555'],
    [0xaa, 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa'],
    [0xff, 'ffffffff-ffff-4fff-bfff-ffffffffffff'],
  ])('sets the UUID version and variant while preserving random bytes: %i', (byte, expected) => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => bytes.fill(byte as number))
    vi.stubGlobal('crypto', { getRandomValues })

    const result = randomUUID()

    expect(result).toBe(expected)
    expect(result).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(getRandomValues).toHaveBeenCalledOnce()
    expect(getRandomValues.mock.calls[0]![0]).toHaveLength(16)
  })

  it.each([undefined, {}])('refuses to generate a UUID without secure randomness: %j', (crypto) => {
    vi.stubGlobal('crypto', crypto)

    expect(() => randomUUID()).toThrow('Secure random generation is unavailable')
  })

  it('propagates failure of the secure random source', () => {
    vi.stubGlobal('crypto', {
      getRandomValues() {
        throw new Error('random source failed')
      },
    })

    expect(() => randomUUID()).toThrow('random source failed')
  })
})
