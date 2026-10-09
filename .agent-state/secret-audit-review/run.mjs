// Offline actual Project Audit API/transport; controlled original cancel tails.
// Requires locked web/node_modules and Node. Optional AGENTEAM_REVIEW_SUBJECT
// and AGENTEAM_REVIEW_OUTPUT override only target checkout and ignored output.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync, spawnSync } from 'node:child_process'
const here = path.dirname(fileURLToPath(import.meta.url))
const subject = process.env.AGENTEAM_REVIEW_SUBJECT ?? path.resolve(here, '../..')
const output = process.env.AGENTEAM_REVIEW_OUTPUT ?? path.join(subject, 'output/ai/secret-variable-audit/independent-review')
fs.mkdirSync(output, { recursive: true })
const source = fs.readFileSync(subject + '/web/src/tests/secret-variable-audit.spec.ts', 'utf8')
const marker = "describe('Secret Variable Audit closed read compatibility'"
if (!source.includes(marker)) throw Error('frozen fixture marker unavailable')
const prefix = source.slice(0, source.indexOf(marker)).replaceAll("from '../", `from '${subject}/web/src/`)
const baselineClient = path.join(output, 'baseline-client.ts')
const baselineAPI = path.join(output, 'baseline-project-audit.ts')
for (const [input, destination] of [['client', baselineClient], ['project-audit', baselineAPI]]) {
  let original = execFileSync('git', ['show', `8cb0a953:web/src/api/${input}.ts`], { cwd: subject, encoding: 'utf8' })
  original = original.replaceAll("from './", `from '${subject}/web/src/api/`)
  if (input === 'project-audit') original = original.replace(`from '${subject}/web/src/api/client'`, `from '${baselineClient}'`)
  fs.writeFileSync(destination, original)
}
const spec = path.join(output, 'independent.spec.ts')
fs.writeFileSync(spec, `import { createProjectAuditAPI as baselineAPI } from '${baselineAPI}'\n` + prefix + fs.readFileSync(path.join(here, 'tail-controls.ts'), 'utf8'))
const config = path.join(output, 'vitest.config.mjs')
fs.writeFileSync(config, `export default ${JSON.stringify({
  root: subject + '/web',
  cacheDir: output + '/cache',
  resolve: { alias: { vitest: subject + '/web/node_modules/vitest/dist/index.js' } },
  test: { environment: 'jsdom', include: [spec], maxWorkers: 1, restoreMocks: true },
})}`)
const result = spawnSync(process.execPath, [subject + '/web/node_modules/vitest/vitest.mjs', 'run', '--config', config], { cwd: subject + '/web', stdio: 'inherit' })
if (result.error) throw result.error
process.exitCode = result.status ?? 1
