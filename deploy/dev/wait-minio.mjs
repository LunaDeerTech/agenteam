import { setTimeout as delay } from 'node:timers/promises'

// Container "running" is not storage readiness. This is the same bounded
// development dependency wait as before; Central keeps its own startup budget.
const deadline = Date.now() + 60_000
while (Date.now() < deadline) {
  try {
    const response = await fetch('http://minio:9000/minio/health/live', {
      redirect: 'error',
      signal: AbortSignal.timeout(Math.max(1, Math.min(2_000, deadline - Date.now()))),
    })
    await response.body?.cancel()
    if (response.ok) process.exit(0)
  } catch {
    // Do not print endpoint or raw transport errors into development logs.
  }
  await delay(Math.max(1, Math.min(2_000, deadline - Date.now())))
}
console.error('Development MinIO did not become available within 60 seconds.')
process.exit(1)
