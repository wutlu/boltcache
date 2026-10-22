# Benchmark results

- CPU: AMD EPYC 7763 64-Core Processor (4 logical CPUs), Linux 7.0.0-1017-azure
- Pinning: server_cpus=0,1 client_cpus=2,3 threads=2
- Load: clients=50 requests=500000 pipeline_requests=2000000 keyspace=100000 data_size=32 rounds=3
- BoltCache commit: 63613c8
- Images: boltcache:bench, redis:8.10.2, redis:8.10.2, valkey/valkey:9.2, docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2

Median requests/second (higher is better); p50 latency in ms below each value.

## Pipeline 1

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 105,219<br><sub>0.247 ms</sub> | 99,980<br><sub>0.439 ms</sub> | 83,319<br><sub>0.551 ms</sub> | 76,899<br><sub>0.647 ms</sub> | **111,062<br><sub>0.279 ms</sub>** |
| GET | 105,197<br><sub>0.239 ms</sub> | 99,980<br><sub>0.431 ms</sub> | 90,893<br><sub>0.519 ms</sub> | 76,911<br><sub>0.631 ms</sub> | **111,037<br><sub>0.263 ms</sub>** |
| INCR | 105,197<br><sub>0.239 ms</sub> | 99,980<br><sub>0.447 ms</sub> | 86,941<br><sub>0.527 ms</sub> | 76,911<br><sub>0.631 ms</sub> | **111,062<br><sub>0.271 ms</sub>** |
| LPUSH | **105,197<br><sub>0.247 ms</sub>** | 99,980<br><sub>0.439 ms</sub> | 86,941<br><sub>0.535 ms</sub> | 74,063<br><sub>0.639 ms</sub> | 95,220<br><sub>0.463 ms</sub> |
| HSET | 105,219<br><sub>0.239 ms</sub> | 95,220<br><sub>0.447 ms</sub> | 83,319<br><sub>0.543 ms</sub> | 74,063<br><sub>0.647 ms</sub> | **105,241<br><sub>0.399 ms</sub>** |
| ZADD | **99,980<br><sub>0.439 ms</sub>** | 86,941<br><sub>0.495 ms</sub> | 79,987<br><sub>0.575 ms</sub> | 74,063<br><sub>0.655 ms</sub> | **99,980<br><sub>0.439 ms</sub>** |

## Pipeline 16

| Test | BoltCache | Redis | Redis (io-threads 2) | Valkey (io-threads 2) | Dragonfly |
|---|---:|---:|---:|---:|---:|
| SET | 666,445<br><sub>0.951 ms</sub> | **799,360<br><sub>0.919 ms</sub>** | 727,008<br><sub>1.039 ms</sub> | 726,744<br><sub>0.991 ms</sub> | 570,939<br><sub>1.295 ms</sub> |
| GET | 727,008<br><sub>0.879 ms</sub> | **888,494<br><sub>0.703 ms</sub>** | **888,494<br><sub>0.775 ms</sub>** | 799,680<br><sub>0.887 ms</sub> | 666,222<br><sub>1.095 ms</sub> |
| INCR | 727,008<br><sub>0.879 ms</sub> | **888,494<br><sub>0.759 ms</sub>** | **888,494<br><sub>0.791 ms</sub>** | 799,680<br><sub>0.895 ms</sub> | 666,445<br><sub>1.135 ms</sub> |
| LPUSH | **799,680<br><sub>0.719 ms</sub>** | 799,360<br><sub>0.847 ms</sub> | **799,680<br><sub>0.895 ms</sub>** | 727,008<br><sub>0.999 ms</sub> | 319,898<br><sub>1.095 ms</sub> |
| HSET | **799,680<br><sub>0.863 ms</sub>** | 799,360<br><sub>0.919 ms</sub> | 799,360<br><sub>0.943 ms</sub> | 726,744<br><sub>1.055 ms</sub> | 533,191<br><sub>0.759 ms</sub> |
| ZADD | 185,977<br><sub>4.079 ms</sub> | 443,951<br><sub>1.855 ms</sub> | **499,625<br><sub>1.567 ms</sub>** | 444,247<br><sub>1.687 ms</sub> | 333,222<br><sub>0.919 ms</sub> |

