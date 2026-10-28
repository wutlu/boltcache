# Benchmark results

- CPU: AMD EPYC 7763 64-Core Processor (4 logical CPUs), Linux 7.0.0-1017-azure
- Pinning: server_cpus=0,1 client_cpus=2,3 threads=2
- Load: clients=50 requests=500000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=3
- BoltCache commit: 2f59d5f
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

## Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 105,197<br><sub>0.247 ms</sub> | 95,220<br><sub>0.455 ms</sub> | 83,319<br><sub>0.551 ms</sub> | 74,052<br><sub>0.647 ms</sub> | **111,062<br><sub>0.271 ms</sub>** |
| GET | 105,197<br><sub>0.247 ms</sub> | 99,980<br><sub>0.439 ms</sub> | 90,893<br><sub>0.519 ms</sub> | 71,398<br><sub>0.639 ms</sub> | **111,037<br><sub>0.263 ms</sub>** |
| INCR | 105,241<br><sub>0.247 ms</sub> | 95,220<br><sub>0.447 ms</sub> | 86,941<br><sub>0.527 ms</sub> | 74,052<br><sub>0.647 ms</sub> | **111,062<br><sub>0.263 ms</sub>** |
| LPUSH | **105,175<br><sub>0.247 ms</sub>** | 99,980<br><sub>0.439 ms</sub> | 86,941<br><sub>0.535 ms</sub> | 68,937<br><sub>0.655 ms</sub> | 95,220<br><sub>0.447 ms</sub> |
| HSET | **105,219<br><sub>0.247 ms</sub>** | 95,220<br><sub>0.447 ms</sub> | 83,306<br><sub>0.543 ms</sub> | 74,063<br><sub>0.655 ms</sub> | 99,980<br><sub>0.391 ms</sub> |
| ZADD | 105,197<br><sub>0.239 ms</sub> | 86,926<br><sub>0.503 ms</sub> | 79,987<br><sub>0.575 ms</sub> | 74,052<br><sub>0.663 ms</sub> | **105,241<br><sub>0.423 ms</sub>** |

## Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | **1,142,204<br><sub>0.511 ms</sub>** | 727,008<br><sub>0.959 ms</sub> | 666,445<br><sub>1.111 ms</sub> | 727,008<br><sub>0.999 ms</sub> | 571,102<br><sub>1.311 ms</sub> |
| GET | **1,142,204<br><sub>0.551 ms</sub>** | 888,494<br><sub>0.727 ms</sub> | 888,494<br><sub>0.799 ms</sub> | 799,360<br><sub>0.895 ms</sub> | 666,222<br><sub>1.119 ms</sub> |
| INCR | **1,332,445<br><sub>0.503 ms</sub>** | 888,494<br><sub>0.775 ms</sub> | 888,494<br><sub>0.807 ms</sub> | 799,680<br><sub>0.911 ms</sub> | 615,006<br><sub>1.175 ms</sub> |
| LPUSH | **1,142,204<br><sub>0.495 ms</sub>** | 799,360<br><sub>0.783 ms</sub> | 799,680<br><sub>0.903 ms</sub> | 727,008<br><sub>1.015 ms</sub> | 319,846<br><sub>1.223 ms</sub> |
| HSET | **1,142,204<br><sub>0.599 ms</sub>** | 787,402<br><sub>0.927 ms</sub> | 727,008<br><sub>0.943 ms</sub> | 666,222<br><sub>1.079 ms</sub> | 499,875<br><sub>0.759 ms</sub> |
| ZADD | **571,265<br><sub>1.271 ms</sub>** | 443,853<br><sub>1.855 ms</sub> | 499,625<br><sub>1.559 ms</sub> | 420,698<br><sub>1.839 ms</sub> | 319,898<br><sub>1.215 ms</sub> |

## memtier: 50% SET / 50% GET, random keys

Median of the rounds. *Cores used* is the server's CPU time divided by wall time: a server stuck at ~1-2 cores while more are available is limited by a single thread.

### Pipeline 1

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | 105,603 | 0.447 | 0.703 | 2.431 | 1.67 | 63,235 |
| Redis | 101,801 | 0.479 | 0.799 | 1.375 | 1.00 | 101,801 |
| Redis (io-threads 2) | 87,495 | 0.575 | 0.815 | 1.887 | 1.20 | 72,913 |
| Valkey (io-threads 2) | 76,193 | 0.655 | 0.727 | 2.383 | 2.02 | 37,719 |
| Dragonfly | **108,663** | 0.407 | 0.695 | 1.711 | 1.95 | 55,725 |

### Pipeline 16

| Server | ops/s | p50 ms | p99 ms | p99.9 ms | Cores used | ops per core-second |
|---|---:|---:|---:|---:|---:|---:|
| BoltCache | **939,429** | 0.799 | 1.255 | 5.247 | 1.42 | 661,569 |
| Redis | 828,265 | 0.895 | 1.463 | 2.655 | 1.01 | 820,065 |
| Redis (io-threads 2) | 789,050 | 1.007 | 1.415 | 2.959 | 1.69 | 466,894 |
| Valkey (io-threads 2) | 808,323 | 0.967 | 1.647 | 3.711 | 2.01 | 402,151 |
| Dragonfly | 638,488 | 1.231 | 1.935 | 3.791 | 2.00 | 319,244 |

