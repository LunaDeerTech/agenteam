import { AccountFailure, shape, string } from './client'

export type OutboundRule = Readonly<{
  cidr: string
  ports: 'all' | readonly number[]
  allow_http: boolean
}>

function requireValue(value: boolean) {
  if (!value) throw new AccountFailure('invalid-response')
}

// This is only the accepted rule grammar, not a target/network classifier.
function cidr(value: unknown): string {
  const text = string(value, 1, 43)
  const parts = text.split('/')
  requireValue(parts.length === 2 && /^(0|[1-9][0-9]{0,2})$/.test(parts[1]!))
  const address = parts[0]!,
    bits = Number(parts[1])
  let numeric = 0n
  let width: number
  if (address.includes(':')) {
    width = 128
    requireValue(/^[0-9a-f:]+$/.test(address) && bits <= width)
    const halves = address.split('::')
    requireValue(halves.length <= 2)
    const parse = (part: string): number[] => {
      if (!part) return []
      return part.split(':').map((v) => {
        requireValue(/^[0-9a-f]{1,4}$/.test(v))
        return parseInt(v, 16)
      })
    }
    const left = parse(halves[0]!),
      right = halves.length === 2 ? parse(halves[1]!) : []
    const omitted = 8 - left.length - right.length
    requireValue(halves.length === 2 ? omitted >= 1 : omitted === 0)
    const groups = [
      ...left,
      ...(halves.length === 2 ? Array<number>(omitted).fill(0) : []),
      ...right,
    ]
    let bestStart = -1,
      bestLength = 1
    for (let i = 0; i < groups.length; i++) {
      if (groups[i] !== 0) continue
      const start = i
      while (i < groups.length && groups[i] === 0) i++
      if (i - start > bestLength) {
        bestStart = start
        bestLength = i - start
      }
    }
    const hex = groups.map((v) => v.toString(16))
    const canonical =
      bestStart < 0
        ? hex.join(':')
        : hex.slice(0, bestStart).join(':') + '::' + hex.slice(bestStart + bestLength).join(':')
    requireValue(canonical === address)
    for (const group of groups) numeric = (numeric << 16n) | BigInt(group)
    requireValue(bits >= 7 && numeric >> 121n === 0x7en)
  } else {
    width = 32
    const octets = address.split('.')
    requireValue(octets.length === 4 && bits <= width)
    for (const octet of octets) {
      requireValue(/^(0|[1-9][0-9]{0,2})$/.test(octet) && Number(octet) <= 255)
      numeric = (numeric << 8n) | BigInt(octet)
    }
    requireValue(
      (bits >= 8 && numeric >> 24n === 10n) ||
        (bits >= 12 && numeric >> 20n === 0xac1n) ||
        (bits >= 16 && numeric >> 16n === 0xc0a8n),
    )
  }
  const hostBits = BigInt(width - bits)
  requireValue((numeric >> hostBits) << hostBits === numeric)
  return text
}

function ports(value: unknown, canonical: boolean): OutboundRule['ports'] {
  if (value === 'all') return value
  requireValue(Array.isArray(value) && value.length >= 1 && value.length <= 256)
  const result: number[] = []
  for (const port of value as unknown[]) {
    requireValue(
      typeof port === 'number' && Number.isSafeInteger(port) && port >= 1 && port <= 65535,
    )
    result.push(port as number)
  }
  const ordered = result.slice().sort((a, b) => a - b)
  requireValue(ordered.every((v, i) => !i || v !== ordered[i - 1]))
  if (canonical) requireValue(result.every((v, i) => v === ordered[i]))
  return Object.freeze(ordered)
}

function rules(value: unknown, canonical: boolean): readonly OutboundRule[] {
  requireValue(Array.isArray(value) && value.length <= 256)
  const result: OutboundRule[] = []
  for (const candidate of value as unknown[]) {
    const v = shape(candidate, ['cidr', 'ports', 'allow_http'])
    requireValue(typeof v.allow_http === 'boolean')
    result.push(
      Object.freeze({
        cidr: cidr(v.cidr),
        ports: ports(v.ports, canonical),
        allow_http: v.allow_http as boolean,
      }),
    )
  }
  // All accepted rule JSON is ASCII. Code-unit ordering equals Go bytes.Compare.
  const encoded = result.map((value) => ({ value, wire: JSON.stringify(value) }))
  if (canonical) requireValue(encoded.every((v, i) => !i || encoded[i - 1]!.wire < v.wire))
  encoded.sort((a, b) => (a.wire < b.wire ? -1 : a.wire > b.wire ? 1 : 0))
  requireValue(encoded.every((v, i) => !i || encoded[i - 1]!.wire !== v.wire))
  return Object.freeze(encoded.map((v) => v.value))
}

export function parseOutboundRules(value: unknown): readonly OutboundRule[] {
  return rules(value, true)
}

export function captureOutboundRules(value: unknown): readonly OutboundRule[] {
  try {
    return rules(value, false)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}

export function captureOutboundPortText(value: string): readonly number[] {
  try {
    requireValue(typeof value === 'string')
    const parsed = value.split(',').map((part) => {
      const token = part.replace(/^[ \t\r\n\v\f]+|[ \t\r\n\v\f]+$/g, '')
      requireValue(/^[1-9][0-9]{0,4}$/.test(token))
      return Number(token)
    })
    return ports(parsed, false) as readonly number[]
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
