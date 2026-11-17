# Benchmarks

redis-from-scratch measured against Redis 7.4 with the official `redis-benchmark`, under identical conditions.

## Summary

- With 16-command pipelining, redis-from-scratch reaches **89% of Redis 7 for SET** and **92% for GET**.
- With `appendfsync always`, which acknowledges a write only after it is on disk, it reaches **91% of Redis 7**. Group commit made this **22× faster** than the first version, which called `fsync` once per command.
- Without pipelining, it reaches **65–67% of Redis 7**. Here every request pays a full network round trip plus per-request overhead in the server.

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
| redis-from-scratch | SET | 1 | 38,358 | 65% |
| Redis 7 | SET | 1 | 59,242 | |
| redis-from-scratch | GET | 1 | 40,136 | 67% |
| Redis 7 | GET | 1 | 59,970 | |
| redis-from-scratch | SET | 16 | 371,747 | 89% |
| Redis 7 | SET | 16 | 419,287 | |
| redis-from-scratch | GET | 16 | 552,486 | 92% |
| Redis 7 | GET | 16 | 598,802 | |

### SET throughput by appendfsync policy (20,000 requests, no pipelining)

| appendfsync | redis-from-scratch | Redis 7 | vs Redis 7 |
|---|---:|---:|---:|
| always | 11,068 | 12,107 | 91% |
| everysec | 40,816 | 59,347 | 69% |
| no | 33,168 | 40,241 | 82% |

### Before and after group commit

The first version wrote and fsynced every command on its own, inside the global write lock. Both versions were measured under the same conditions: Redis 7, used as the reference, scored 418,410 and 419,287 SET requests/sec with pipelining in the two runs.

| Scenario | Before | After | Change | vs Redis 7 before → after |
|---|---:|---:|---:|---:|
| SET, appendfsync always | 498 | 11,068 | **22×** | 4% → 91% |
| SET, pipeline 16 | 192,678 | 371,747 | **1.9×** | 46% → 89% |
| SET, pipeline 1 | 36,717 | 38,358 | unchanged | 63% → 65% |
| GET, pipeline 16 | 573,066 | 552,486 | unchanged | 105% → 92% |

GET was not touched by this change. Its shift from 105% to 92% of Redis is run-to-run noise, described below.

## Analysis

**Group commit removes disk I/O from the critical path.** A write command now only encodes itself into an in-memory buffer while it holds the write lock, and receives a sequence number. A single background goroutine takes everything in the buffer, writes it with one `write()` call, fsyncs it if the policy is `always`, and marks that sequence range as durable. While one batch is on its way to disk, the next one is already filling up. In a unit test, 1,000 durable writes from 50 concurrent clients needed only 40 fsyncs.

**Replies wait for durability, per connection.** Each connection has a session that remembers the sequence number of its latest write. The connection's output stream is wrapped in a gate that waits until that sequence is durable before any byte reaches the socket, whether through an explicit flush or an automatic flush of a full buffer. Sixteen pipelined SETs therefore wait once, not sixteen times, and no client ever sees `OK` for a write that is not yet persisted under the configured policy. Redis follows the same rule: it writes the AOF, and fsyncs it under `always`, before sending the replies of an event-loop iteration.

**Reads scale with cores.** GET only takes the store's read lock, so many clients can read at the same time. With pipelining, this keeps GET close to single-threaded Redis.

**Without pipelining, per-request overhead dominates.** Every request is a full round trip through the Docker network. A SET also hands off to the flusher goroutine and waits for it, which adds a context switch, so group commit does not help single SETs much. The remaining gap comes from parsing allocations, goroutine scheduling, and lock handoffs.

## Run-to-run variation

The benchmark ran on a laptop, and its performance varied between runs. In one run after the group commit change, Redis itself dropped to 268,817 SET and 310,078 GET requests/sec with pipelining, 35–48% below its usual results, which points to the host throttling. The ratios in that run still matched the conclusions above: SET with pipelining at 87% of Redis, `appendfsync always` at 87%, and GET with pipelining at 106%. The results reported here come from the next run, in which Redis scored within 1% of its numbers from before the change.

Only numbers measured in the same run should be compared. For example, `appendfsync no` coming out slower than `everysec` for both servers is within this noise.

## Possible improvements

- **Sharded store.** Split the map into shards with their own locks, so writes to different keys do not wait for each other.
- **Fewer allocations in the parser.** Reading a SET currently costs 12 allocations (`BenchmarkReadCommand`). Reusing argument buffers would reduce work for the garbage collector.
- **Skip the flusher handoff for `everysec` and `no`.** Under these policies a reply only has to wait until the data reaches the operating system, so a connection could write its own batch instead of waiting for another goroutine to do it.

## Reproduce

Requires Docker and Bash (Linux, macOS, or Git Bash on Windows):

```bash
bash scripts/benchmark.sh
```

Settings can be changed with environment variables:

```bash
RUNS=5 REQUESTS=500000 CLIENTS=100 bash scripts/benchmark.sh
```

The script builds the image, starts each server in turn on a private Docker network, and prints the tables above as Markdown. A full run takes about 3–5 minutes.
