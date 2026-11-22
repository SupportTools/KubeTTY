#!/bin/sh
# Pinned, checksummed download for the KubeTTY image build (cluster-services-1496).
#
#   fetch.sh <out-file> <sha256> <upstream-url> [<fetch-cache path>]
#
# Tier (cluster-services CLAUDE.md build rule), printed on every call so the build log shows the
# source of each file:
#   - with a <fetch-cache path> and FETCH_MIRROR set: the org fetch-cache (a cache we run), e.g.
#     ${FETCH_MIRROR}/k8s/release/... If that fails it falls back to <upstream-url> unless
#     FETCH_FALLBACK=none (CI), which makes it a build failure instead.
#   - without one: "upstream-pinned", a direct fetch of <upstream-url>, bounded retries, sha256
#     pinned. Refused under FETCH_FALLBACK=none (CI), so every fetch in the image build must name a
#     fetch-cache path (cluster-services-1499).
# The sha256 is checked in every case; a mismatch always fails the build.
set -eu
out=$1 sha=$2 url=$3 path=${4:-}
name=$(basename "$out")
get() { curl --http1.1 -fsSL --retry 5 --retry-delay 2 --retry-all-errors --connect-timeout 30 -o "$out" "$1"; }
ok() { echo "${sha}  ${out}" | sha256sum -c --status; }
if [ -n "$path" ] && [ -n "${FETCH_MIRROR:-}" ]; then
  if get "${FETCH_MIRROR}/${path}" && ok; then
    echo "fetch: ${name} source=fetch-cache ${FETCH_MIRROR}/${path} sha256=${sha}"
    exit 0
  fi
  echo "fetch: ${name} MIRROR FAILED (${FETCH_MIRROR}/${path})" >&2
  rm -f "$out"
  if [ "${FETCH_FALLBACK:-upstream}" = "none" ]; then
    echo "FETCH_FALLBACK=none: refusing upstream for ${name}" >&2
    exit 1
  fi
  tier=upstream-fallback
else
  if [ "${FETCH_FALLBACK:-upstream}" = "none" ]; then
    # CI: every fetch must name a fetch-cache path (cluster-services-1499), so a new download
    # added without one fails the build instead of quietly going upstream.
    echo "FETCH_FALLBACK=none: ${name} has no fetch-cache path (or FETCH_MIRROR is empty); refusing upstream" >&2
    exit 1
  fi
  tier=upstream-pinned
fi
get "$url"
if ! ok; then
  echo "fetch: ${name} SHA256 MISMATCH from ${url}: got $(sha256sum "$out" | cut -d' ' -f1), want ${sha}" >&2
  rm -f "$out"
  exit 1
fi
echo "fetch: ${name} source=${tier} ${url} sha256=${sha}"
