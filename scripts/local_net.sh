#!/usr/bin/env bash
# Run a local luxd network of N nodes on one machine.
#
# Every node keeps its own data directory and generates its own staking
# identity on first boot, so nothing has to be minted in advance. What the
# nodes share is what is identical between them: one genesis, one plugin
# directory, one chain config. Copying those per node costs 66MB of plugin
# each and buys nothing.
#
# Disk is the resource that runs out first, and logs are what spend it —
# the defaults (8MB x 7 files, four log families per node) allow 22GB
# across a hundred nodes, several times what the chain data itself uses.
# So the log rotation is capped here rather than left at its default, and
# the free space is checked before each batch.
#
# A node can only take part in consensus if genesis names it, and its id is
# derived from a staking key it does not have until it first boots. So the
# first run mints identities and then rebuilds genesis around them; later runs
# find the identities on disk and start straight away.
#
#   NODES=100 scripts/local_net.sh start
#   scripts/local_net.sh status
#   scripts/local_net.sh stop
set -uo pipefail

NODES="${NODES:-5}"
BASE="${BASE:-$HOME/.lux/local-net}"
LUXD="${LUXD:-$HOME/work/lux/node/build/luxd}"
EVM_PLUGIN="${EVM_PLUGIN:-$HOME/work/lux/evm/build/evm}"
GENESIS="${GENESIS:-$HOME/work/lux/genesis/live/mainnet/genesis.json}"
NETWORK_ID="${NETWORK_ID:-96369}"
PORT_BASE="${PORT_BASE:-30000}"
BATCH="${BATCH:-10}"
# How many of the fleet everyone dials at startup. These are validators, so
# peer gossip carries the rest; dialling the whole fleet from every node
# costs far more memory and produces a worse mesh.
SEEDS="${SEEDS:-5}"
# A poll cannot ask more validators than exist, and a quorum larger than the
# sample can never be met — the node then polls forever and finalises nothing.
K="${K:-$([ "$NODES" -lt 20 ] && echo "$NODES" || echo 20)}"
ALPHA="${ALPHA:-$(( (K * 7 + 9) / 10 ))}"
# Refuse to start another batch below this much free space.
MIN_FREE_GB="${MIN_FREE_GB:-40}"
# Memory is a disk constraint too: squeezed hard enough, macOS grows the
# swapfile rather than refusing, and that lands on the same volume.
MIN_AVAIL_GB="${MIN_AVAIL_GB:-6}"
GOMEMLIMIT="${GOMEMLIMIT:-160MiB}"
GOGC="${GOGC:-50}"
# The EVM's own VM id. Without the binary under this exact name luxd logs
# "chain VM plugin not loaded" at info and serves P and X while reporting
# healthy — a node with no C-Chain that looks fine.
EVM_VMID=mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6

http_port() { echo $((PORT_BASE + $1 * 2)); }
stake_port() { echo $((PORT_BASE + $1 * 2 + 1)); }
node_dir()  { printf '%s/node-%03d' "$BASE" "$1"; }

free_gb() { df -g "$BASE" | awk 'NR==2{print $4}'; }

# What the machine could still hand out without evicting anything it is using.
avail_gb() {
  vm_stat | awk -F: '/Pages free|Pages inactive|Pages speculative|Pages purgeable/ {
    gsub(/[ .]/, "", $2); p += $2
  } END { printf "%d", p * 16384 / 1073741824 }'
}

swap_used() { sysctl -n vm.swapusage | sed -n 's/.*used = \([0-9.]*M\).*/swap \1/p'; }

# The info service is REST under /v1/info/ops, not a JSON-RPC endpoint.
info() { # info <port> <path>
  curl -s -m 20 "http://127.0.0.1:$1/v1/info/ops$2"
}

start_node() { # start_node <index> <bootstrap-ips> <bootstrap-ids>
  local i="$1" boot_ip="${2:-}" boot_id="${3:-}" dir
  dir="$(node_dir "$i")"
  mkdir -p "$dir"
  local args=(
    --data-dir="$dir"
    --genesis-file="$BASE/genesis.json"
    --network-id="$NETWORK_ID"
    --plugin-dir="$BASE/plugins"
    --chain-config-dir="$BASE/chains"
    --http-port="$(http_port "$i")"
    --staking-port="$(stake_port "$i")"
    --http-host=127.0.0.1
    --log-level=warn
    --log-display-level=off
    --log-rotater-max-size=1
    --log-rotater-max-files=1
    --consensus-sample-size="$K"
    --consensus-quorum-size="$ALPHA"
  )
  # Sybil protection stays off across the fleet. Left on, a node creates the
  # P-Chain and stops there — it never reaches the other eight, so it serves
  # no C-Chain and casts no vote, and the first block never becomes final.
  args+=(--sybil-protection-enabled=false)
  if [ -n "$boot_ip" ]; then
    args+=(--bootstrap-ips="$boot_ip" --bootstrap-ids="$boot_id")
  else
    args+=(--skip-bootstrap=true)
  fi
  # A hundred Go runtimes on one machine each keep a heap sized for a machine
  # they own. A soft memory limit makes the collector run against the budget
  # the node actually has here; the plugin inherits it through the env.
  GOMEMLIMIT="$GOMEMLIMIT" GOGC="$GOGC" \
    "$LUXD" "${args[@]}" > "$dir/stdout.log" 2>&1 &
  echo $! > "$dir/pid"
}

