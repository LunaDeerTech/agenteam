#!/bin/sh
set -eu
umask 077

# MinIO can still be initializing when its container is running. Bound this
# local readiness wait and never print credentials or raw connection errors.
attempt=0
until mc alias set local http://minio:9000 "$(cat /run/dev-config/minio-user)" "$(cat /run/dev-config/minio-password)" --api S3v4 >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    printf '%s\n' 'Development MinIO did not become available.' >&2
    exit 1
  fi
  sleep 2
done
# mb alone creates a private, unversioned bucket. Do not mutate an existing
# bucket's policy/lifecycle/versioning; the real app checks those invariants.
mc mb --ignore-existing local/agenteam >/dev/null
printf '%s\n' 'Development object bucket is ready.'

