#!/bin/sh
# Static release contracts only: this test does not invoke Docker or a database.
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'release artifact reproducibility test failed: %s\n' "$1" >&2
  exit 1
}

resolver=deploy/scripts/resolve-release-tag.sh
[ -f "$resolver" ] || fail 'release workflow resolver is missing'
if git check-ignore --no-index -q "$resolver"; then
  fail 'release workflow resolver is ignored and cannot be added normally'
fi
git check-ignore --no-index -q deploy/scripts/private-release-helper.sh || \
  fail 'release resolver exception exposes unrelated deployment scripts'

runtime_version=$(tr -d '\r\n' < backend/cmd/server/VERSION)
default_version=$(sed -n 's/^ARG VERSION=//p' Dockerfile | head -n 1 | tr -d '\r')
[ "$default_version" = "$runtime_version" ] || fail 'Dockerfile default version differs from the runtime version'

# Inspect each stage separately so labels/ARGs on a builder cannot satisfy the runtime contract.
runtime=$(awk '/^FROM / { stage = ""; next } { stage = stage $0 "\n" } END { printf "%s", stage }' Dockerfile | tr -d '\r')
printf '%s\n' "$runtime" | grep -Fxq 'ARG VERSION' || fail 'runtime stage does not inherit VERSION'
printf '%s\n' "$runtime" | grep -Fxq 'ARG COMMIT' || fail 'runtime stage does not inherit COMMIT'
printf '%s\n' "$runtime" | grep -Fxq 'LABEL org.opencontainers.image.version="${VERSION}"' || fail 'runtime OCI version label is missing'
printf '%s\n' "$runtime" | grep -Fxq 'LABEL org.opencontainers.image.revision="${COMMIT}"' || fail 'runtime OCI revision label is missing'
builder=$(awk '/^FROM .* AS backend-builder/ { found=1; next } /^FROM / { found=0 } found { print }' Dockerfile | tr -d '\r')
printf '%s\n' "$builder" | grep -Fxq 'ARG VERSION' || fail 'binary and runtime must inherit the same VERSION argument'
printf '%s\n' "$builder" | grep -Fxq 'ARG COMMIT' || fail 'binary and runtime must inherit the same COMMIT argument'

printf 'release artifact reproducibility test passed (no Docker invoked)\n'
