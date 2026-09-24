#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'release runtime config test failed: %s\n' "$1" >&2
  exit 1
}

expected_version='0.2.4'
runtime_version=$(tr -d '\r\n' < backend/cmd/server/VERSION)
[ "$runtime_version" = "$expected_version" ] || \
  fail "runtime VERSION is '$runtime_version', expected '$expected_version'"

check_root_dockerfile() {
  # Normalize complete Dockerfile instructions, not comments or physical lines.
  # Validate cache mounts on the RUN that consumes them, in the correct stage.
  awk '
    function has_cache(instruction, id, target, fields, options, pair, n, m, i, j, type, mount_id, mount_target) {
      n = split(instruction, fields, /[[:space:]]+/)
      for (i = 1; i <= n; i++) {
        if (fields[i] !~ /^--mount=/) continue
        sub(/^--mount=/, "", fields[i])
        m = split(fields[i], options, ",")
        type = mount_id = mount_target = ""
        for (j = 1; j <= m; j++) {
          split(options[j], pair, "=")
          if (pair[1] == "type") type = pair[2]
          if (pair[1] == "id") mount_id = pair[2]
          if (pair[1] == "target") mount_target = pair[2]
        }
        if (type == "cache" && mount_id == id && mount_target == target) return 1
      }
      return 0
    }
    function inspect(instruction, fields, n, op) {
      n = split(instruction, fields, /[[:space:]]+/)
      op = toupper(fields[1])
      if (op == "ARG" && fields[2] ~ /^BUILDKIT_SYNTAX(=|$)/) external_frontend = 1
      if (op == "FROM") {
        stage = toupper(fields[n - 1]) == "AS" ? fields[n] : "runtime"
        if (stage == "frontend-builder") {
          frontend_stages++
          frontend_native = index(instruction, "--platform=${BUILDPLATFORM}") > 0
        }
        if (stage == "backend-builder") {
          backend_stages++
          backend_native = index(instruction, "--platform=${BUILDPLATFORM}") > 0
        }
        return
      }
      if (stage == "frontend-builder" && op == "RUN" && instruction ~ /pnpm install([[:space:]]|$)/) {
        pnpm_cached = has_cache(instruction, "sub2api-pnpm-store", "/root/.local/share/pnpm/store")
      }
      if (stage == "backend-builder") {
        if (op == "RUN" && instruction ~ /go mod download([[:space:]]|$)/) {
          download_cached = has_cache(instruction, "sub2api-gomod", "/go/pkg/mod")
        }
        if (op == "RUN" && instruction ~ /go build([[:space:]]|$)/) {
          build_cached = has_cache(instruction, "sub2api-gomod", "/go/pkg/mod") &&
            has_cache(instruction, "sub2api-gobuild", "/root/.cache/go-build")
          build_embeds = instruction ~ /-tags[=[:space:]]+embed([[:space:]]|$)/
        }
        if (op == "COPY" && instruction ~ /--from=frontend-builder([[:space:]]|$)/ &&
            fields[n - 1] == "/app/backend/internal/web/dist" && fields[n] == "./internal/web/dist") embeds_frontend = 1
      }
      if (stage == "runtime" && op == "COPY" && instruction ~ /--from=backend-builder([[:space:]]|$)/) {
        if (fields[n - 1] == "/app/sub2api" && fields[n] == "/app/sub2api") runtime_binary = 1
        if (fields[n - 1] == "/app/backend/resources" && fields[n] == "/app/resources") runtime_resources = 1
      }
    }
    {
      sub(/\r$/, "")
      if (tolower($0) ~ /^[[:space:]]*#[[:space:]]*syntax[[:space:]]*=/) external_frontend = 1
      if ($0 ~ /^[[:space:]]*(#|$)/) next
      line = $0
      sub(/^[[:space:]]+/, "", line)
      continued = sub(/\\[[:space:]]*$/, "", line)
      instruction = instruction (instruction == "" ? "" : " ") line
      if (!continued) {
        sub(/[[:space:]]+$/, "", instruction)
        inspect(instruction)
        instruction = ""
      }
    }
    END {
      if (external_frontend || instruction != "" || frontend_stages != 1 || backend_stages != 1 ||
          !frontend_native || !backend_native || !pnpm_cached || !download_cached ||
          !build_cached || !build_embeds || !embeds_frontend || stage != "runtime" ||
          !runtime_binary || !runtime_resources) exit 1
    }
  '
}

check_root_dockerfile < Dockerfile || \
  fail 'root Dockerfile must use bundled BuildKit with stage-scoped cache mounts and embedded runtime artifacts'

# Prove that the contract rejects the regressions it protects against. These
# mutations stream through the validator; no working-tree file is changed.
if { printf '# syntax=docker/dockerfile:1.7\n'; cat Dockerfile; } | check_root_dockerfile; then
  fail 'Dockerfile validator accepted an external frontend directive'
fi
if { printf 'ARG BUILDKIT_SYNTAX=docker/dockerfile:1.7\n'; cat Dockerfile; } | check_root_dockerfile; then
  fail 'Dockerfile validator accepted an external frontend build argument'
fi
for mutation in \
  's/id=sub2api-pnpm-store/id=wrong-store/g' \
  's/target=\/go\/pkg\/mod/target=\/wrong-go-mod/g' \
  's/type=cache,id=sub2api-gobuild/type=bind,id=sub2api-gobuild/g' \
  's/--platform=${BUILDPLATFORM}/--platform=${TARGETPLATFORM}/g' \
  's/-tags embed/-tags noembed/g' \
  's/--from=frontend-builder/--from=wrong-builder/g' \
  's/--from=backend-builder/--from=wrong-builder/g'
do
  if sed "$mutation" Dockerfile | check_root_dockerfile; then
    fail "Dockerfile validator accepted a broken build contract: $mutation"
  fi
done

grep -Fqx '      dockerfile: Dockerfile' deploy/docker-compose.dev.yml || \
  fail 'development Compose must build from the root Dockerfile'
grep -Fq '    -f "${REPO_ROOT}/Dockerfile" \' deploy/build_image.sh || \
  fail 'local image builder must use the root Dockerfile'

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml
do
  images=$(
    COMPOSE_DISABLE_ENV_FILE=1 \
    POSTGRES_PASSWORD=release-test-password \
    DATABASE_HOST=database.example.test \
    DATABASE_PASSWORD=release-test-password \
    REDIS_HOST=redis.example.test \
    docker compose -f "$compose_file" config --images
  )
  image_count=$(printf '%s\n' "$images" | grep -Fxc "sub2api-custom:v${expected_version}" || true)
  [ "$image_count" -eq 1 ] || \
    fail "$compose_file resolves application image $image_count times instead of once"
done

release_override_images=$(
  COMPOSE_DISABLE_ENV_FILE=1 \
  POSTGRES_PASSWORD=release-test-password \
  docker compose \
    -f deploy/docker-compose.yml \
    -f deploy/docker-compose.release.override.yml \
    config --images
)
release_override_count=$(printf '%s\n' "$release_override_images" | grep -Fxc "sub2api-custom:v${expected_version}" || true)
[ "$release_override_count" -eq 1 ] || \
  fail "release override resolves application image $release_override_count times instead of once"

workflow=.github/workflows/release.yml
grep -Fq '  deployment-gate:' "$workflow" || \
  fail 'release workflow is missing the deployment-gate job'
grep -Fq 'needs: [update-version, build-frontend, deployment-gate]' "$workflow" || \
  fail 'release job does not wait for deployment-gate'
grep -Fq 'go test -p 1 -parallel 1 ./... -count=1' "$workflow" || \
  fail 'deployment-gate is missing the backend Go test command'
grep -Fq 'go vet -p 1 ./...' "$workflow" || \
  fail 'deployment-gate is missing the backend Go vet command'
grep -Fq 'pnpm run typecheck' "$workflow" || \
  fail 'deployment-gate is missing frontend typecheck'
grep -Fq 'pnpm run test:run' "$workflow" || \
  fail 'deployment-gate is missing the full frontend Vitest suite'
grep -Fq 'pnpm run build' "$workflow" || \
  fail 'deployment-gate is missing the frontend build'
grep -Fq 'for test_file in deploy/tests/*-test.sh' "$workflow" || \
  fail 'deployment-gate is missing deployment test execution'
grep -Fq 'bash deploy/test-caddyfile-cache.sh' "$workflow" || \
  fail 'deployment-gate is missing the Caddyfile deployment test'
grep -Fq 'git diff --check' "$workflow" || \
  fail 'deployment-gate is missing the whitespace check'
if grep -Fq -- '--skip=validate' "$workflow"; then
  fail 'release workflow still skips GoReleaser validation'
fi

printf 'release runtime configuration test passed\n'
