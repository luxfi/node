#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# How long a node takes to come up, and how long until it calls itself healthy.
#
# Two clocks, because they answer different questions and only one of them is
# comparable between projects that boot a different number of chains:
#
#   listening -- the first request the API server answers at all. This is
#     process start, config parse, database open, API server bind. Every node
#     here does the same amount of that work.
#   healthy   -- the first time the health endpoint says every chain it decided
#     to run has bootstrapped. A node that runs twelve chains is doing more work
#     to reach this than a node that runs three, so the number is only read
#     next to the chain count, which is printed beside it.
#
# The health path is an argument because it is not the same string in every
# project: this node serves /v1/health/ops/*, the project it forked from serves
# /ext/health/*. Asking the wrong one returns 404 forever, which would look
# exactly like a node that never became healthy.
#
# A fresh data directory every round -- the second start of a node that already
# has a database is a different measurement from the first. High ports and
# private directories, because this box runs other people's nodes; only the pid
# this script spawned is ever signalled.
#
# usage: healthy.sh <label> <binary> <httpport> <stakingport> <rounds> <livepath> <healthpath> [flags...]
set -u
label=$1; bin=$2; hp=$3; sp=$4; rounds=$5; live=$6; health=$7; shift 7

for r in $(seq 1 "$rounds"); do
  dir=$(mktemp -d /tmp/healthy.XXXXXX)
  log=$dir/node.log
  at=$(cut -d' ' -f1 /proc/loadavg)
  t0=$(date +%s.%N)
  "$bin" --network-id=local --data-dir="$dir" --http-port="$hp" --staking-port="$sp" \
         --sybil-protection-enabled=false --log-level=info "$@" >"$log" 2>&1 &
  pid=$!
  tlive=""; thealth=""
  for _ in $(seq 1 3000); do          # 300s ceiling at 0.1s a poll
    kill -0 "$pid" 2>/dev/null || break
    if [ -z "$tlive" ]; then
      code=$(curl -s -o /dev/null -w '%{http_code}' -m 2 "http://localhost:$hp$live" 2>/dev/null)
      [ "$code" = "200" ] && tlive=$(echo "$(date +%s.%N) - $t0" | bc)
    fi
    if [ -n "$tlive" ]; then
      body=$(curl -s -m 2 "http://localhost:$hp$health" 2>/dev/null)
      if [[ "$body" == *'"healthy":true'* ]]; then
        thealth=$(echo "$(date +%s.%N) - $t0" | bc); break
      fi
    fi
    sleep 0.1
  done
  chains=$(grep -ac "CHAIN CREATED SUCCESSFULLY\|created chain\|Creating chain" "$log" 2>/dev/null || true)
  echo "$label round$r load=$at listening=${tlive:-NEVER}s healthy=${thealth:-NEVER}s chains=$chains"
  kill -TERM "$pid" 2>/dev/null
  for _ in $(seq 1 150); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  kill -KILL "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  rm -rf "$dir"
done
