#!/usr/bin/env bash
# Reproducible benchmark: BoltCache vs Redis, Valkey and Dragonfly.
#
# Every server runs in Docker with the same CPU budget (SERVER_CPUS), and
# redis-benchmark runs pinned to separate cores (CLIENT_CPUS) so client and
# server don't compete. Persistence is disabled everywhere. Each scenario is
# run ROUNDS times and the median is reported.
#
#   bench/run.sh                 # needs Docker and Python 3; Linux recommended
#   SERVER_CPUS=0-3 CLIENT_CPUS=4-7 bench/run.sh
#
# Results go to bench/results/<timestamp>/ (raw CSV + report.md).
set -euo pipefail

SERVER_CPUS="${SERVER_CPUS:-0,1}"
CLIENT_CPUS="${CLIENT_CPUS:-2,3}"
ROUNDS="${ROUNDS:-3}"
CLIENTS="${CLIENTS:-50}"
REQUESTS="${REQUESTS:-500000}"
PIPELINE_REQUESTS="${PIPELINE_REQUESTS:-2000000}"
TESTS="${TESTS:-set,get,incr,lpush,hset,zadd}"
KEYSPACE="${KEYSPACE:-100000}"
DATA_SIZE="${DATA_SIZE:-32}"

REDIS_IMAGE="${REDIS_IMAGE:-redis:8.10.2}"
VALKEY_IMAGE="${VALKEY_IMAGE:-valkey/valkey:9.2}"
DRAGONFLY_IMAGE="${DRAGONFLY_IMAGE:-docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.2}"
BOLTCACHE_IMAGE="${BOLTCACHE_IMAGE:-boltcache:bench}"
MEMTIER_IMAGE="${MEMTIER_IMAGE:-redislabs/memtier_benchmark:2.1.4}"
MEMTIER_SECONDS="${MEMTIER_SECONDS:-15}"

cpu_count() { # "0,1" -> 2, "0-3" -> 4
  python3 -c "import sys
n=0
for p in sys.argv[1].split(','):
    a,_,b=p.partition('-'); n+= (int(b)-int(a)+1) if b else 1
print(n)" "$1"
}
THREADS=$(cpu_count "$SERVER_CPUS")
CLIENT_THREADS=$(cpu_count "$CLIENT_CPUS")
PORT=7700
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/bench/results/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUT"

log() { echo "==> $*" >&2; }

if [[ "$BOLTCACHE_IMAGE" == "boltcache:bench" ]]; then
  log "building BoltCache image from $ROOT"
  docker build -q -t boltcache:bench "$ROOT" >/dev/null
fi
for img in "$REDIS_IMAGE" "$VALKEY_IMAGE" "$DRAGONFLY_IMAGE" "$MEMTIER_IMAGE"; do docker pull -q "$img" >/dev/null; done

# name|image|command (the port is $PORT)
SERVERS=(
  "BoltCache|$BOLTCACHE_IMAGE|server"
  "Redis|$REDIS_IMAGE|redis-server --port $PORT --save '' --appendonly no --protected-mode no"
  "Redis (io-threads $THREADS)|$REDIS_IMAGE|redis-server --port $PORT --save '' --appendonly no --protected-mode no --io-threads $THREADS"
  "Valkey (io-threads $THREADS)|$VALKEY_IMAGE|valkey-server --port $PORT --save '' --appendonly no --protected-mode no --io-threads $THREADS"
  "Dragonfly|$DRAGONFLY_IMAGE|dragonfly --port $PORT --proactor_threads $THREADS --dbfilename= --logtostderr"
)

start_server() {
  local image=$1 cmd=$2
  if [[ "$cmd" == "server" ]]; then
    # BoltCache: RESP listener on $PORT, nothing else, no persistence.
    cat > "$OUT/boltcache.yaml" <<YAML
server:
  mode: rest
  rest: {host: "127.0.0.1", port: $((PORT + 1))}
  resp: {host: "127.0.0.1", port: $PORT}
persistence: {enabled: false}
logging: {level: warn}
YAML
    docker run -d --rm --name bench-server --network host --cpuset-cpus "$SERVER_CPUS" \
      -v "$OUT/boltcache.yaml:/etc/boltcache/config.yaml:ro" "$image" >/dev/null
  else
    # shellcheck disable=SC2086
    docker run -d --rm --name bench-server --network host --cpuset-cpus "$SERVER_CPUS" \
      --ulimit memlock=-1 "$image" sh -c "exec $cmd" >/dev/null
  fi
  for _ in $(seq 1 100); do
    if docker run --rm --network host "$REDIS_IMAGE" redis-cli -p "$PORT" PING 2>/dev/null | grep -q PONG; then
      return 0
    fi
    sleep 0.2
  done
  docker logs bench-server >&2 || true
  echo "server did not start" >&2
  exit 1
}

