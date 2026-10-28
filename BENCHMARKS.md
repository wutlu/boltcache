# Benchmarks

Every number here comes from [`bench/run.sh`](bench/run.sh), which anyone can run. The [Benchmark workflow](.github/workflows/benchmark.yml) runs it on GitHub Actions; raw CSV files of each run are attached to the workflow run as an artifact.

## Method

- **Servers**: BoltCache, Redis with and without I/O threads, Valkey with I/O threads and Dragonfly, each in Docker with `--network host` and the **same CPU budget** (`--cpuset-cpus`). Persistence is disabled for all of them.
- **Client**: `redis-benchmark` pinned to **different cores** than the server, with as many threads as it has cores.
- **Workloads**:
  - `redis-benchmark`: `SET`, `GET`, `INCR`, `LPUSH`, `HSET`, `ZADD`; 50 connections; random keys from a 100,000-key space (`-r`); 32-byte values; without pipelining and with pipeline depth 16.
  - `memtier_benchmark`: 50% `SET` / 50% `GET` on random keys, same connection count and value size. It also records how many CPU cores each server actually used, which shows whether a server is limited by a single thread.
- **Rounds**: every scenario runs 3 times on a freshly started server and the median is reported, along with the median p50 latency.

## Caveats

Please read these before quoting numbers:

- **GitHub's runners are small, shared VMs** (4 vCPUs): 2 cores for the server, 2 for the client. Expect noise between runs. Multi-threaded servers (BoltCache, Dragonfly) can't show how they scale on many cores. On a larger machine, run `SERVER_CPUS=0-7 CLIENT_CPUS=8-15 bench/run.sh`.
- **Without pipelining, the client is the bottleneck** on this machine: most servers land at roughly the same throughput, which is why the latency column is more informative there.
- `redis-benchmark`'s `LPUSH`, `HSET` and `ZADD` tests write to a **single key**. That's a worst case for designs with fine-grained locking like BoltCache (every connection contends for the same lock) and a best case for single-threaded servers.
- `redis-benchmark` computes requests/second from millisecond timestamps, so identical values across servers usually mean "the same within measurement precision".

## Results

### 2026-10-28, GitHub Actions, 2 server cores, BoltCache 2.2

