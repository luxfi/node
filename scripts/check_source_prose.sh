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
report() { # pattern, what it leaks, [exempt pattern]
  local hits
  hits=$(git grep -nE "$1" -- '*.go' 2>/dev/null) || return 0
  [ -n "${3:-}" ] && hits=$(printf '%s\n' "$hits" | grep -vE "$3")
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
# Loopback, the unspecified address and the RFC1918 ranges are not "an IP address"
# in the sense that matters — they name no host of ours. What matters is a routable
# address that points at real infrastructure.
# matters: `net.Listen("tcp", "127.0.0.1:0")` is how a test asks the OS for a free
# port, and it names no host. Flagging them buried the real hits — a public address
# in an example — under dozens of listeners, and a check that mostly cries wolf gets
# ignored, which is the same end as a check that cannot fire.
report '[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' \
  'IP addresses' \
  '127\.0\.0\.1|0\.0\.0\.0|255\.255\.255\.255|1\.2\.3\.4|::1|10\.[0-9]|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.|8\.8\.8\.8'
report '\b(ghcr\.io/[a-z]+/[a-z-]+:v?[0-9]+\.[0-9]+\.[0-9]+)' \
  'pinned image references — these belong in manifests, not source'

# A private key in source is public the moment it is pushed, and no amount of
# "it is only a scratch file" changes that. Two were found here: a fourteen-line
# converter run once, and a build-ignored debug helper nothing called. Both had
# sat in a public repo. Key material comes from KMS or a fixture generated at
# test time, never from a literal.
report '(key|secret|priv)[A-Za-z]*[[:space:]]*(:=|=)[[:space:]]*"[0-9a-fA-F]{64}"' \
  'private key material in source — it is public once pushed; rotate and source it from KMS'

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
