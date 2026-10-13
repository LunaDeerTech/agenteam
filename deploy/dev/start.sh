#!/bin/sh
set -eu
umask 077

# A running nginx must not serve the previous build if this restart fails.
rm -f /dev-state/frontend/.ready
rm -f /dev-state/bin/agenteam /dev-state/bin/agenteam.next
test "$(go env GOVERSION)" = go1.27.1
test "$(node --version)" = v24.12.0
. /dev-state/config/central.env

# Never execute a previous build after a failed compilation.
cd /workspace
go build -buildvcs=false -o /dev-state/bin/agenteam.next ./cmd/agenteam

# One writable frontend workspace, with the actual source/public directories
# mounted read-only. Copy only the small top-level configuration files so Node
# resolves this workspace's single node_modules and never writes in the source.
find /dev-state/web -maxdepth 1 -type f ! -name .installed-lock -delete
find /workspace/web -maxdepth 1 -type f -exec cp '{}' /dev-state/web/ ';'
cd /dev-state/web
lock=$({ node --version; npm --version; sha256sum package.json package-lock.json; } | sha256sum | cut -d ' ' -f 1)
if [ ! -f node_modules/.package-lock.json ] || [ ! -f .installed-lock ] || [ "$(cat .installed-lock)" != "$lock" ]; then
  npm ci --no-audit --no-fund
  printf '%s\n' "$lock" > .installed-lock
fi
rm -rf /dev-state/frontend/next
npm run build -- --outDir /dev-state/frontend/next --emptyOutDir
# nginx's worker must be able to read these public assets despite the private
# umask used for configuration, compiler outputs and Account recovery material.
find /dev-state/frontend/next -type d -exec chmod 755 '{}' ';'
find /dev-state/frontend/next -type f -exec chmod 644 '{}' ';'
rm -rf /dev-state/frontend/previous
if [ -d /dev-state/frontend/current ]; then
  mv /dev-state/frontend/current /dev-state/frontend/previous
fi
mv /dev-state/frontend/next /dev-state/frontend/current
rm -rf /dev-state/frontend/previous
mv /dev-state/bin/agenteam.next /dev-state/bin/agenteam

cd /workspace
/dev-state/bin/agenteam --check-config
touch /dev-state/frontend/.ready
chmod 644 /dev-state/frontend/.ready
# Central receives SIGTERM directly, runs its normal migrations/initialization,
# and owns the unchanged Stop/Drain/Guard/database shutdown path.
exec /dev-state/bin/agenteam

