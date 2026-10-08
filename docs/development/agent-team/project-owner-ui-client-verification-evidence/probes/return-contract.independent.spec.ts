// Pending root handover: alias must resolve to a fixed candidate auth.ts.
import { describe, expect, it } from 'vitest'
import { safeReturnTarget } from '@frozen-owner/auth'
import { invalidReturnTargets, validReturnTargets } from './contract-vectors'

describe('independent protected return contract', () => {
  it.each(validReturnTargets)('accepts %s as %s', (input, expected) => {
    expect(safeReturnTarget(input)).toBe(expected)
  })
  it.each(invalidReturnTargets.map((input, index) => [index, input]))(
    'rejects invalid target %s',
    (_index, input) => {
      expect(safeReturnTarget(input)).toBe('/')
    },
  )
})
