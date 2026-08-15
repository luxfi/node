#!/usr/bin/env bash
# Source is published. It states what the code does and what must hold — never what
# went wrong, where, or on which host.
#
# A comment naming a pod, a node id, a cluster, an incident or a measured outage turns
# every reader of the public repository into a reader of our operations. It also ages
# badly: the invariant stays true, the anecdote does not.
#
# Rewrite rather than delete. "Measured on host X, N requests produced M results" becomes
# the rule that made it wrong — "a batch that accepts nothing must descend, because a
# responder serves the window ending at the id it was asked for".
#
# Runs over tracked Go source. Exits non-zero on a match.

set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

fail=0
report() { # pattern, what it leaks
  local hits
  hits=$(git grep -nE "$1" -- '*.go' 2>/dev/null) || return 0
  [ -z "$hits" ] && return 0
  printf '\n%s\n' "$2"
  printf '%s\n' "$hits" | sed 's/^/  /'
  fail=1
}

report 'luxd-[0-9]|zood-[a-z0-9-]*[0-9]|parsd-[a-z0-9-]*[0-9]' \
  'pod names — these are our hosts'
report 'NodeID-[A-Za-z0-9]{8}' \
  'validator node ids'
report '\b(lux|zoo|pars|hanzo)-(mainnet|testnet|devnet)\b' \
  'cluster namespaces'
report '[Ii]ncident-[0-9]|INCIDENT' \
  'incident references — the ticket is not the reason the rule holds'
report '[Mm]easured (on|at) (lux|zoo|pars|hanzo|host|prod)' \
  'outage measurements taken from production'
report '[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' \
  'IP addresses'
report '\b(ghcr\.io/[a-z]+/[a-z-]+:v?[0-9]+\.[0-9]+\.[0-9]+)' \
  'pinned image references — these belong in manifests, not source'

# A mutation left in the tree is not a style problem. One found in this repo replaced
# the height in the signed vote message with a constant — signatures valid at any
# height — and it sat uncommitted where any `git add -u` would have swept it into a
# public consensus repo. Whoever injects one is responsible for restoring it; this
# refuses to let the tree be committed while one is still there.
report '(//|/\*).*MUTAT(ION|ED) *:' \
  'mutation-test artifacts — restore the original before committing'

if [ "$fail" -ne 0 ]; then
  cat <<'EOF'

Source states invariants, not history. Rewrite each comment above to say what must be
true and why, with no host, cluster, id, ticket or outage in it.
EOF
  exit 1
fi
echo "source prose clean"
