# Benchmarks

redis-from-scratch measured against Redis 7.4 with the official `redis-benchmark`, under identical conditions.

## Summary

- Without pipelining, redis-from-scratch reaches **63–70% of Redis 7** throughput for SET and GET.
- With 16-command pipelining, **GET is on par with Redis 7** (573k vs 546k requests/sec), because reads run in parallel under a read lock across all cores.
- With 16-command pipelining, **SET reaches 46% of Redis 7**, because every write takes a global lock and makes its own `write()` call to the append-only file.
- With `appendfsync always`, redis-from-scratch is **23× slower** than Redis 7. It calls `fsync` once per command, while Redis groups the writes of many clients into a single `fsync`.

## Environment

| | |
|---|---|
| CPU | AMD Ryzen 7 4800H, 8 cores / 16 threads |
| Host | Windows 11, 7.4 GB RAM |
| Containers | Docker Engine 29.9.3 on Docker Desktop (WSL2, kernel 6.18), 16 CPUs and 3.5 GB RAM for the VM |
| redis-from-scratch | Go 1.27.2, image `redis-from-scratch` (9.89 MB, distroless) |
| Redis | 7.4.12 (`redis:7` image) |
| Client | `redis-benchmark` 7.4.12 |

Both servers and the benchmark client ran as containers on the same Docker network, so traffic never left the Docker VM. Neither server had CPU limits. Redis executes commands on a single thread; redis-from-scratch uses one goroutine per client and can use every core.

## Method

- 50 concurrent clients (`-c 50`), random keys from a keyspace of 100,000 (`-r 100000`), 3-byte values (the `redis-benchmark` default).
- Both servers had the append-only file turned on, so every write is persisted. Redis ran with `--appendonly yes --save ""`, which disables RDB snapshots so that only AOF is compared.
- Each number is the **median of 3 runs**. Each server started fresh in a new container with an empty data volume.

## Results

### Throughput (appendfsync everysec, 200,000 requests)

| Server | Command | Pipeline | Requests/sec | vs Redis 7 |
|---|---|---:|---:|---:|
| redis-from-scratch | SET | 1 | 36,717 | 63% |
| Redis 7 | SET | 1 | 57,921 | |
| redis-from-scratch | GET | 1 | 41,806 | 70% |
| Redis 7 | GET | 1 | 59,559 | |
| redis-from-scratch | SET | 16 | 192,678 | 46% |
| Redis 7 | SET | 16 | 418,410 | |
| redis-from-scratch | GET | 16 | **573,066** | **105%** |
| Redis 7 | GET | 16 | 546,448 | |

### SET throughput by appendfsync policy (20,000 requests, no pipelining)

| appendfsync | redis-from-scratch | Redis 7 | vs Redis 7 |
|---|---:|---:|---:|
| always | 498 | 11,521 | 4% |
| everysec | 37,807 | 48,662 | 78% |
| no | 39,062 | 53,476 | 73% |

## Analysis

**Reads scale with cores.** GET only takes the store's read lock, so many clients can read at the same time. When pipelining removes most of the network round-trip cost, this parallelism lets redis-from-scratch overtake single-threaded Redis on GET.

**Writes are serialized twice.** Every SET holds the router's `writeOrder` mutex, so that the AOF records writes in the same order the store applied them, and then the store's write lock. On top of that, every SET makes its own `write()` system call to the AOF. Redis instead collects the AOF output of a whole event-loop iteration in memory and writes it once. The SET result with pipelining (46%) shows this cost most clearly.

**`appendfsync always` exposes the missing group commit.** redis-from-scratch calls `fsync` inside the write lock for every command, so 50 clients wait in a single queue for the disk: about 500 writes/sec, or roughly 2 ms per `fsync`. Redis fsyncs once per event-loop iteration, and replies to every client in that batch only after the shared `fsync`. It gives the same guarantee (no acknowledged write is lost) at 23× the throughput.

**Without pipelining, the network dominates.** Every request is a full round trip through the Docker network, which hides most server-side differences. The 63–70% gap comes from per-request overhead: parsing allocations, goroutine scheduling, and lock handoffs.

## Possible improvements

- **Group commit for the AOF.** Buffer the encoded commands from all clients, have one goroutine `write` and `fsync` them in a batch, and reply to clients only after their batch is durable. This should close most of the gap for `always` and pipelined SET.
- **Sharded store.** Split the map into shards with their own locks, so writes to different keys do not wait for each other.
- **Fewer allocations in the parser.** Reading a SET currently costs 12 allocations (`BenchmarkReadCommand`). Reusing argument buffers would reduce work for the garbage collector.

## Reproduce

Requires Docker and Bash (Linux, macOS, or Git Bash on Windows):

```bash
bash scripts/benchmark.sh
```

Settings can be changed with environment variables:

```bash
RUNS=5 REQUESTS=500000 CLIENTS=100 bash scripts/benchmark.sh
```

The script builds the image, starts each server in turn on a private Docker network, and prints the tables above as Markdown. A full run takes about 4–5 minutes.
