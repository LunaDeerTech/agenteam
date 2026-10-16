import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
const css = readFileSync(resolve('src/styles/tokens.css'), 'utf8')
const blocks = css.match(/\{[^}]+\}/g)!
function variables(block: string) {
  return Object.fromEntries(
    [...block.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!.trim()]),
  )
}
const light = variables(blocks[0]!)
const dark = { ...light, ...variables(blocks[1]!) }
function luminance(hex: string) {
  const channels = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
  return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722
}
function contrast(a: string, b: string) {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (values[0]! + 0.05) / (values[1]! + 0.05)
}
it('keeps every documented theme token in sync with the stylesheet', () => {
  const docs = readFileSync(resolve('../docs/frontend-design/styles/colors-and-themes.md'), 'utf8')
  const entries = [...docs.matchAll(/\| `(--[\w-]+)` \| `([^`]+)` \| `([^`]+)` \|/g)]
  expect(entries.length).toBeGreaterThanOrEqual(30)
  for (const entry of entries) {
    expect(dark[entry[1]!] === 'var(--accent)' ? dark['--accent'] : dark[entry[1]!], entry[1]).toBe(
      entry[2],
    )
    expect(
      light[entry[1]!] === 'var(--accent)' ? light['--accent'] : light[entry[1]!],
      entry[1],
    ).toBe(entry[3])
  }
})
for (const [theme, tokens] of Object.entries({ light, dark })) {
  describe(`${theme} contrast`, () => {
    it('meets text and essential boundary contrast across approved surfaces', () => {
      const surfaces = ['--bg', '--canvas', '--surface', '--surface-alt', '--card', '--accent-soft']
      for (const surface of surfaces) {
        for (const text of ['--text', '--muted'])
          expect(
            contrast(tokens[text]!, tokens[surface]!),
            `${text}/${surface}`,
          ).toBeGreaterThanOrEqual(4.5)
        for (const graphic of surface === '--accent-soft'
          ? ['--accent']
          : ['--control-border', '--accent'])
          expect(
            contrast(tokens[graphic]!, tokens[surface]!),
            `${graphic}/${surface}`,
          ).toBeGreaterThanOrEqual(3)
      }
      expect(contrast(tokens['--action-text']!, tokens['--action-fill']!)).toBeGreaterThanOrEqual(
        4.5,
      )
      for (const semantic of ['success', 'warning', 'danger'])
        expect(
          contrast(tokens[`--${semantic}`]!, tokens[`--${semantic}-bg`]!),
        ).toBeGreaterThanOrEqual(4.5)
      for (const graphic of [
        '--task-progress',
        '--task-review',
        '--agent-1',
        '--agent-2',
        '--agent-3',
      ]) {
        for (const surface of ['--surface', '--surface-alt', '--card'])
          expect(contrast(tokens[graphic]!, tokens[surface]!)).toBeGreaterThanOrEqual(3)
      }
    })
  })
}
it('shared components do not depend on Debug fixtures or views', () => {
  const directory = resolve('src/components/ui')
  for (const file of readdirSync(directory).filter((f) => f.endsWith('.vue') || f.endsWith('.ts')))
    expect(readFileSync(`${directory}/${file}`, 'utf8')).not.toMatch(
      /from\s+['"][^'"]*(?:views\/debug|fixtures)/,
    )
})
