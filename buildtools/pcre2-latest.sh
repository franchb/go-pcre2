#!/usr/bin/env bash
# Prints "TAG COMMIT" for the newest PCRE2 release (pcre2-10.NN, no RCs),
# read from the upstream repository's tags.
set -euo pipefail

repo=https://github.com/PCRE2Project/pcre2
tag="$(git ls-remote --tags --refs "$repo" 'pcre2-*' \
  | sed -n 's#.*refs/tags/\(pcre2-[0-9][0-9]*\.[0-9][0-9]*\)$#\1#p' \
  | sort -V | tail -1)"
test -n "$tag"

# Annotated tags list the commit as TAG^{}; lightweight tags list it directly.
commit="$(git ls-remote "$repo" "refs/tags/$tag^{}" | cut -f1)"
if [ -z "$commit" ]; then
  commit="$(git ls-remote "$repo" "refs/tags/$tag" | cut -f1)"
fi
test -n "$commit"

echo "$tag $commit"
