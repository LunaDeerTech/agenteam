#!/bin/bash
# Run only inside the nonce-owned bookworm/PG17.8 candidate container.
set -euo pipefail
test "$(postgres --version)" = 'postgres (PostgreSQL) 17.8 (Debian 17.8-1.pgdg12+1)'
mkdir -p /tmp/retrieval-build
tar -xzf /deps/groonga-16.1.3.tar.gz -C /tmp/retrieval-build
tar -xzf /deps/pgroonga-4.1.0.tar.gz -C /tmp/retrieval-build
dpkg-deb -x /deps/pg17dev.deb /
disabled=(BFLOAT16 ZLIB ZSTD SIMDJSON RAPIDJSON MECAB KYTEA LIBSTEMMER
  ZEROMQ LIBEVENT OPENZL XXHASH LIBEDIT LZ4 ROARING_BITMAPS BLOSC XSIMD
  H3 LLAMA_CPP USEARCH FAISS CURL BASE64 JSONCONS)
flags=()
for feature in "${disabled[@]}"; do flags+=("-DGRN_WITH_${feature}=no"); done
cmake -S /tmp/retrieval-build/groonga-16.1.3 -B /tmp/retrieval-build/groonga-build \
  -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr/local \
  -DGRN_WITH_NFKC=ON -DGRN_WITH_MESSAGE_PACK=system \
  -DGRN_WITH_APACHE_ARROW=OFF -DGRN_WITH_MRUBY=OFF \
  -DGRN_WITH_SIMSIMD=OFF -DGRN_WITH_BUNDLED_ONIGMO=ON \
  -DGRN_WITH_DOC=OFF -DGRN_WITH_BENCHMARKS=OFF -DGRN_WITH_EXAMPLES=OFF \
  -DGRN_WITH_TOOLS=OFF -DFETCHCONTENT_FULLY_DISCONNECTED=ON "${flags[@]}"
cmake --build /tmp/retrieval-build/groonga-build --parallel 2
cmake --install /tmp/retrieval-build/groonga-build
ldconfig
meson setup /tmp/retrieval-build/pgroonga-build /tmp/retrieval-build/pgroonga-4.1.0 \
  --buildtype=release --wrap-mode=nodownload -Dmessage_pack=enabled \
  -Dxxhash=enabled -Dtest=false -Dpg_config=/usr/lib/postgresql/17/bin/pg_config
meson compile -C /tmp/retrieval-build/pgroonga-build -j 2
meson install -C /tmp/retrieval-build/pgroonga-build
groonga --version
ldd /usr/lib/postgresql/17/lib/pgroonga.so