wait_http() { # wait_http <port> <seconds>
  local p="$1" limit="${2:-60}" n=0
  while [ "$n" -lt "$limit" ]; do
    info "$p" /node/id | grep -q nodeID && return 0
    sleep 1; n=$((n + 1))
  done
  return 1
}

# Lay down what every node reads and none of them writes.
stage() {
  for f in "$LUXD" "$EVM_PLUGIN" "$GENESIS"; do
    [ -e "$f" ] || { echo "missing: $f"; exit 2; }
  done
  mkdir -p "$BASE/plugins" "$BASE/chains/C"
  cp "$EVM_PLUGIN" "$BASE/plugins/$EVM_VMID"; chmod +x "$BASE/plugins/$EVM_VMID"
  # A validator keeps the working set, not the archive: nothing below
  # quantum finality can re-org, so history it will never be asked about is
  # disk spent for nothing. The caches are sized for a hundred of these
  # sharing one machine, not for one node owning it.
  cat > "$BASE/chains/C/config.json" <<'JSON'
{"pruning-enabled":true,"state-history":32,
 "trie-clean-cache":32,"trie-dirty-cache":32,"snapshot-cache":16,
 "accepted-cache-size":8,"skip-tx-indexing":true,
 "tx-pool-global-slots":512,"tx-pool-global-queue":128,"tx-pool-account-slots":16,
 "metrics-expensive-enabled":false,
 "eth-apis":["eth","net","web3","internal-eth","internal-blockchain","internal-transaction"]}
JSON
}

# Stamp the local network id into a copy of the source genesis. The mainnet
# genesis declares 1; left alone, these nodes would answer to — and dial —
# the real network.
base_genesis() {
  python3 -c 'import json,sys; g=json.load(open(sys.argv[1])); g["networkID"]=int(sys.argv[3]); json.dump(g,open(sys.argv[2],"w"))' \
    "$GENESIS" "$1" "$NETWORK_ID"
}

# First pass: every node without a staking key boots once, alone, purely to
# mint one. Its id and proof of possession are what genesis needs to name it.
mint_identities() {
  local missing=0 i=0
  while [ "$i" -lt "$NODES" ]; do
    [ -f "$(node_dir "$i")/staking/staker.crt" ] || missing=1
    i=$((i + 1))
  done
  [ "$missing" -eq 0 ] && [ -f "$BASE/identities.json" ] && return 0

  echo "minting $NODES staking identities"
  base_genesis "$BASE/genesis.json"
  i=0
  while [ "$i" -lt "$NODES" ]; do start_node "$i"; i=$((i + 1)); done

  # Wait on the collectors by pid. A bare wait would also wait on the nodes
  # this shell just started, which never exit.
  mkdir -p "$BASE/ids"
  local jobs="" i=0
  while [ "$i" -lt "$NODES" ]; do
    ( n=0
      while [ "$n" -lt 60 ]; do
        info "$(http_port "$i")" /node/id > "$BASE/ids/$i.json" 2>/dev/null
        grep -q nodeID "$BASE/ids/$i.json" && break
        sleep 5; n=$((n + 1))
      done ) &
    jobs="$jobs $!"
    i=$((i + 1))
  done
  for j in $jobs; do wait "$j"; done
  cmd_stop > /dev/null

  python3 - "$BASE" "$NODES" <<'PY'
import json, sys, pathlib
base, n = pathlib.Path(sys.argv[1]), int(sys.argv[2])
out = []
for i in range(n):
    f = base / "ids" / f"{i}.json"
    if not f.exists(): continue
    d = json.loads(f.read_text() or "{}")
    if "nodeID" in d and "nodePOP" in d:
        out.append({"nodeID": d["nodeID"], "signer": d["nodePOP"]})
json.dump(out, open(base / "identities.json", "w"), indent=1)
print(f"  {len(out)} of {n} minted")
PY
}

# Second pass: a genesis whose validator set is this fleet. Without it the
# nodes have no one to poll who counts, so nothing ever finalises and every
# follower sits refusing certs it cannot verify.
build_genesis() {
  base_genesis "$BASE/genesis.json"
  python3 - "$BASE" "$NODES" <<'PY'
import json, pathlib, sys
base = pathlib.Path(sys.argv[1])
g = json.load(open(base / "genesis.json"))
ids = json.load(open(base / "identities.json"))[:int(sys.argv[2])]
first = g["initialStakers"][0]
g["initialStakers"] = [
    {"nodeID": d["nodeID"], "rewardAddress": first["rewardAddress"],
     "delegationFee": first["delegationFee"], "signer": d["signer"]}
    for d in ids
]
json.dump(g, open(base / "genesis.json", "w"))
staked = sum(a["initialAmount"] for a in g["allocations"]
             if a.get("utxoAddr") in g["initialStakedFunds"])
print(f"  {len(ids)} validators sharing {staked/1e9:,.0f} LUX of stake")
PY
}

