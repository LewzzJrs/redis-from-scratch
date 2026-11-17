#!/usr/bin/env bash
set -euo pipefail
export MSYS_NO_PATHCONV=1

REQUESTS="${REQUESTS:-200000}"
DURABLE_REQUESTS="${DURABLE_REQUESTS:-20000}"
CLIENTS="${CLIENTS:-50}"
KEYSPACE="${KEYSPACE:-100000}"
RUNS="${RUNS:-3}"

NETWORK="redis-benchmark-net"
SERVER="redis-benchmark-server"
OUR_IMAGE="redis-from-scratch:benchmark"
REDIS_IMAGE="redis:7"

cd "$(dirname "$0")/.."

cleanup() {
  docker rm -fv "$SERVER" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
trap cleanup EXIT

start_server() {
  docker rm -fv "$SERVER" >/dev/null 2>&1 || true
  docker run -d --name "$SERVER" --network "$NETWORK" "$@" >/dev/null

  for _ in $(seq 1 30); do
    if [ "$(docker run --rm --network "$NETWORK" "$REDIS_IMAGE" redis-cli -h "$SERVER" PING 2>/dev/null)" = "PONG" ]; then
      return 0
    fi
    sleep 1
  done

  echo "server did not answer PING in time" >&2
  docker logs "$SERVER" >&2
  exit 1
}

measure_once() {
  local test_name="$1" requests="$2" pipeline="$3"
  docker run --rm --network "$NETWORK" "$REDIS_IMAGE" \
    redis-benchmark -h "$SERVER" -t "$test_name" -n "$requests" -c "$CLIENTS" -r "$KEYSPACE" -P "$pipeline" --csv 2>/dev/null \
    | awk -F'","' 'NR > 1 { gsub(/"/, "", $2); printf "%.0f\n", $2 }'
}

median_requests_per_second() {
  local test_name="$1" requests="$2" pipeline="$3"
  for _ in $(seq 1 "$RUNS"); do
    measure_once "$test_name" "$requests" "$pipeline"
  done | sort -n | awk '{ values[NR] = $1 } END { print values[int((NR + 1) / 2)] }'
}

with_thousands_separator() {
  echo "$1" | sed -e ':again' -e 's/\([0-9]\)\([0-9]\{3\}\)\($\|,\)/\1,\2\3/' -e 't again'
}

throughput_rows() {
  local server_label="$1"
  shift
  start_server "$@"

  for pipeline in 1 16; do
    for test_name in set get; do
      local result
      result="$(median_requests_per_second "$test_name" "$REQUESTS" "$pipeline")"
      printf '| %s | %s | %s | %s |\n' "$server_label" "${test_name^^}" "$pipeline" "$(with_thousands_separator "$result")"
    done
  done
}

fsync_row() {
  local policy="$1"
  local ours theirs

  start_server "$OUR_IMAGE" -port 6379 -aof /data/appendonly.aof -appendfsync "$policy"
  ours="$(median_requests_per_second set "$DURABLE_REQUESTS" 1)"

  start_server "$REDIS_IMAGE" redis-server --appendonly yes --appendfsync "$policy" --save ""
  theirs="$(median_requests_per_second set "$DURABLE_REQUESTS" 1)"

  printf '| %s | %s | %s |\n' "$policy" "$(with_thousands_separator "$ours")" "$(with_thousands_separator "$theirs")"
}

echo "Building $OUR_IMAGE and pulling $REDIS_IMAGE..." >&2
docker build -q -t "$OUR_IMAGE" . >/dev/null
docker pull -q "$REDIS_IMAGE" >/dev/null
docker network create "$NETWORK" >/dev/null 2>&1 || true

echo "### Throughput (AOF on, appendfsync everysec, $CLIENTS clients, $REQUESTS requests, median of $RUNS runs)"
echo
echo "| Server | Command | Pipeline | Requests/sec |"
echo "|---|---|---:|---:|"
throughput_rows "redis-from-scratch" "$OUR_IMAGE"
throughput_rows "Redis 7" "$REDIS_IMAGE" redis-server --appendonly yes --appendfsync everysec --save ""

echo
echo "### SET throughput by appendfsync policy ($CLIENTS clients, $DURABLE_REQUESTS requests, no pipelining, median of $RUNS runs)"
echo
echo "| appendfsync | redis-from-scratch | Redis 7 |"
echo "|---|---:|---:|"
for policy in always everysec no; do
  fsync_row "$policy"
done
