#!/bin/sh
set -eu

requested_tag=${1-}
git_ref=${2-}
output_file=${3-}

if [ -n "$requested_tag" ]; then
  tag=$requested_tag
else
  case "$git_ref" in
    refs/tags/*) tag=${git_ref#refs/tags/} ;;
    *) printf 'release ref must be a tag\n' >&2; exit 1 ;;
  esac
fi

case "$tag" in
  ''|*[!A-Za-z0-9.+-]*)
    printf 'release tag contains unsupported characters\n' >&2
    exit 1
    ;;
esac

number='(0|[1-9][0-9]*)'
prerelease='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
build='[0-9A-Za-z-]+'
if ! printf '%s\n' "$tag" | LC_ALL=C grep -Eq "^v${number}\.${number}\.${number}(-${prerelease}(\.${prerelease})*)?(\+${build}(\.${build})*)?$"; then
  printf 'release tag must use a v-prefixed semantic version\n' >&2
  exit 1
fi

runtime_version=${tag#v}
case "$runtime_version" in
  *-custom) runtime_version=${runtime_version%-custom} ;;
esac

if [ -n "$output_file" ]; then
  {
    printf 'tag=%s\n' "$tag"
    printf 'runtime_version=%s\n' "$runtime_version"
  } >> "$output_file"
else
  printf '%s\n%s\n' "$tag" "$runtime_version"
fi
