#!/bin/sh
# Shared internal development defaults; never source user-supplied shell data.
# Existing values always win. Only app bootstrap persists defaults; services
# can start independently with the same values before that first app startup.
dev_config=/dev-state/config
dev_credentials_error() {
  printf '%s\n' 'Invalid or missing persistent development credentials. Restore the config directory together with its original data.' >&2
  exit 1
}
dev_require_credentials() {
  for dev_name in database-password minio-user minio-password; do
    [ -f "$dev_config/$dev_name" ] && [ ! -L "$dev_config/$dev_name" ] && [ -s "$dev_config/$dev_name" ] || dev_credentials_error
  done
}
dev_credential() {
  dev_file="$dev_config/$1"
  if [ ! -e "$dev_file" ] && [ ! -L "$dev_file" ]; then printf '%s' "$2"; return; fi
  [ -f "$dev_file" ] && [ ! -L "$dev_file" ] && [ -s "$dev_file" ] || dev_credentials_error
  dev_size=$(wc -c < "$dev_file")
  [ "$dev_size" -le 512 ] || dev_credentials_error
  dev_value=$(cat "$dev_file")
  [ "${#dev_value}" -eq "$dev_size" ] && [ "${#dev_value}" -ge "$3" ] && [ "${#dev_value}" -le "$4" ] || dev_credentials_error
  case "$dev_value" in *[![:graph:]]*) dev_credentials_error ;; esac
  printf '%s' "$dev_value"
}
dev_load_credentials() {
  LC_ALL=C
  export LC_ALL
  [ -d "$dev_config" ] && [ ! -L "$dev_config" ] || dev_credentials_error
  # New local environments need no password settings. These services have no
  # host-published ports; these fixed values are only development defaults.
  DEV_DATABASE_PASSWORD=$(dev_credential database-password agenteam-local-development 1 512) || exit 1
  DEV_MINIO_ROOT_USER=$(dev_credential minio-user agenteam 3 128) || exit 1
  DEV_MINIO_ROOT_PASSWORD=$(dev_credential minio-password agenteam-local-development-minio 8 512) || exit 1
  export DEV_DATABASE_PASSWORD DEV_MINIO_ROOT_USER DEV_MINIO_ROOT_PASSWORD
}
