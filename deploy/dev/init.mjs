import { randomBytes } from 'node:crypto'
import { chownSync, chmodSync, closeSync, existsSync, lstatSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs'

// One-time local deployment material. Never print keys or recovery contents.
const uid = Number(process.env.DEV_UID)
const gid = Number(process.env.DEV_GID)
if (!Number.isSafeInteger(uid) || uid < 1 || !Number.isSafeInteger(gid) || gid < 1) {
  throw new Error('AGENTEAM_DEV_UID/GID must be positive numeric IDs')
}
const root = '/dev-state'
function directory(path, mode = 0o700) {
  mkdirSync(path, { recursive: true, mode })
  if (!lstatSync(path).isDirectory() || lstatSync(path).isSymbolicLink()) {
    throw new Error('development state requires real directories')
  }
  chmodSync(path, mode)
  chownSync(path, uid, gid)
}
directory(root)
for (const part of ['config', 'bin', 'home', 'go', 'go-build', 'tmp', 'npm-cache', 'cache', 'node-compile-cache', 'web', 'object-spool', 'account-recovery']) {
  directory(`${root}/${part}`)
}
// nginx serves only this public build tree; keys/data have separate mounts.
directory(`${root}/frontend`, 0o755)
// nginx's root master creates the worker-owned request/proxy temp directories.
directory(`${root}/nginx-cache`, 0o755)
// Database and MinIO entrypoints own their data directories themselves.
for (const part of ['postgres', 'minio', 'mc']) {
  mkdirSync(`${root}/${part}`, { recursive: true, mode: 0o700 })
  if (!lstatSync(`${root}/${part}`).isDirectory() || lstatSync(`${root}/${part}`).isSymbolicLink()) {
    throw new Error('development service state requires real directories')
  }
  if (part !== 'postgres') {
    chownSync(`${root}/${part}`, uid, gid)
  }
}
function secret(name, generate) {
  const path = `${root}/config/${name}`
  if (!existsSync(path)) {
    const fd = openSync(path, 'wx', 0o600)
    try { writeFileSync(fd, generate()) } finally { closeSync(fd) }
  }
  const info = lstatSync(path)
  if (!info.isFile() || info.isSymbolicLink() || info.size === 0) {
    throw new Error('development configuration requires nonempty regular files')
  }
  chmodSync(path, 0o600)
  chownSync(path, uid, gid)
  return readFileSync(path, 'utf8')
}
const password = secret('database-password', () => randomBytes(24).toString('hex'))
const minioUser = secret('minio-user', () => `dev${randomBytes(10).toString('hex')}`)
const minioPassword = secret('minio-password', () => randomBytes(32).toString('hex'))
const machine = secret('machine-id', () => `${randomBytes(16).toString('hex')}\n`)
// Fixed shape values prevent a modified credential file becoming shell code.
if (!/^[0-9a-f]{48}$/.test(password) || !/^dev[0-9a-f]{20}$/.test(minioUser) || !/^[0-9a-f]{64}$/.test(minioPassword) || !/^[0-9a-f]{32}\n$/.test(machine) || /^0{32}\n$/.test(machine)) {
  throw new Error('unexpected development credential format')
}
secret('central.env', () => {
  const ring = kid => JSON.stringify({ format: 1, current_kid: kid, keys: [{ kid, key_b64: randomBytes(32).toString('base64') }] })
  const values = {
    AGENTEAM_CENTRAL_DATABASE_URL: `postgresql://agenteam:${password}@database:5432/agenteam`,
    AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY: minioUser,
    AGENTEAM_CENTRAL_OBJECT_SECRET_KEY: minioPassword,
    AGENTEAM_CENTRAL_CURSOR_KEYRING: ring('cursor1'),
    AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING: ring('download1'),
    AGENTEAM_CENTRAL_ACCOUNT_KEYRING: ring('account1'),
    AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING: ring('knowledge1'),
    AGENTEAM_CENTRAL_SECRET_KEYRING: JSON.stringify({ format: 1, current_version: '1', keys: [{ version: '1', key_b64: randomBytes(32).toString('base64') }] }),
  }
  return Object.entries(values).map(([key, value]) => `export ${key}='${value}'\n`).join('')
})
console.log('Development directories and persistent credentials are ready.')

