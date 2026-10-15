#!/bin/sh
# Install the exact, unmodified upstream binary in an owned image/fixture.
# This does not connect to PostgreSQL, change preloads, or bootstrap indexes.
# Prerequisites: the frozen PG17.8/vector0.8.1 bookworm amd64 base and
# libopenblas0 + libopenblas0-pthread 0.3.21+ds-4. No package upgrade/download.
set -eu
test "$#" -eq 1 || { echo 'usage: install-pg-search.sh /absolute/package.deb' >&2; exit 2; }
package=$1
case "$package" in /*) ;; *) echo 'absolute package path required' >&2; exit 2;; esac
test "$(id -u)" = 0
test "$(dpkg --print-architecture)" = amd64
test "$(dpkg-query -W -f='${Version}' postgresql-17)" = '17.8-1.pgdg12+1'
test "$(dpkg-query -W -f='${Version}' libopenblas0)" = '0.3.21+ds-4'
test "$(dpkg-query -W -f='${Version}' libopenblas0-pthread)" = '0.3.21+ds-4'
test "$(sed -n "s/^default_version = '\([^']*\)'$/\1/p" /usr/share/postgresql/17/extension/vector.control)" = '0.8.1'
printf '%s  %s\n' '79ea1fb115e3e136f6086b82d8a91e2cf86d2ba21f58a58477d4f372d73384ce' "$package" | sha256sum -c -
test "$(dpkg-deb -f "$package" Package)" = postgresql-17-pg-search
test "$(dpkg-deb -f "$package" Version)" = 0.26.1
test "$(dpkg-deb -f "$package" Architecture)" = amd64

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
for entry in \
 'fd9e861ae7025866b6abc08af807e7617b509570f817bccf70a52c6fb7a8de1b lindera-cc-cedict-notice.txt' \
 '2cf235bf0842d6d61eb0244fb22e645a321b12408a3cf75feb3a53577d885bde lindera-ipadic-notice.txt' \
 '56b37f2a5680b77ce2ebaec68a7931652d20e69b4b2cfabc74dc06bfdc6a927a lindera-ko-dic-notice.txt' \
 '3a668a70e1d025a79e455e5b9a2268d5d262b108dff14ec70c77f19f929bc9b2 openblas.txt'
do
    digest=${entry%% *}
    name=${entry#* }
    printf '%s  %s\n' "$digest" "$script_dir/licenses/$name" | sha256sum -c -
done

dpkg -i "$package"
test "$(dpkg-query -W -f='${Version}' postgresql-17-pg-search)" = 0.26.1
# The base's dpkg path-exclude removes documentation. Restore the license and
# copyright from the exact verified package, never a guessed license URL.
license_dir=/usr/share/licenses/pg_search
install -d "$license_dir"
dpkg-deb --fsys-tarfile "$package" | tar -xOf - ./usr/share/doc/postgresql-17-pg-search/LICENSE > "$license_dir/LICENSE"
dpkg-deb --fsys-tarfile "$package" | tar -xOf - ./usr/share/doc/postgresql-17-pg-search/copyright > "$license_dir/copyright"
for name in lindera-cc-cedict-notice.txt lindera-ipadic-notice.txt lindera-ko-dic-notice.txt openblas.txt
do
    install -m 0644 "$script_dir/licenses/$name" "$license_dir/$name"
done
printf '%s\n' 'Installed unmodified pg_search 0.26.1. Explicit preload, bootstrap and readiness are still required.'
