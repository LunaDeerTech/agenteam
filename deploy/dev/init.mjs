import { randomBytes } from 'node:crypto'
import { chownSync, chmodSync, closeSync, existsSync, linkSync, lstatSync, mkdirSync, openSync, readFileSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'

// Local app bootstrap only; database/MinIO initialize their own data and
// Central initializes its bucket. Never print passwords or key material.
const root = '/dev-state'
// Keep the owner of existing private keys/spool. For a fresh tree use its
// checkout owner, with UID/GID 1000 when the checkout is owned by root.
const ownerPath = [`${root}/config/keyrings.json`, `${root}/config/central.env`, `${root}/object-spool`, '/workspace']
  .find(path => existsSync(path) && lstatSync(path).uid > 0)
const owner = ownerPath ? lstatSync(ownerPath) : null
if (owner?.isSymbolicLink()) throw new Error('Development owner source must not be a symbolic link')
const uid = owner?.uid || 1000
const gid = owner?.gid || 1000
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
directory(`${root}/frontend`, 0o755)
directory(`${root}/nginx-cache`, 0o755)
try { unlinkSync(`${root}/frontend/.ready`) } catch (error) { if (error.code !== 'ENOENT') throw error }

function input(name, minimum, maximum) {
  const value = process.env[name] ?? ''
  if (value.length < minimum || value.length > maximum || /[^\x21-\x7e]/.test(value)) {
    throw new Error(`Invalid ${name}; use nonempty printable ASCII without spaces`)
  }
  return value
}
const password = input('DEV_DATABASE_PASSWORD', 1, 512)
const minioUser = input('DEV_MINIO_ROOT_USER', 3, 128)
const minioPassword = input('DEV_MINIO_ROOT_PASSWORD', 8, 512)
const connection = {
  AGENTEAM_CENTRAL_DATABASE_URL: `postgresql://agenteam:${encodeURIComponent(password)}@database:5432/agenteam`,
  AGENTEAM_CENTRAL_OBJECT_ACCESS_KEY: minioUser,
  AGENTEAM_CENTRAL_OBJECT_SECRET_KEY: minioPassword,
}
const keyNames = [
  'AGENTEAM_CENTRAL_CURSOR_KEYRING',
  'AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING',
  'AGENTEAM_CENTRAL_ACCOUNT_KEYRING',
  'AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING',
  'AGENTEAM_CENTRAL_SECRET_KEYRING',
]
function readPrivate(name) {
  const path = `${root}/config/${name}`
  const info = lstatSync(path)
  if (!info.isFile() || info.isSymbolicLink() || info.size === 0 || info.size > 128 * 1024) {
    throw new Error('development configuration requires bounded nonempty regular files')
  }
  chmodSync(path, 0o600)
  chownSync(path, uid, gid)
  return readFileSync(path, 'utf8')
}
function persistent(name, generate) {
  const path = `${root}/config/${name}`
  if (!existsSync(path)) {
    const temporary = `${root}/config/persistent-${randomBytes(12).toString('hex')}.tmp`
    try {
      const fd = openSync(temporary, 'wx', 0o600)
      try { writeFileSync(fd, generate()) } finally { closeSync(fd) }
      chownSync(temporary, uid, gid)
      // Publish a complete private file; never replace an existing value.
      try { linkSync(temporary, path) } catch (error) { if (error.code !== 'EEXIST') throw error }
    } finally {
      try { unlinkSync(temporary) } catch (error) { if (error.code !== 'ENOENT') throw error }
    }
  }
  return readPrivate(name)
}
function mismatch() {
  throw new Error('Development credentials differ from existing data. Restore the original config directory with its database and object data; credentials are not reset or rotated during startup.')
}
// Validate all old credentials before importing keyrings or creating new files.
const credentials = { 'database-password': password, 'minio-user': minioUser, 'minio-password': minioPassword }
for (const [name, value] of Object.entries(credentials)) {
  if (existsSync(`${root}/config/${name}`) && readPrivate(name) !== value) mismatch()
}
let legacy
if (existsSync(`${root}/config/central.env`)) {
  legacy = {}
  for (const line of readPrivate('central.env').trimEnd().split('\n')) {
    const match = /^export (AGENTEAM_CENTRAL_[A-Z_]+)='([^'\r\n]*)'$/.exec(line)
    if (!match || Object.hasOwn(legacy, match[1])) throw new Error('Invalid legacy development configuration')
    legacy[match[1]] = match[2]
  }
  const names = [...Object.keys(connection), ...keyNames]
  if (Object.keys(legacy).length !== names.length || names.some(name => !Object.hasOwn(legacy, name))) {
    throw new Error('Incomplete legacy development configuration')
  }
  for (const [name, value] of Object.entries(connection)) if (legacy[name] !== value) mismatch()
}
function newKeyrings() {
  const ring = kid => JSON.stringify({ format: 1, current_kid: kid, keys: [{ kid, key_b64: randomBytes(32).toString('base64') }] })
  return {
    AGENTEAM_CENTRAL_CURSOR_KEYRING: ring('cursor1'),
    AGENTEAM_CENTRAL_OBJECT_DOWNLOAD_KEYRING: ring('download1'),
    AGENTEAM_CENTRAL_ACCOUNT_KEYRING: ring('account1'),
    AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING: ring('knowledge1'),
    AGENTEAM_CENTRAL_SECRET_KEYRING: JSON.stringify({ format: 1, current_version: '1', keys: [{ version: '1', key_b64: randomBytes(32).toString('base64') }] }),
  }
}
function parseKeyringJSON(raw) {
  try { return JSON.parse(raw) } catch { throw new Error('Invalid persistent development keyring JSON') }
}
const candidate = existsSync(`${root}/config/keyrings.json`)
  ? parseKeyringJSON(readPrivate('keyrings.json'))
  : legacy ? Object.fromEntries(keyNames.map(name => [name, legacy[name]])) : newKeyrings()
