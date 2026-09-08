#!/usr/bin/env bash
# time from spawn to the first request the API server answers, then one health
# sample 30s later. usage: listen.sh <label> <bin> <hp> <sp> <rounds> <live> <health>
set -u
label=$1; bin=$2; hp=$3; sp=$4; rounds=$5; live=$6; health=$7
for r in $(seq 1 "$rounds"); do
  dir=$(mktemp -d /tmp/listen.XXXXXX); log=$dir/node.log
  at=$(cut -d' ' -f1 /proc/loadavg)
  t0=$(date +%s.%N)
  "$bin" --network-id=local --data-dir="$dir" --http-port="$hp" --staking-port="$sp" \
         --sybil-protection-enabled=false --log-level=info >"$log" 2>&1 &
  pid=$!; tl=""
  for _ in $(seq 1 1200); do
    kill -0 "$pid" 2>/dev/null || break
    [ "$(curl -s -o /dev/null -w '%{http_code}' -m 2 "http://localhost:$hp$live" 2>/dev/null)" = "200" ] && { tl=$(echo "$(date +%s.%N) - $t0" | bc); break; }
    sleep 0.2
  done
  hv="n/a"
  if [ -n "$tl" ]; then
    sleep 30
    hv=$(curl -s -m 4 "http://localhost:$hp$health" 2>/dev/null | python3 -c 'import sys,json;d=json.load(sys.stdin);print(str(d.get("healthy")))' 2>/dev/null || echo "unreadable")
  fi
  echo "$label round$r load=$at listening=${tl:-NEVER}s healthy_at_listen+30s=$hv"
  kill -TERM "$pid" 2>/dev/null
  for _ in $(seq 1 100); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  kill -KILL "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; rm -rf "$dir"
done
