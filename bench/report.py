#!/usr/bin/env python3
"""Summarise bench/run.sh output: median requests/second per server, test
and pipeline depth, as a Markdown report."""
import csv
import pathlib
import statistics
import sys
from collections import defaultdict

out = pathlib.Path(sys.argv[1])
servers = [line.split("|", 1)[0] for line in (out / "servers.txt").read_text().splitlines() if line]
slug = lambda n: "".join(c for c in n.lower().replace(" ", "_").replace("(", "_").replace(")", "_") if c.isalnum() or c in "_-")

# results[pipeline][test][server] = [rps, ...], p50 likewise
rps = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
p50 = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
tests = []
for name in servers:
    for f in sorted(out.glob(f"{slug(name)}_p*_r*.csv")):
        pipeline = int(f.name.rsplit("_p", 1)[1].split("_")[0])
        for row in csv.DictReader(f.open()):
            test = row["test"].split()[0]
            if test not in tests:
                tests.append(test)
            rps[pipeline][test][name].append(float(row["rps"]))
            p50[pipeline][test][name].append(float(row["p50_latency_ms"]))

env_lines = (out / "env.txt").read_text().splitlines()
env = dict(line.split("=", 1) for line in env_lines if "=" in line)
images = [line.split("|")[1] for line in (out / "servers.txt").read_text().splitlines() if line]
print("# Benchmark results\n")
print(f"- CPU: {env.get('cpu_model', '?')} ({env.get('cpus', '?')} logical CPUs), {env.get('kernel', '')}")
print(f"- Pinning: {env_lines[3]}")
print(f"- Load: {env_lines[4]}")
print(f"- BoltCache commit: {env.get('boltcache_commit', '?')}")
print("- Images: " + ", ".join(images))
print("\nMedian requests/second (higher is better); p50 latency in ms below each value.\n")
for pipeline in sorted(rps):
    print(f"## Pipeline {pipeline}\n")
    print("| Test | " + " | ".join(servers) + " |")
    print("|---|" + "---:|" * len(servers))
    for test in tests:
        cells = []
        best = max((statistics.median(v) for v in rps[pipeline][test].values() if v), default=0)
        for name in servers:
            v = rps[pipeline][test][name]
            if not v:
                cells.append("–")
                continue
            m = statistics.median(v)
            lat = statistics.median(p50[pipeline][test][name])
            cell = f"{m:,.0f}<br><sub>{lat:.3f} ms</sub>"
            cells.append(f"**{cell}**" if m == best else cell)
        print(f"| {test} | " + " | ".join(cells) + " |")
    print()

# memtier results: ops/s, latency and server CPU usage.
mt = defaultdict(lambda: defaultdict(list))  # mt[pipeline][server] = [(ops, p50, p99, p999, cores)]
for name in servers:
    for f in sorted(out.glob(f"{slug(name)}_memtier_p*_r*.txt")):
        pipeline = int(f.name.split("_memtier_p", 1)[1].split("_")[0])
        parts = f.read_text().split()
        if len(parts) == 5:
            mt[pipeline][name].append(tuple(float(x) for x in parts))
if mt:
    print("## memtier: 50% SET / 50% GET, random keys\n")
    print("Median of the rounds. *Cores used* is the server's CPU time divided by wall time: "
          "a server stuck at ~1-2 cores while more are available is limited by a single thread.\n")
    for pipeline in sorted(mt):
        print(f"### Pipeline {pipeline}\n")
        print("| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |")
        print("|---|---:|---:|---:|---:|---:|---:|")
        best = max(statistics.median(r[0] for r in rows) for rows in mt[pipeline].values() if rows)
        for name in servers:
            rows = mt[pipeline][name]
            if not rows:
                continue
            med = lambda i: statistics.median(r[i] for r in rows)
            ops = med(0)
            cell = f"{ops:,.0f}"
            if ops == best:
                cell = f"**{cell}**"
            cores = med(4)
            eff = f"{ops / cores:,.0f}" if cores > 0 else "–"
            print(f"| {name} | {cell} | {med(1):.3f} | {med(2):.3f} | {med(3):.3f} | {cores:.2f} | {eff} |")
        print()
