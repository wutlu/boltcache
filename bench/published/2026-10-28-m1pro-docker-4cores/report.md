# Benchmark results

- CPU: Apple M1 Pro (10 logical CPUs), Darwin 24.6.0
- Pinning: server_cpus=0-3 client_cpus=4-9 threads=4
- Load: clients=50 requests=300000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=1
- BoltCache commit: 218184f
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

## Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 4) | Valkey (io-threads 4) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **199,468<br><sub>0.095 ms</sub>** | 133,156<br><sub>0.279 ms</sub> | 133,156<br><sub>0.231 ms</sub> | 132,979<br><sub>0.087 ms</sub> | 132,567<br><sub>0.167 ms</sub> |
| GET | **399,467<br><sub>0.063 ms</sub>** | 149,701<br><sub>0.271 ms</sub> | 239,808<br><sub>0.167 ms</sub> | 298,211<br><sub>0.055 ms</sub> | 300,000<br><sub>0.103 ms</sub> |
| INCR | **399,467<br><sub>0.055 ms</sub>** | 149,925<br><sub>0.271 ms</sub> | 239,808<br><sub>0.167 ms</sub> | 299,401<br><sub>0.071 ms</sub> | 299,700<br><sub>0.103 ms</sub> |
| LPUSH | **300,000<br><sub>0.071 ms</sub>** | 149,701<br><sub>0.303 ms</sub> | 199,867<br><sub>0.183 ms</sub> | 299,401<br><sub>0.071 ms</sub> | 239,808<br><sub>0.143 ms</sub> |
| HSET | **299,700<br><sub>0.079 ms</sub>** | 171,331<br><sub>0.271 ms</sub> | 199,867<br><sub>0.175 ms</sub> | 298,507<br><sub>0.063 ms</sub> | 239,808<br><sub>0.151 ms</sub> |
| ZADD | **300,000<br><sub>0.087 ms</sub>** | 133,274<br><sub>0.303 ms</sub> | 199,867<br><sub>0.183 ms</sub> | 299,401<br><sub>0.079 ms</sub> | 239,808<br><sub>0.159 ms</sub> |

## Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 4) | Valkey (io-threads 4) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **1,596,169<br><sub>0.207 ms</sub>** | 995,520<br><sub>0.623 ms</sub> | 1,138,952<br><sub>0.471 ms</sub> | 1,138,304<br><sub>0.375 ms</sub> | 887,311<br><sub>0.455 ms</sub> |
| GET | 1,990,050<br><sub>0.167 ms</sub> | 1,330,672<br><sub>0.479 ms</sub> | **1,998,002<br><sub>0.271 ms</sub>** | 1,994,018<br><sub>0.255 ms</sub> | **1,998,002<br><sub>0.335 ms</sub>** |
| INCR | **2,663,116<br><sub>0.143 ms</sub>** | 1,332,445<br><sub>0.471 ms</sub> | 1,998,002<br><sub>0.263 ms</sub> | 1,998,002<br><sub>0.287 ms</sub> | 1,998,002<br><sub>0.343 ms</sub> |
| LPUSH | **2,663,116<br><sub>0.127 ms</sub>** | 614,062<br><sub>1.151 ms</sub> | 798,722<br><sub>0.847 ms</sub> | 726,744<br><sub>1.007 ms</sub> | 999,500<br><sub>0.551 ms</sub> |
| HSET | **2,000,000<br><sub>0.255 ms</sub>** | 1,141,552<br><sub>0.607 ms</sub> | 1,998,002<br><sub>0.303 ms</sub> | 1,600,000<br><sub>0.407 ms</sub> | 999,500<br><sub>0.527 ms</sub> |
| ZADD | 613,874<br><sub>0.319 ms</sub> | 531,632<br><sub>1.263 ms</sub> | **887,311<br><sub>0.767 ms</sub>** | 666,222<br><sub>1.111 ms</sub> | 570,776<br><sub>0.887 ms</sub> |

## memtier: 50% SET / 50% GET, random keys

Median of the rounds. *Cores used* is the server's CPU time divided by wall time: a server stuck at ~1-2 cores while more are available is limited by a single thread.

### Pipeline 1

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **441,342** | 0.079 | 0.671 | 2.799 | 3.41 | 129,426 |
| Redis | 180,909 | 0.279 | 0.719 | 2.735 | 1.04 | 173,951 |
| Redis (io-threads 4) | 229,428 | 0.207 | 0.815 | 3.407 | 2.81 | 81,647 |
| Valkey (io-threads 4) | 331,396 | 0.127 | 1.063 | 5.343 | 4.17 | 79,471 |
| Dragonfly | 304,929 | 0.135 | 0.943 | 4.127 | 3.51 | 86,874 |

### Pipeline 16

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **3,229,880** | 0.183 | 1.495 | 5.055 | 3.42 | 944,409 |
| Redis | 1,451,428 | 0.551 | 1.423 | 4.511 | 1.01 | 1,437,057 |
| Redis (io-threads 4) | 2,173,474 | 0.351 | 1.255 | 3.775 | 3.02 | 719,693 |
| Valkey (io-threads 4) | 1,920,773 | 0.399 | 1.751 | 6.687 | 4.08 | 470,778 |
| Dragonfly | 1,808,355 | 0.415 | 1.663 | 6.879 | 4.03 | 448,723 |

