#!/bin/sh
set -eu
umask 077

# Only local directory/key preparation needs root. Compilation and Central run
# as the configured non-root user, with no extra wrapper retaining SIGTERM.
node /workspace/deploy/dev/init.mjs
exec su-exec "${DEV_UID}:${DEV_GID}" /bin/sh /workspace/deploy/dev/start.sh
