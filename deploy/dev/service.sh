#!/bin/sh
set -eu
umask 077
. /workspace/deploy/dev/credentials.sh

dev_service=$1
shift
case "$dev_service" in
  database)
    # Never replace credentials belonging to an initialized database.
    if [ -e "${PGDATA:-/var/lib/postgresql/data/pgdata}/PG_VERSION" ]; then
      dev_require_credentials
    fi
    dev_load_credentials
    POSTGRES_PASSWORD=$DEV_DATABASE_PASSWORD
    export POSTGRES_PASSWORD
    ;;
  minio)
    if [ -d /data/.minio.sys ]; then dev_require_credentials; fi
    dev_load_credentials
    MINIO_ROOT_USER=$DEV_MINIO_ROOT_USER
    MINIO_ROOT_PASSWORD=$DEV_MINIO_ROOT_PASSWORD
    export MINIO_ROOT_USER MINIO_ROOT_PASSWORD
    ;;
  *) dev_credentials_error ;;
esac
unset DEV_DATABASE_PASSWORD DEV_MINIO_ROOT_USER DEV_MINIO_ROOT_PASSWORD
# Compose supplies the real image entrypoint and its arguments. This adapter
# only loads the original credentials; it does not initialize or alter SQL.
exec "$@"