stop_server() { docker rm -f bench-server >/dev/null 2>&1 || true; }
trap stop_server EXIT

bench() { # $1 = pipeline, $2 = requests
  docker run --rm --network host --cpuset-cpus "$CLIENT_CPUS" "$REDIS_IMAGE" \
    redis-benchmark -p "$PORT" -t "$TESTS" -c "$CLIENTS" -n "$2" -P "$1" \
      -r "$KEYSPACE" -d "$DATA_SIZE" --threads "$CLIENT_THREADS" --csv
}

# memtier: 50% SET / 50% GET on random keys, also recording how many CPU
# cores the server actually used (from its cgroup), which shows whether it
# is saturated or limited by a single thread.
memtier() { # $1 = pipeline; prints "ops p50 p99 p999 cores"
  local stats line
  # Sample the server's CPU usage once per second from outside the
  # container (images like distroless have no shell to exec into).
  stats=$(mktemp)
  docker stats bench-server --format '{{.CPUPerc}}' >"$stats" 2>/dev/null &
  local spid=$!
  line=$(docker run --rm --network host --cpuset-cpus "$CLIENT_CPUS" "$MEMTIER_IMAGE" \
    -s 127.0.0.1 -p "$PORT" -t "$CLIENT_THREADS" -c "$(( (CLIENTS + CLIENT_THREADS - 1) / CLIENT_THREADS ))" \
    --pipeline "$1" --ratio 1:1 --key-maximum "$KEYSPACE" --key-pattern R:R -d "$DATA_SIZE" \
    --test-time "$MEMTIER_SECONDS" --hide-histogram 2>/dev/null | awk '/^Totals/{print $2, $6, $7, $8}')
  kill "$spid" 2>/dev/null || true
  wait "$spid" 2>/dev/null || true
  # Median of the samples, skipping the first two (ramp-up); 100% = 1 core.
  echo "$line $(tr -cd '0-9.%\n' <"$stats" | python3 -c "
import sys, statistics
v = [float(l.strip().rstrip('%')) for l in sys.stdin if l.strip().rstrip('%')]
v = v[2:] or v or [0]
print(round(statistics.median(v) / 100, 2))")"
  rm -f "$stats"
}

for entry in "${SERVERS[@]}"; do
  IFS='|' read -r name image cmd <<<"$entry"
  slug=$(echo "$name" | tr 'A-Z ()' 'a-z_' | tr -cd 'a-z0-9_-')
  for p in 1 16; do
    reqs=$REQUESTS
    [[ $p -gt 1 ]] && reqs=$PIPELINE_REQUESTS
    for r in $(seq 1 "$ROUNDS"); do
      log "$name, pipeline $p, round $r/$ROUNDS"
      start_server "$image" "$cmd"
      bench "$p" "$reqs" >"$OUT/${slug}_p${p}_r${r}.csv"
      memtier "$p" >"$OUT/${slug}_memtier_p${p}_r${r}.txt"
      stop_server
    done
  done
  echo "$name|$image" >>"$OUT/servers.txt"
done

{
  echo "cpu_model=$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || sysctl -n machdep.cpu.brand_string)"
  echo "cpus=$(nproc 2>/dev/null || sysctl -n hw.ncpu)"
  echo "kernel=$(uname -sr)"
  echo "server_cpus=$SERVER_CPUS client_cpus=$CLIENT_CPUS threads=$THREADS"
  echo "clients=$CLIENTS requests=$REQUESTS pipeline_requests=$PIPELINE_REQUESTS keyspace=$KEYSPACE data_size=$DATA_SIZE rounds=$ROUNDS"
  echo "boltcache_commit=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
} >"$OUT/env.txt"

python3 "$ROOT/bench/report.py" "$OUT" | tee "$OUT/report.md"
