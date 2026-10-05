#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -eq 0 ]; then
  set -- -run '^Test(Account(Profile|Avatar|CurrentUserRoute)|ObjectAvatar|AvatarCleanup)'
fi
exec sh scripts/test-objects.sh "$@"
