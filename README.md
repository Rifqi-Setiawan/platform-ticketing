# TribunPass

A Go ticket reservation service for a fictional tournament final. Give two buyers one remaining ticket, deliver a payment twice, or pay after the deadline. The review lab shows the resulting stock and ticket IDs from PostgreSQL.

[![Ticket office preview](docs/demo-preview.jpg)](docs/demo.mp4)

[Watch the 90-second walkthrough](docs/demo.mp4) · [Engineering case study](docs/case-study.md) · [System design and decisions](docs/design.md) · [OpenAPI contract](docs/openapi.yaml)

## Run the ticket office

From this directory with Docker and the Compose plugin installed:

```sh
docker compose up -d --build --wait
```

Open **http://localhost:3000**. Reserve one Reguler or VIP ticket, simulate a payment, then refresh to see the same ticket. Open **Review lab** to run the last-ticket race, callback replay and late-payment scenarios. Each run creates its own inventory. The public-facing demo uses fictional payments. The first build downloads the base images and npm/Go dependencies.

The API listens on **http://localhost:8080**. Open `/health/ready` for database readiness and `/metrics` for pool pressure, expiry backlog and per-process result counters. Compose binds its published ports to loopback. `docker compose down` stops the application and keeps local reservations in the named volume. Guest sessions expire after 24 hours.

## Read the Go implementation

| Question | Starting point |
|---|---|
| Why can only ten buyers get ten tickets? | [Conditional stock update and transaction](internal/postgres/holds.go) |
| What if the winning response disappears? | [Two HTTP servers, distinct buyers and a lost response](tests/system) |
| Who wins when payment competes with expiry? | [Time rule](internal/booking/booking.go), [locking and finalization](internal/postgres/payments.go) |
| How does work resume after downtime? | [Expiry loop](internal/expiry/worker.go), [PostgreSQL integration tests](internal/postgres/store_test.go) |
| How is callback replay verified? | [Signed HTTP delivery](internal/simulator/simulator.go), [callback handler](internal/httpapi/payments.go) |
| What happens under load? | [Measurements and investigation](docs/case-study.md), [reproducible benchmark](bench/run.sh) |

## Run the full test suite

Go 1.26.0+ and a writable **test** PostgreSQL database are required for integration tests. The suite creates and removes its own test schemas. To start a disposable database on an unused port:

```sh
docker run -d --name tribunpass-testdb -p 127.0.0.1:55432:5432 \
  -e POSTGRES_USER=test -e POSTGRES_PASSWORD=test -e POSTGRES_DB=tribunpass postgres:16-alpine
# Wait until this reports "accepting connections".
docker exec tribunpass-testdb pg_isready -U test -d tribunpass
export TEST_DATABASE_URL='postgres://test:test@127.0.0.1:55432/tribunpass?sslmode=disable'
go vet ./...
go test -race -count=1 ./...
```

On PowerShell, set the same URL with `$env:TEST_DATABASE_URL = '…'`. The race detector requires a supported Go platform with cgo and a C compiler. Omitting the test DSN skips database-dependent tests. CI provides PostgreSQL, runs the full race suite, and builds the Next.js frontend. The benchmark needs Bash, curl, Python 3, k6 and the running Compose stack:

```sh
bash bench/run.sh
```

Each run creates a fresh sandbox, sizes its section to the selected profile, checks the actual response counts and verifies the final inventory equation with a database query. Machine-readable results go under `bench/results/`.

## Configuration

| Setting | Default | Purpose |
|---|---|---|
| `WEB_PORT`, `API_PORT` | `3000`, `8080` | Compose host ports. |
| `DB_MAX_OPEN` | `10` | Database connections per Go process. |
| `POSTGRES_PASSWORD` | local Compose demo value | Password used for the private demo database. Use a URL-safe value or supply a correctly escaped `DATABASE_URL` when running the binary directly. |
| `SECURE_COOKIES` | `false` | Enable for an HTTPS deployment. |
| `SIMULATOR_SECRET` | generated at process startup | For callbacks routed across instances, set a shared secret of at least 32 bytes. The default single-process simulator delivers to itself. |
| `DATABASE_URL`, `PORT`, `BIND_HOST` | DSN required outside Compose, port `8080`, all interfaces | Direct Go binary settings. Specify `BIND_HOST=127.0.0.1` for loopback-only development. |
| `CALLBACK_URL` | local API callback on the configured port | Full internal URL for simulator delivery. |
| `API_ORIGIN` | local API in Next.js development, Compose API in Docker builds | Same-origin proxy upstream, resolved when Next.js builds its routes. |

All database migrations are embedded in the binary. Application startup serializes migrations with a PostgreSQL advisory lock. The frontend includes a locally bundled font and its license.
