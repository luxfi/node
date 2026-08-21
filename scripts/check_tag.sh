#!/usr/bin/env bash
# Build a tag the way a consumer gets it, and check it carries what it claims.
#
# Three releases went out this way: one without its untracked proposervm files,
# one whose dependency shipped without an untracked source file, and one whose
# Dockerfile still named an outdated plugin release. Every time
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

# The EVM is an external VM plugin, not a package linked into luxd. Its one
# authoritative pin is the Dockerfile ARG; the incidental transitive version in
# node's module graph is unrelated and may be older. Prove the pinned release is
# published rather than comparing it to that unrelated module selection.
declared=$(awk -F= '/^ARG EVM_VERSION=/{print $2}' Dockerfile)
resolved=$(GOWORK=off go mod download -json "github.com/luxfi/evm@$declared" | awk -F'"' '/"Version"/{print $4; exit}')
if [ "$declared" != "$resolved" ]; then
	echo "FAIL: Dockerfile pins evm $declared, but that module release does not resolve" >&2
	exit 1
fi
echo "evm plugin: $resolved"

for req in "$@"; do
	mod=${req%%=*}
	want=${req#*=}
	if [ "$mod" = luxfi/evm ]; then
		got=$declared
	else
		got=$(GOWORK=off go list -m -f '{{.Version}}' "github.com/$mod")
	fi
	lowest=$(printf '%s\n%s\n' "$want" "$got" | sort -V | head -1)
	if [ "$lowest" != "$want" ]; then
		echo "FAIL: $mod resolves $got, below the $want this tag claims" >&2
		exit 1
	fi
	echo "$mod: $got"
done

echo "$tag carries what it claims"
