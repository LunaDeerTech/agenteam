#!/bin/sh
set -eu
umask 077

# Only local directory/key preparation needs root. Compilation and Central run
# as the existing data owner (or local default), without retaining SIGTERM.
. /workspace/deploy/dev/credentials.sh
mkdir -p /dev-state/config
if [ -e /dev-state/config/keyrings.json ] || [ -e /dev-state/config/central.env ] || [ -e /dev-state/config/machine-id ]; then
  dev_require_credentials
fi
dev_load_credentials
node /workspace/deploy/dev/init.mjs
. /dev-state/config/runtime.env
exec su-exec "${DEV_RUNTIME_UID}:${DEV_RUNTIME_GID}" /bin/sh /workspace/deploy/dev/start.sh
