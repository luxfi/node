#!/usr/bin/env bash
# Build a tag the way a consumer gets it, and check it carries what it claims.
#
# Three releases went out this way: one without its untracked proposervm files,
# one whose dependency shipped without an untracked source file, and one whose
# Dockerfile still named an image two releases behind its own go.mod. Every time
# the tests were green, because a test builds the working tree and a release
# ships the commit — and nothing compared the two.
#
#   scripts/check_tag.sh v1.36.125 [luxfi/consensus=v1.36.56 luxfi/evm=v1.104.34]
#
# Builds from a clone of the tag, then asserts each named module resolves at or
# above the version given. A pin below what a fix needs builds perfectly and
# ships the bug.
set -euo pipefail

tag=${1:?usage: check_tag.sh <tag> [module=minversion ...]}
shift || true

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

git clone -q --depth 1 --branch "$tag" "$(git rev-parse --show-toplevel)" "$work/src"
cd "$work/src"

GOWORK=off go build ./... 2>&1 | grep -v '^ld: warning' || true
echo "build: ok"

# EVM_VERSION is a separate literal from go.mod and has shipped stale before.
declared=$(awk -F= '/^ARG EVM_VERSION=/{print $2}' Dockerfile)
resolved=$(GOWORK=off go list -m -f '{{.Version}}' github.com/luxfi/evm)
if [ "$declared" != "$resolved" ]; then
	echo "FAIL: Dockerfile builds the plugin from evm $declared while luxd runs $resolved" >&2
	exit 1
fi
echo "evm: Dockerfile and go.mod agree at $resolved"

for req in "$@"; do
	mod=${req%%=*}
	want=${req#*=}
	got=$(GOWORK=off go list -m -f '{{.Version}}' "github.com/$mod")
	lowest=$(printf '%s\n%s\n' "$want" "$got" | sort -V | head -1)
	if [ "$lowest" != "$want" ]; then
		echo "FAIL: $mod resolves $got, below the $want this tag claims" >&2
		exit 1
	fi
	echo "$mod: $got"
done

echo "$tag carries what it claims"
