#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
resolver="$repo_root/deploy/scripts/resolve-release-tag.sh"

fail() {
  printf 'release tag input test failed: %s\n' "$1" >&2
  exit 1
}

[ -f "$resolver" ] || fail 'release tag resolver is missing'

expected=$(printf 'v0.2.4-custom\n0.2.4')
actual=$(sh "$resolver" 'v0.2.4-custom' '')
[ "$actual" = "$expected" ] || fail "custom tag resolved unexpectedly: $actual"

expected=$(printf 'v1.2.3-rc.1+build.7\n1.2.3-rc.1+build.7')
actual=$(sh "$resolver" '' 'refs/tags/v1.2.3-rc.1+build.7')
[ "$actual" = "$expected" ] || fail "tag ref resolved unexpectedly: $actual"

if sh "$resolver" '' 'refs/heads/main' >/dev/null 2>&1; then
  fail 'non-tag ref was accepted as a release'
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM
output_file="$tmpdir/github-output"
sh "$resolver" 'v0.2.4-custom' '' "$output_file"
expected=$(printf 'tag=v0.2.4-custom\nruntime_version=0.2.4')
[ "$(cat "$output_file")" = "$expected" ] || fail 'GitHub output did not preserve tag and runtime version'

for invalid in 'v01.2.3' 'v1.2.3-01' 'v1.2.3-rc..1' 'v1.2.3-rc.' 'v1.2.3+build..1' 'v1.2.3+'; do
  if sh "$resolver" "$invalid" '' "$output_file" >/dev/null 2>&1; then
    fail "invalid semantic version was accepted: $invalid"
  fi
  [ "$(cat "$output_file")" = "$expected" ] || fail 'invalid tag modified GitHub output'
done

for valid in 'v0.0.0' 'v1.2.3-0' 'v1.2.3-01alpha' 'v1.2.3+001'; do
  sh "$resolver" "$valid" '' >/dev/null || fail "valid semantic version rejected: $valid"
done

malicious="v0.2.4\$(touch \"$tmpdir/pwned\")"
if sh "$resolver" "$malicious" '' >/dev/null 2>&1; then
  fail 'shell syntax in a manual tag was accepted'
fi
[ ! -e "$tmpdir/pwned" ] || fail 'manual tag input was evaluated as shell code'

printf 'release tag input test passed\n'