cmd_start() {
  mkdir -p "$BASE"
  stage
  mint_identities
  build_genesis

  # Genesis changed, so the chain it describes is a different chain. The
  # staking keys stay — they are what genesis just named.
  local i=0
  while [ "$i" -lt "$NODES" ]; do
    local d; d="$(node_dir "$i")"
    # genesis.bytes is the built genesis cached for hash stability. It is
    # read in preference to the file, so leaving it behind silently reboots
    # the fleet onto the validator set it had before.
    rm -rf "$d/db" "$d/chains" "$d/logs" "$d/genesis.bytes"
    i=$((i + 1))
  done

  echo "starting $NODES validators"
  # Every node in this fleet is a genesis validator, so none of them is a
  # special case: they all dial the same handful of seeds, and the seeds are
  # among them. The ids come off disk, so nothing has to be running first.
  local seeds_ip seeds_id
  seeds_ip=$(python3 -c 'import json,sys
n=min(int(sys.argv[2]),int(sys.argv[4]))
print(",".join(f"127.0.0.1:{int(sys.argv[3])+j*2+1}" for j in range(n)))' "$BASE" "$SEEDS" "$PORT_BASE" "$NODES")
  seeds_id=$(python3 -c 'import json,sys
ids=json.load(open(sys.argv[1]+"/identities.json"))
print(",".join(d["nodeID"] for d in ids[:int(sys.argv[2])]))' "$BASE" "$SEEDS")
  [ -n "$seeds_id" ] || { echo "no seed ids"; exit 1; }
  i=0
  while [ "$i" -lt "$NODES" ]; do
    local free avail; free=$(free_gb); avail=$(avail_gb)
    if [ "$free" -lt "$MIN_FREE_GB" ]; then
      echo "stopping at $i nodes: ${free}G disk free, below the ${MIN_FREE_GB}G floor"; break
    fi
    if [ "$avail" -lt "$MIN_AVAIL_GB" ]; then
      echo "stopping at $i nodes: ${avail}G memory available, below the ${MIN_AVAIL_GB}G floor"; break
    fi
    local end=$((i + BATCH)); [ "$end" -gt "$NODES" ] && end="$NODES"
    printf 'nodes %d-%d  (disk %sG, memory %sG, %s)\n' "$i" "$((end - 1))" "$free" "$avail" "$(swap_used)"
    while [ "$i" -lt "$end" ]; do
      start_node "$i" "$seeds_ip" "$seeds_id"
      i=$((i + 1))
    done
    sleep 10
  done
  echo
  cmd_status
}

cmd_status() {
  local up=0 total=0 peers
  for d in "$BASE"/node-*; do
    [ -d "$d" ] || continue
    total=$((total + 1))
    kill -0 "$(cat "$d/pid" 2>/dev/null)" 2>/dev/null && up=$((up + 1))
  done
  peers=$(info "$(http_port 0)" /peers | sed -n 's/.*"numPeers":"\{0,1\}\([0-9]*\).*/\1/p')
  printf '%s of %s processes alive, node 0 sees %s peers\n' "$up" "$total" "${peers:-?}"

  # Liveness is not participation: a node can hold a socket open for hours
  # while its chains never finish bootstrapping. Asked one at a time, a fleet
  # of unresponsive nodes takes the whole timeout each.
  local probe; probe="$(mktemp -d)"
  local n=0 jobs=""
  while [ "$n" -lt "$total" ]; do
    ( info "$(http_port "$n")" "/chain/bootstrapped?chain=C" > "$probe/$n" 2>/dev/null ) &
    jobs="$jobs $!"
    n=$((n + 1))
  done
  for j in $jobs; do wait "$j"; done
  printf '%s of %s report the C-Chain bootstrapped\n' "$(grep -l true "$probe"/* 2>/dev/null | wc -l | tr -d ' ')" "$total"
  rm -rf "$probe"

  printf 'disk: %sG free, %s used by the fleet\n' "$(free_gb)" "$(du -sh "$BASE" 2>/dev/null | cut -f1)"
  printf 'mem:  %sG available\n' "$(avail_gb)"
  printf 'rss:  %s MB total across luxd + plugins\n' \
    "$(ps -o rss=,command= -A | awk '/luxd|'"$EVM_VMID"'/{s+=$1} END{printf "%.0f", s/1024}')"
  printf '%s\n' "$(swap_used)"
}

cmd_stop() {
  for d in "$BASE"/node-*; do
    [ -f "$d/pid" ] && kill "$(cat "$d/pid")" 2>/dev/null
  done
  sleep 3
  pkill -f "$BASE/node-" 2>/dev/null
  echo "stopped"
}

case "${1:-start}" in
  start)  cmd_start ;;
  status) cmd_status ;;
  stop)   cmd_stop ;;
  clean)  cmd_stop; rm -rf "$BASE"; echo "removed $BASE" ;;
  *) echo "usage: NODES=100 $0 {start|status|stop|clean}"; exit 2 ;;
esac
