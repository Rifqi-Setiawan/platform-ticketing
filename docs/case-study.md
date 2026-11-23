# Selling the last ticket in Go

TribunPass models a tournament ticket office with general-admission inventory for each stand. The buyer can hold one ticket for five minutes. The difficult moment is the boundary between a successful reservation, a lost HTTP response, and a competing buyer. The demo exposes that boundary through live inventory, reservation history and a repeatable payment simulator.

[Watch the walkthrough](demo.mp4) · [High- and low-level design](design.md) · [OpenAPI](openapi.yaml)

## From a click to a database guarantee

Consider one ticket and two buyers. Both screens can legitimately report that one ticket is available. When they click reserve, PostgreSQL evaluates a conditional decrement on the same stock row. One transaction obtains it and commits the hold alongside the idempotency result. When the other obtains the row, the remaining stock is zero. This second buyer receives `409 SOLD_OUT`. If the winner loses their HTTP response, retrying through another API instance returns the original reservation. The buyer can verify its present status separately. [Reservation transaction](../internal/postgres/holds.go)

Payment delivery has two identities. The **event ID** identifies a provider message and makes duplicate delivery replayable. The **hold ID** identifies the purchase and has a unique ticket constraint. Even a second successful message with a different event ID cannot grant another ticket for the same hold. A late payment and the expiry worker both acquire the hold lock before making a decision. The winner of that lock reads the database clock and persists the business outcome, inventory change and transition together. [Payment and expiry transactions](../internal/postgres/payments.go)

## What was exercised

- **One hundred distinct buyers and ten tickets.** Two real HTTP servers with independent database pools received concurrent requests against a shared stand. Ten requests returned `201`, ninety returned `409 SOLD_OUT`. A single SQL snapshot verified the remaining stock and holds. [System tests](../tests/system)
- **Dropped response after commit.** A test transport discarded the first successful HTTP response. A client retry through the second server retrieved the original hold. Replaying payment returned the same ticket. A buyer from another sandbox could not read or pay that hold. [Lost-response test](../tests/system)
- **A request that starts on time can finish late.** A test holds the reservation row lock until after its deadline, then releases the already-waiting callback. PostgreSQL marks the hold expired and returns exactly one unit of inventory. Tests also inject a SQL failure after the stock update and verify that the transaction rolls back. [Database integration tests](../internal/postgres/store_test.go)
- **Taking a worker offline.** An overdue persisted hold remains recoverable when a new worker starts. Bounded batches and `SKIP LOCKED` keep two workers from owning the same transition. [Worker](../internal/expiry/worker.go) · [Worker tests](../internal/expiry/worker_test.go)

## Measured on 23 November 2026

The Go API, PostgreSQL 16.16 and k6 2.4.0 ran on the same shared Linux VPS with **4 vCPUs**, Intel Xeon Platinum 8370C, about 55 GiB host memory and ext4 storage. The Compose backend used Go 1.26.11 with 10 pool connections. Each closed-loop k6 virtual user sent 20 unique reservation requests. The success profile started with enough stock for every request. The contention profile started with ten tickets. Session creation and inventory seeding happened before measurement. All six runs finished with zero unexpected responses and a valid database stock invariant. [Machine-readable results](benchmark-results.json) · [Benchmark runner](../bench/run.sh)

| Profile | Concurrent clients | Requests | Created | Sold out | Requests/s | p50 | p95 |
|---|---:|---:|---:|---:|---:|---:|---:|
| Enough stock | 10 | 200 | 200 | 0 | 518 | 11.9 ms | 49.5 ms |
| Enough stock | 50 | 1,000 | 1,000 | 0 | 694 | 57.3 ms | 144.5 ms |
| Enough stock | 100 | 2,000 | 2,000 | 0 | 722 | 107.9 ms | 295.8 ms |
| Ten tickets | 10 | 200 | 10 | 190 | 1,492 | 5.7 ms | 11.3 ms |
| Ten tickets | 50 | 1,000 | 10 | 990 | 1,622 | 24.7 ms | 58.7 ms |
| Ten tickets | 100 | 2,000 | 10 | 1,990 | 1,543 | 48.4 ms | 133.0 ms |

A sold-out transaction records the idempotency decision and skips hold and timeline creation. Its higher request rate explains why the two profiles deserve separate numbers. These measurements characterize short, synthetic bursts against one stand, with the HTTP load generator colocated with the API. The closed-loop setup, warm host caches and shared CPU affect the absolute rates.

## Investigation — where the time went

At 100 clients with available stock, the pool accumulated **224 seconds of aggregate wait** over 2,000 requests. Many requests wait simultaneously, so this metric sums overlapping durations. The follow-up experiment repeated the same 2,000-request burst three times per pool size, alternating their order. Increasing the pool from 10 to 20 produced a median run throughput of **605 requests/s**, down from **687**. Median run p95 rose from **305 ms** to **348 ms**. Each run still issued exactly 2,000 holds. [Per-run counters and latency](benchmark-results.json)

To locate contention, a separate 10,000-reservation run sampled active connections approximately every 100 ms. **179 of 180 active samples** contained a PostgreSQL session waiting on a lock during the conditional stock update. At peak, nine connections waited there. That directly identifies the single popular stand as a serialization point. The pool remains at ten for this demo. Splitting stock into independently owned allocations would change enforcement of the capacity invariant and would need a new correctness test. [Sampling SQL](../bench/stock-locks.sql)

## Reproduce the investigation

From the repository root on a shell with Docker Compose, k6, curl and Python 3:

```sh
docker compose up -d --build --wait
bash bench/run.sh
DB_MAX_OPEN=20 docker compose up -d --no-deps --wait api
VUS_LIST=100 PROFILES=success bash bench/run.sh
DB_MAX_OPEN=10 docker compose up -d --no-deps --wait api
```

For lock sampling, run the following in one terminal and `VUS_LIST=100 ITERATIONS_PER_VU=100 PROFILES=success bash bench/run.sh` in another. Stop the sampler after the load finishes.

```sh
docker compose exec -T db psql -XqAt -U tribunpass -d tribunpass < bench/stock-locks.sql
```

The public ticket office gives each visitor a sandbox with synthetic inventory. Use the review lab to observe a payment replay or a race for the last ticket. Payments are simulated, and each guest session expires after 24 hours. [Try TribunPass](https://tribunpass.rifqisetiawan.my.id/) · [Portfolio case study](https://portfolio.rifqisetiawan.my.id/project/tribunpass-go-ticketing)
