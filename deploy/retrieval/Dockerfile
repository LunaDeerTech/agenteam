# syntax=docker/dockerfile:1.7
FROM pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc

# The named build context must contain the exact upstream release artifact.
# Proxy configuration retains Docker's ordinary build behavior. An optional
# build secret supplies the deployment's trusted CA bundle only to APT.
# APT reads HTTPS roots as _apt; the public CA bundle is not a private key.
# Bind inputs read-only so the release archive does not remain in an image
# layer and vfs builders need only one filesystem snapshot for installation.
RUN --mount=type=bind,from=pg_search_package,source=postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb,target=/tmp/pg-search.deb \
    --mount=type=bind,source=install-pg-search.sh,target=/inputs/install-pg-search.sh \
    --mount=type=bind,source=licenses,target=/inputs/licenses \
    --mount=type=secret,id=apt_ca,required=false,mode=0444 \
    set -eu; \
    mkdir -p /recipe; \
    cp -p /inputs/install-pg-search.sh /recipe/install-pg-search.sh; \
    cp -R /inputs/licenses /recipe/licenses; \
    sed -i 's|http://|https://|g' /etc/apt/sources.list.d/pgdg.list /etc/apt/sources.list.d/debian.sources; \
    if [ -s /run/secrets/apt_ca ]; then \
      set -- -o Acquire::https::CaInfo=/run/secrets/apt_ca; \
    else \
      set --; \
    fi; \
    apt-get "$@" -o APT::Update::Error-Mode=any update; \
    apt-get "$@" install -y --no-install-recommends --no-upgrade \
      libopenblas0=0.3.21+ds-4 libopenblas0-pthread=0.3.21+ds-4; \
    sh /recipe/install-pg-search.sh /tmp/pg-search.deb; \
    rm -rf /var/lib/apt/lists/*

# Extension/index creation remains an explicit same-database bootstrap step.
