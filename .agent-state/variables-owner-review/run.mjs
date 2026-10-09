// Independent offline review. Generates only an ignored test from the frozen
// author's fixture setup; all Session/Workspace/Variables/API consumers are real.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
const here = path.dirname(fileURLToPath(import.meta.url))
const owner = path.resolve(here, '../..')
const subject = '/workspace/agenteam-project-variables-ui'
const output = path.join(owner, 'output/ai/work-owner-planning-ui/implementation/variables-owner-review')
fs.mkdirSync(output, { recursive: true })
const original = fs.readFileSync(subject + '/web/src/tests/project-variables-state.spec.ts', 'utf8')
const marker = "describe('Variables page consumes current Project owner and six actual API methods'"
if (original.indexOf(marker) < 0) throw Error('fixture marker unavailable')
const prefix = original.slice(0, original.indexOf(marker)).replaceAll("from '../", `from '${subject}/web/src/`)
const spec = path.join(output, 'independent.spec.ts')
fs.writeFileSync(spec, prefix + fs.readFileSync(path.join(here, 'tail-controls.ts'), 'utf8'))
const config = path.join(output, 'vitest.config.mjs')
fs.writeFileSync(config, `export default ${JSON.stringify({
  root: subject + '/web',
  resolve: { alias: {
    vitest: subject + '/web/node_modules/vitest/dist/index.js',
    '@vue/test-utils': subject + '/web/node_modules/@vue/test-utils/dist/vue-test-utils.esm-bundler.mjs',
  } },
  test: { environment: 'jsdom', include: [spec], restoreMocks: true },
})}`)
const run = spawnSync(process.execPath, [subject + '/web/node_modules/vitest/vitest.mjs', 'run', '--config', config], { cwd: subject + '/web', stdio: 'inherit' })
if (run.error) throw run.error
process.exitCode = run.status ?? 1