if (!candidate || Object.keys(candidate).length !== keyNames.length || keyNames.some(name => typeof candidate[name] !== 'string')) {
  throw new Error('Invalid development keyrings')
}
const material = new Set()
for (const name of keyNames) {
  const ring = parseKeyringJSON(candidate[name])
  const versioned = name === 'AGENTEAM_CENTRAL_SECRET_KEYRING'
  const id = versioned ? 'version' : 'kid'
  const current = versioned ? 'current_version' : 'current_kid'
  if (ring?.format !== 1 || typeof ring[current] !== 'string' || !Array.isArray(ring.keys) || ring.keys.length === 0 || !ring.keys.some(key => key && key[id] === ring[current])) {
    throw new Error('Invalid persistent development keyring')
  }
  const ids = new Set()
  for (const key of ring.keys) {
    if (!key || typeof key[id] !== 'string' || ids.has(key[id]) || typeof key.key_b64 !== 'string' || Buffer.from(key.key_b64, 'base64').length !== 32 || Buffer.from(key.key_b64, 'base64').toString('base64') !== key.key_b64 || material.has(key.key_b64)) {
      throw new Error('Development keyrings require independent canonical 32-byte keys')
    }
    ids.add(key[id]); material.add(key.key_b64)
  }
  if (legacy && legacy[name] !== candidate[name]) throw new Error('Legacy and current development keyrings differ')
}
for (const [name, value] of Object.entries(credentials)) persistent(name, () => value)
persistent('keyrings.json', () => JSON.stringify(candidate) + '\n')
const machine = persistent('machine-id', () => `${randomBytes(16).toString('hex')}\n`)
if (!/^[0-9a-f]{32}\n$/.test(machine) || /^0{32}\n$/.test(machine)) throw new Error('Invalid persistent machine identity')

// Only the generated runtime environment changes. Quote every value as POSIX
// shell data; original keys, credentials and the legacy file remain untouched.
const quote = value => "'" + value.replaceAll("'", "'\\''") + "'"
const runtime = Object.entries({ DEV_RUNTIME_UID: String(uid), DEV_RUNTIME_GID: String(gid), ...connection, ...candidate }).map(([name, value]) => `export ${name}=${quote(value)}\n`).join('')
const target = `${root}/config/runtime.env`
if (existsSync(target)) readPrivate('runtime.env')
const temporary = `${root}/config/runtime-${randomBytes(12).toString('hex')}.tmp`
try {
  const fd = openSync(temporary, 'wx', 0o600)
  try { writeFileSync(fd, runtime) } finally { closeSync(fd) }
  chownSync(temporary, uid, gid)
  renameSync(temporary, target)
} finally {
  try { unlinkSync(temporary) } catch (error) { if (error.code !== 'ENOENT') throw error }
}
console.log('Development app directories and persistent keys are ready.')