Run: [actions/runs/40603959793](https://github.com/wutlu/boltcache/actions/runs/40603959793). Raw files: [`bench/published/2026-10-28-github-actions`](bench/published/2026-10-28-github-actions).
- CPU: AMD EPYC 7763 64-Core Processor (4 logical CPUs), Linux 7.0.0-1017-azure
- Pinning: server_cpus=0,1 client_cpus=2,3 threads=2
- Load: clients=50 requests=500000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=3
- BoltCache commit: 2f59d5f
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

#### Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 105,197<br><sub>0.247 ms</sub> | 95,220<br><sub>0.455 ms</sub> | 83,319<br><sub>0.551 ms</sub> | 74,052<br><sub>0.647 ms</sub> | **111,062<br><sub>0.271 ms</sub>** |
| GET | 105,197<br><sub>0.247 ms</sub> | 99,980<br><sub>0.439 ms</sub> | 90,893<br><sub>0.519 ms</sub> | 71,398<br><sub>0.639 ms</sub> | **111,037<br><sub>0.263 ms</sub>** |
| INCR | 105,241<br><sub>0.247 ms</sub> | 95,220<br><sub>0.447 ms</sub> | 86,941<br><sub>0.527 ms</sub> | 74,052<br><sub>0.647 ms</sub> | **111,062<br><sub>0.263 ms</sub>** |
| LPUSH | **105,175<br><sub>0.247 ms</sub>** | 99,980<br><sub>0.439 ms</sub> | 86,941<br><sub>0.535 ms</sub> | 68,937<br><sub>0.655 ms</sub> | 95,220<br><sub>0.447 ms</sub> |
| HSET | **105,219<br><sub>0.247 ms</sub>** | 95,220<br><sub>0.447 ms</sub> | 83,306<br><sub>0.543 ms</sub> | 74,063<br><sub>0.655 ms</sub> | 99,980<br><sub>0.391 ms</sub> |
| ZADD | 105,197<br><sub>0.239 ms</sub> | 86,926<br><sub>0.503 ms</sub> | 79,987<br><sub>0.575 ms</sub> | 74,052<br><sub>0.663 ms</sub> | **105,241<br><sub>0.423 ms</sub>** |

#### Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **1,142,204<br><sub>0.511 ms</sub>** | 727,008<br><sub>0.959 ms</sub> | 666,445<br><sub>1.111 ms</sub> | 727,008<br><sub>0.999 ms</sub> | 571,102<br><sub>1.311 ms</sub> |
| GET | **1,142,204<br><sub>0.551 ms</sub>** | 888,494<br><sub>0.727 ms</sub> | 888,494<br><sub>0.799 ms</sub> | 799,360<br><sub>0.895 ms</sub> | 666,222<br><sub>1.119 ms</sub> |
| INCR | **1,332,445<br><sub>0.503 ms</sub>** | 888,494<br><sub>0.775 ms</sub> | 888,494<br><sub>0.807 ms</sub> | 799,680<br><sub>0.911 ms</sub> | 615,006<br><sub>1.175 ms</sub> |
| LPUSH | **1,142,204<br><sub>0.495 ms</sub>** | 799,360<br><sub>0.783 ms</sub> | 799,680<br><sub>0.903 ms</sub> | 727,008<br><sub>1.015 ms</sub> | 319,846<br><sub>1.223 ms</sub> |
| HSET | **1,142,204<br><sub>0.599 ms</sub>** | 787,402<br><sub>0.927 ms</sub> | 727,008<br><sub>0.943 ms</sub> | 666,222<br><sub>1.079 ms</sub> | 499,875<br><sub>0.759 ms</sub> |
| ZADD | **571,265<br><sub>1.271 ms</sub>** | 443,853<br><sub>1.855 ms</sub> | 499,625<br><sub>1.559 ms</sub> | 420,698<br><sub>1.839 ms</sub> | 319,898<br><sub>1.215 ms</sub> |

#### memtier: 50% SET / 50% GET, random keys

Median of the rounds. *Cores used* is the server's CPU time divided by wall time: a server stuck at ~1-2 cores while more are available is limited by a single thread.

##### Pipeline 1

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | 105,603 | 0.447 | 0.703 | 2.431 | 1.67 | 63,235 |
| Redis | 101,801 | 0.479 | 0.799 | 1.375 | 1.00 | 101,801 |
| Redis (io-threads 2) | 87,495 | 0.575 | 0.815 | 1.887 | 1.20 | 72,913 |
| Valkey (io-threads 2) | 76,193 | 0.655 | 0.727 | 2.383 | 2.02 | 37,719 |
| Dragonfly | **108,663** | 0.407 | 0.695 | 1.711 | 1.95 | 55,725 |

##### Pipeline 16

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **939,429** | 0.799 | 1.255 | 5.247 | 1.42 | 661,569 |
| Redis | 828,265 | 0.895 | 1.463 | 2.655 | 1.01 | 820,065 |
| Redis (io-threads 2) | 789,050 | 1.007 | 1.415 | 2.959 | 1.69 | 466,894 |
| Valkey (io-threads 2) | 808,323 | 0.967 | 1.647 | 3.711 | 2.01 | 402,151 |
| Dragonfly | 638,488 | 1.231 | 1.935 | 3.791 | 2.00 | 319,244 |


**Reading these results:** On this small runner BoltCache leads every pipelined test, including `ZADD`, which it lost in 2.1. Without pipelining all servers sit at the client's limit (about 100K ops/s). BoltCache used only 1.4 of its 2 cores in the pipelined memtier run, so the client was the bottleneck there too.

### 2026-10-28, 4 server cores (Apple M1 Pro, Docker Linux VM), BoltCache 2.2

Raw files: [`bench/published/2026-10-28-m1pro-docker-4cores`](bench/published/2026-10-28-m1pro-docker-4cores). 4 cores for the server, 6 for the client, 1 round each. This is a laptop VM rather than dedicated hardware, so treat it as indicative; the GitHub Actions run below is the independently reproducible one.
- CPU: Apple M1 Pro (10 logical CPUs), Darwin 24.6.0
- Pinning: server_cpus=0-3 client_cpus=4-9 threads=4
- Load: clients=50 requests=300000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=1
- BoltCache commit: 218184f
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

#### Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 4) | Valkey (io-threads 4) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **199,468<br><sub>0.095 ms</sub>** | 133,156<br><sub>0.279 ms</sub> | 133,156<br><sub>0.231 ms</sub> | 132,979<br><sub>0.087 ms</sub> | 132,567<br><sub>0.167 ms</sub> |
| GET | **399,467<br><sub>0.063 ms</sub>** | 149,701<br><sub>0.271 ms</sub> | 239,808<br><sub>0.167 ms</sub> | 298,211<br><sub>0.055 ms</sub> | 300,000<br><sub>0.103 ms</sub> |
| INCR | **399,467<br><sub>0.055 ms</sub>** | 149,925<br><sub>0.271 ms</sub> | 239,808<br><sub>0.167 ms</sub> | 299,401<br><sub>0.071 ms</sub> | 299,700<br><sub>0.103 ms</sub> |
| LPUSH | **300,000<br><sub>0.071 ms</sub>** | 149,701<br><sub>0.303 ms</sub> | 199,867<br><sub>0.183 ms</sub> | 299,401<br><sub>0.071 ms</sub> | 239,808<br><sub>0.143 ms</sub> |
| HSET | **299,700<br><sub>0.079 ms</sub>** | 171,331<br><sub>0.271 ms</sub> | 199,867<br><sub>0.175 ms</sub> | 298,507<br><sub>0.063 ms</sub> | 239,808<br><sub>0.151 ms</sub> |
| ZADD | **300,000<br><sub>0.087 ms</sub>** | 133,274<br><sub>0.303 ms</sub> | 199,867<br><sub>0.183 ms</sub> | 299,401<br><sub>0.079 ms</sub> | 239,808<br><sub>0.159 ms</sub> |

#### Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 4) | Valkey (io-threads 4) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **1,596,169<br><sub>0.207 ms</sub>** | 995,520<br><sub>0.623 ms</sub> | 1,138,952<br><sub>0.471 ms</sub> | 1,138,304<br><sub>0.375 ms</sub> | 887,311<br><sub>0.455 ms</sub> |
| GET | 1,990,050<br><sub>0.167 ms</sub> | 1,330,672<br><sub>0.479 ms</sub> | **1,998,002<br><sub>0.271 ms</sub>** | 1,994,018<br><sub>0.255 ms</sub> | **1,998,002<br><sub>0.335 ms</sub>** |
| INCR | **2,663,116<br><sub>0.143 ms</sub>** | 1,332,445<br><sub>0.471 ms</sub> | 1,998,002<br><sub>0.263 ms</sub> | 1,998,002<br><sub>0.287 ms</sub> | 1,998,002<br><sub>0.343 ms</sub> |
| LPUSH | **2,663,116<br><sub>0.127 ms</sub>** | 614,062<br><sub>1.151 ms</sub> | 798,722<br><sub>0.847 ms</sub> | 726,744<br><sub>1.007 ms</sub> | 999,500<br><sub>0.551 ms</sub> |
| HSET | **2,000,000<br><sub>0.255 ms</sub>** | 1,141,552<br><sub>0.607 ms</sub> | 1,998,002<br><sub>0.303 ms</sub> | 1,600,000<br><sub>0.407 ms</sub> | 999,500<br><sub>0.527 ms</sub> |
| ZADD | 613,874<br><sub>0.319 ms</sub> | 531,632<br><sub>1.263 ms</sub> | **887,311<br><sub>0.767 ms</sub>** | 666,222<br><sub>1.111 ms</sub> | 570,776<br><sub>0.887 ms</sub> |

#### memtier: 50% SET / 50% GET, random keys

Median of the rounds. *Cores used* is the server's CPU time divided by wall time: a server stuck at ~1-2 cores while more are available is limited by a single thread.

##### Pipeline 1

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **441,342** | 0.079 | 0.671 | 2.799 | 3.41 | 129,426 |
| Redis | 180,909 | 0.279 | 0.719 | 2.735 | 1.04 | 173,951 |
| Redis (io-threads 4) | 229,428 | 0.207 | 0.815 | 3.407 | 2.81 | 81,647 |
| Valkey (io-threads 4) | 331,396 | 0.127 | 1.063 | 5.343 | 4.17 | 79,471 |
| Dragonfly | 304,929 | 0.135 | 0.943 | 4.127 | 3.51 | 86,874 |

##### Pipeline 16

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **3,229,880** | 0.183 | 1.495 | 5.055 | 3.42 | 944,409 |
| Redis | 1,451,428 | 0.551 | 1.423 | 4.511 | 1.01 | 1,437,057 |
| Redis (io-threads 4) | 2,173,474 | 0.351 | 1.255 | 3.775 | 3.02 | 719,693 |
| Valkey (io-threads 4) | 1,920,773 | 0.399 | 1.751 | 6.687 | 4.08 | 470,778 |
| Dragonfly | 1,808,355 | 0.415 | 1.663 | 6.879 | 4.03 | 448,723 |


**Reading these results:**

- With 4 cores BoltCache leads every `memtier` scenario. With pipelining it is about 2.2× single-threaded Redis and 1.5× Redis with I/O threads. Without pipelining it is about 2.4× Redis.
- Redis with I/O threads stays around 2.2M ops/s. Its I/O threads only move socket reads and writes off the main thread, and commands still execute on that one thread, so it can't use all the cores it is given. BoltCache executes commands on every core.
- Per CPU core, single-threaded Redis is still the most efficient server (*ops per core-second*). BoltCache wins by using more cores, not by doing less work per command.
- `ZADD` with pipelining writes to one hot key, which BoltCache has to serialize. There, Redis with I/O threads leads (887K vs 614K ops/s).

### 2026-10-22, GitHub Actions, BoltCache 2.1 (before the 2.2 optimizations)

Run: [actions/runs/39810262288](https://github.com/wutlu/boltcache/actions/runs/39810262288). Raw CSV files: [`bench/published/2026-10-22-github-actions`](bench/published/2026-10-22-github-actions).

- CPU: AMD EPYC 7763 64-Core Processor (4 logical CPUs), Linux 7.0.0-1017-azure
- Pinning: server_cpus=0,1 client_cpus=2,3 threads=2
- Load: clients=50 requests=500000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=3
- BoltCache commit: 63613c8
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

#### Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 105,219<br><sub>0.247 ms</sub> | 99,980<br><sub>0.439 ms</sub> | 83,319<br><sub>0.551 ms</sub> | 76,899<br><sub>0.647 ms</sub> | **111,062<br><sub>0.279 ms</sub>** |
| GET | 105,197<br><sub>0.239 ms</sub> | 99,980<br><sub>0.431 ms</sub> | 90,893<br><sub>0.519 ms</sub> | 76,911<br><sub>0.631 ms</sub> | **111,037<br><sub>0.263 ms</sub>** |
| INCR | 105,197<br><sub>0.239 ms</sub> | 99,980<br><sub>0.447 ms</sub> | 86,941<br><sub>0.527 ms</sub> | 76,911<br><sub>0.631 ms</sub> | **111,062<br><sub>0.271 ms</sub>** |
| LPUSH | **105,197<br><sub>0.247 ms</sub>** | 99,980<br><sub>0.439 ms</sub> | 86,941<br><sub>0.535 ms</sub> | 74,063<br><sub>0.639 ms</sub> | 95,220<br><sub>0.463 ms</sub> |
| HSET | 105,219<br><sub>0.239 ms</sub> | 95,220<br><sub>0.447 ms</sub> | 83,319<br><sub>0.543 ms</sub> | 74,063<br><sub>0.647 ms</sub> | **105,241<br><sub>0.399 ms</sub>** |
| ZADD | **99,980<br><sub>0.439 ms</sub>** | 86,941<br><sub>0.495 ms</sub> | 79,987<br><sub>0.575 ms</sub> | 74,063<br><sub>0.655 ms</sub> | **99,980<br><sub>0.439 ms</sub>** |

#### Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 666,445<br><sub>0.951 ms</sub> | **799,360<br><sub>0.919 ms</sub>** | 727,008<br><sub>1.039 ms</sub> | 726,744<br><sub>0.991 ms</sub> | 570,939<br><sub>1.295 ms</sub> |
| GET | 727,008<br><sub>0.879 ms</sub> | **888,494<br><sub>0.703 ms</sub>** | **888,494<br><sub>0.775 ms</sub>** | 799,680<br><sub>0.887 ms</sub> | 666,222<br><sub>1.095 ms</sub> |
| INCR | 727,008<br><sub>0.879 ms</sub> | **888,494<br><sub>0.759 ms</sub>** | **888,494<br><sub>0.791 ms</sub>** | 799,680<br><sub>0.895 ms</sub> | 666,445<br><sub>1.135 ms</sub> |
| LPUSH | **799,680<br><sub>0.719 ms</sub>** | 799,360<br><sub>0.847 ms</sub> | **799,680<br><sub>0.895 ms</sub>** | 727,008<br><sub>0.999 ms</sub> | 319,898<br><sub>1.095 ms</sub> |
| HSET | **799,680<br><sub>0.863 ms</sub>** | 799,360<br><sub>0.919 ms</sub> | 799,360<br><sub>0.943 ms</sub> | 726,744<br><sub>1.055 ms</sub> | 533,191<br><sub>0.759 ms</sub> |
| ZADD | 185,977<br><sub>4.079 ms</sub> | 443,951<br><sub>1.855 ms</sub> | **499,625<br><sub>1.567 ms</sub>** | 444,247<br><sub>1.687 ms</sub> | 333,222<br><sub>0.919 ms</sub> |

**Reading these results:**

- **Without pipelining** BoltCache is in the top group with Dragonfly, at about half Redis' median latency (0.24 ms vs 0.44 ms). All servers are close to the client's limit here.
- **With pipelining** BoltCache leads `LPUSH` and `HSET` and is 10–18% behind single-threaded Redis on `SET`, `GET` and `INCR` with this 2-core budget.
- **`ZADD` with pipelining is a known weakness.** BoltCache reaches about 40% of Redis' throughput. The skiplist operation itself is slower in Go than in Redis' C implementation (about 2.4 µs per random insert into a 100,000-member set), and every connection contends for the one sorted set this test writes to. Improving it is on the roadmap.
- Redis' I/O threads and Valkey don't help with only 2 cores. All of these servers spread work across more cores, so on bigger machines the picture changes. Run the script there before deciding.

