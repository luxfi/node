#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# One benchmark, several builds of the node, timed against each other.
#
# The question this answers is not "how fast is this node" but "is this node
# faster than the one it replaced, and than the one it forked from". That only
# means something when all three do the SAME work, so this program does not
# choose the work: it is handed a compiled Go test binary per build and one
# benchmark name that exists in all of them, and it runs that name.
#
# Two things are deliberate.
#
# FIXED ITERATIONS. Go's own benchmark loop picks how many iterations to run
# from how fast the machine is answering, so on a loaded box two builds are
# handed different amounts of work and the ns/op that comes back is an average
# over different samples. Every run here is -test.benchtime=Nx, the same N for
# every build, so the three binaries perform an identical count of an identical
# operation and the only free variable left is how long it took.
#
# INTERLEAVED. The builds are run round-robin -- every build once, then every
# build again -- rather than all of one build's runs and then the next. A box
# whose load moves during the measurement then moves it for all of them at
# once instead of taxing whichever build happened to run while a neighbour was
# compiling. The load average is read at the top of every round and printed on
# every line, because a number from a machine at 40 and a number from the same
# machine at 460 are not comparable and the reader cannot tell them apart
# afterwards unless it was written down at the time.
#
# A SINGLE TIMING IS NOT A MEASUREMENT: the median of the rounds is reported
# beside the min and the max, and the spread is usually the more honest half.
import json
import re
import statistics
import subprocess
import sys
import time

NS = re.compile(r"^(Benchmark\S*?)(?:-\d+)?\s+(\d+)\s+([0-9.]+) ns/op")


def load():
    return open("/proc/loadavg").read().split()[0]


def once(binary, bench, iters):
    """Run one benchmark once, at a fixed iteration count. Returns (exit, {name: (iters, ns)})."""
    cmd = [binary, "-test.run", "^$", "-test.bench", bench,
           "-test.benchtime", f"{iters}x", "-test.count", "1"]
    started = time.time()
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=3600)
    seen = {}
    for line in p.stdout.split("\n"):
        m = NS.match(line.strip())
        if m:
            seen[m.group(1)] = (int(m.group(2)), float(m.group(3)))
    return p.returncode, seen, time.time() - started, p.stdout + p.stderr


def compare(name, builds, bench, iters, rounds):
    """builds: [(label, path)]. Every label runs every round, in order."""
    times = {label: [] for label, _ in builds}
    counts = {}
    print(f"\n===== {name}  bench={bench}  iters={iters}x  rounds={rounds}", flush=True)
    for r in range(rounds):
        at = load()
        for label, path in builds:
            code, seen, wall, raw = once(path, bench, iters)
            if code != 0 or not seen:
                print(f"  round{r+1} load={at} {label:5s} EXIT={code} NO RESULT", flush=True)
                print(raw[-1200:], flush=True)
                continue
            iters_ran, ns = list(seen.values())[0]
            times[label].append(ns)
            counts[label] = iters_ran
            print(f"  round{r+1} load={at:>7s} {label:5s} exit={code} "
                  f"{ns:>14.1f} ns/op  iters={iters_ran} wall={wall:.1f}s", flush=True)
    print(f"  --- {name}", flush=True)
    for label, _ in builds:
        v = times[label]
        if not v:
            print(f"  {label:5s} NO DATA", flush=True)
            continue
        print(f"  {label:5s} median {statistics.median(v):>14.1f}  "
              f"min {min(v):>14.1f}  max {max(v):>14.1f}  runs={len(v)}  "
              f"iters/run={counts[label]}", flush=True)
    return times


def main():
    if len(sys.argv) != 3:
        print("usage: across.py <spec.json> <out.json>", file=sys.stderr)
        return 2
    spec = json.load(open(sys.argv[1]))
    out = {}
    for g in spec:
        out[g["name"]] = compare(g["name"], [tuple(m) for m in g["builds"]],
                                 g["bench"], g["iters"], g["rounds"])
    json.dump(out, open(sys.argv[2], "w"), indent=1)
    return 0


if __name__ == "__main__":
    sys.exit(main())
