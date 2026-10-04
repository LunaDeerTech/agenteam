import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { solvePublicRotation } from './public-solver.ts'
import { reachableDrag } from './drag-geometry.ts'

// The corpus is produced by TestPublicRotationKnownGeometry. Each known input
// is test-owned and unrelated to any issued account challenge or credentials.
const vectors = JSON.parse(await readFile(process.argv[2], 'utf8'))
assert.ok(vectors.length > 0)
for (const v of vectors) {
  const image = p => ({ ...p, rgba: Buffer.from(p.rgba, 'base64') })
  const result = solvePublicRotation({ master: image(v.master), thumb: image(v.thumb) })
  assert.equal(result.angle, v.go, `${v.name}: Go/JS mismatch`)
  assert.ok(Math.abs(result.angle - v.want) <= 5, `${v.name}: known oracle mismatch`)
}
const empty = { width: 160, height: 160, rgba: new Uint8Array(160 * 160 * 4) }
assert.throws(() => solvePublicRotation({ master: { width: 220, height: 220, rgba: new Uint8Array(220 * 220 * 4) }, thumb: empty }), /opaque public pixels/)
console.log(`public solver parity: ${vectors.length} known-input cases passed`)
for (let angle = 0; angle < 360; angle++) {
  assert.ok(reachableDrag(angle, 158).distance <= 1)
}
assert.deepEqual(reachableDrag(97, 158), { pixel: 43, angle: 97, distance: 0 })
assert.throws(() => reachableDrag(97, 10), /too coarse/)
console.log('physical integer drag: all 360 angles fit one-degree bound at 158px travel')
