export interface PublicPixels {
  width: number
  height: number
  rgba: ArrayLike<number>
}

// Test-only public pixels, no private answer or server state. Keep this function
// self-contained so the identical algorithm runs in Node and page.evaluate.
export function solvePublicRotation(input: { master: PublicPixels; thumb: PublicPixels }) {
  const { master: m, thumb: t } = input
  if (m.width !== 220 || m.height !== 220 || t.width < 140 || t.width > 170 || t.height !== t.width ||
      m.rgba.length !== m.width * m.height * 4 || t.rgba.length !== t.width * t.height * 4) {
    throw new Error('invalid public puzzle dimensions')
  }
  const points: { x: number; y: number; rgb: number[] }[] = []
  const sum = [0, 0, 0], squares = [0, 0, 0]
  const center = t.width / 2 - 1, radius = t.width / 2 - 4
  for (let y = 4; y < t.height - 4; y += 4) {
    for (let x = 4; x < t.width - 4; x += 4) {
      if ((x - center) ** 2 + (y - center) ** 2 > radius ** 2) continue
      const i = (y * t.width + x) * 4
      if (t.rgba[i + 3] < 250) continue
      const rgb = [t.rgba[i], t.rgba[i + 1], t.rgba[i + 2]]
      for (let k = 0; k < 3; k++) {
        sum[k] += rgb[k]
        squares[k] += rgb[k] * rgb[k]
      }
      points.push({ x, y, rgb })
    }
  }
  if (points.length < 128) throw new Error('insufficient opaque public pixels')
  let texture = 0
  for (let k = 0; k < 3; k++) texture += squares[k] / points.length - (sum[k] / points.length) ** 2
  if (texture < 1) throw new Error('public puzzle has no directional texture')

  const span = (values: number[]) => {
    let width = Math.max(...values) - Math.min(...values) + 1
    if (width - Math.floor(width) > 0.1) width++
    return Math.trunc(width)
  }
  let angle = 0, score = Infinity
  for (let rotation = 0; rotation < 360; rotation++) {
    const a = rotation * Math.PI / 180, co = Math.cos(a), si = Math.sin(a), d = t.width - 1
    const w = span([0, d * co, d * co - d * si, -d * si])
    const h = span([0, d * si, d * si + d * co, d * co])
    const hx = w / 2 - Math.floor((w - t.width) / 2)
    const hy = h / 2 - Math.floor((h - t.height) / 2)
    const crop = Math.floor((m.width - t.width) / 2)
    let errorSum = 0, count = 0
    for (const p of points) {
      let sx = p.x + crop, sy = p.y + crop
      if (rotation !== 0) {
        const dx = p.x + 1.5 - hx, dy = p.y + 1.5 - hy
        sx = co * dx + si * dy + hx - 0.5 + crop
        sy = -si * dx + co * dy + hy - 0.5 + crop
      }
      const x = Math.floor(sx), y = Math.floor(sy)
      if (x < 0 || y < 0 || x + 1 >= m.width || y + 1 >= m.height) continue
      const fx = sx - x, fy = sy - y, i = (y * m.width + x) * 4
      if (m.rgba[i + 3] < 250 || m.rgba[i + 7] < 250 ||
          m.rgba[i + 4 * m.width + 3] < 250 || m.rgba[i + 4 * m.width + 7] < 250) continue
      for (let k = 0; k < 3; k++) {
        const upper = m.rgba[i + k] * (1 - fx) + m.rgba[i + 4 + k] * fx
        const lower = m.rgba[i + 4 * m.width + k] * (1 - fx) + m.rgba[i + 4 * m.width + 4 + k] * fx
        const delta = upper * (1 - fy) + lower * fy - p.rgb[k]
        errorSum += delta * delta
      }
      count++
    }
    if (count === points.length && errorSum / count < score) {
      angle = (360 - rotation) % 360
      score = errorSum / count
    }
  }
  if (!Number.isFinite(score)) throw new Error('public geometry could not be matched')
  return { angle, score, texture }
}
