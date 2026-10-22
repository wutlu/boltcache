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
