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
for img in "$REDIS_IMAGE" "$VALKEY_IMAGE" "$DRAGONFLY_IMAGE"; do docker pull -q "$img" >/dev/null; done

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
