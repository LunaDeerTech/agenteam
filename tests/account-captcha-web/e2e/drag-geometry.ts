export function circularDistance(a: number, b: number) {
  const difference = Math.abs(a - b) % 360
  return Math.min(difference, 360 - difference)
}

// Chromium MouseEvent clientX and the official component's parseInt angle are
// discrete. Enumerate physical integer positions, never alter the solver or
// ask the server to accept a wider error. The result is only a pointer target.
export function reachableDrag(angle: number, travel: number) {
  if (!Number.isInteger(angle) || angle < 0 || angle >= 360 ||
      !Number.isInteger(travel) || travel < 1 || travel > 4096) {
    throw new Error('invalid public drag geometry')
  }
  let best = { pixel: 0, angle: 0, distance: Infinity }
  const factor = 360 / travel
  for (let pixel = 0; pixel <= travel; pixel++) {
    // The endpoint is handled explicitly by the pinned component.
    const submitted = pixel === travel ? 360 : Math.trunc(pixel * factor)
    const distance = circularDistance(angle, submitted)
    if (distance < best.distance) best = { pixel, angle: submitted, distance }
  }
  if (best.distance > 1) throw new Error('drag is too coarse for one-degree fidelity')
  return best
}
