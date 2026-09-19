#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

gateway_variables=$(mktemp "${TMPDIR:-/tmp}/sub2api-gateway-env.XXXXXX")
cleanup() {
  rm -f "$gateway_variables"
}
trap cleanup EXIT HUP INT TERM

awk '
  /^(GATEWAY_[A-Z0-9_]+|SUB2API_IMAGES_MAIN_MODEL)=/ {
    separator = index($0, "=")
    print substr($0, 1, separator - 1)
  }
' deploy/.env.example > "$gateway_variables"
printf 'TIMEZONE\n' >> "$gateway_variables"

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  while IFS= read -r key; do
    # Empty environment values are ignored by the config loader. Defaults
    # belong there, below persisted YAML, not in Compose's environment layer.
    # Explicit values from either .env or the shell must still pass through.
    expected=$(printf '      - %s=${%s:-}' "$key" "$key")
    expected_count=$(grep -Fxc "$expected" "$compose_file" || true)
    key_count=$(grep -Ec "^[[:space:]]*-[[:space:]]*${key}([[:space:]]*=.*)?[[:space:]]*$" "$compose_file" || true)
    if [ "$expected_count" -ne 1 ] || [ "$key_count" -ne 1 ]; then
      printf '%s must pass %s without overriding persisted YAML by default, exactly once\n' "$compose_file" "$key" >&2
      exit 1
    fi
  done < "$gateway_variables"
done

printf 'docker compose Gateway environment test passed\n'
