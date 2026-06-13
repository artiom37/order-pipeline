# Order Pipeline

A full-stack take-home exercise implementation for a food-delivery order pipeline.

## Architecture

- **Go backend** exposes APIs, SSE dashboard events, worker, dispatcher, simulator, and load generator commands.
- **PostgreSQL** is the source of truth for order state, idempotency, lifecycle events, and the transactional outbox.
- **Redis Streams** are used as the asynchronous worker queue with consumer groups and acknowledgements.
- **React + Vite + Tailwind** powers the live business dashboard.
- **Docker Compose** runs everything on one machine.

## Why this design

- PostgreSQL is the source of truth because losing or duplicating orders is more expensive than maximizing raw queue throughput.
- Redis Streams are used instead of Redis Pub/Sub because Streams support consumer groups, pending messages, and explicit acknowledgement.
- The outbox pattern avoids the failure gap between committing an order and publishing queue work.
- Workers use guarded state transitions (`WHERE id = ? AND status = ?`) so duplicated messages cannot advance an order twice.
- SSE is used for dashboard updates because this UI mostly needs server-to-browser live events.

## Prerequisites

- Docker Desktop
- Go 1.23+ locally if running commands outside Docker
- Node.js 20+ locally if running the frontend outside Docker

## Run everything

```bash
docker compose up --build
```

Open:

- Dashboard: http://localhost:5173
- API health: http://localhost:8080/health
- API readiness: http://localhost:8080/ready
- Metrics: http://localhost:8080/metrics

## Drive load

Small load:

```bash
curl -X POST http://localhost:8080/api/load/start \
  -H "Content-Type: application/json" \
  -d '{"orders_per_second":5,"duration_seconds":30}'
```

Dinner rush:

```bash
curl -X POST http://localhost:8080/api/load/rush
```

CLI load generator:

```bash
go run ./backend/cmd/loadgen --api http://localhost:8080 --rate 50 --duration 60s
```

## Trigger failures

Degrade restaurant:

```bash
curl -X POST http://localhost:8080/api/chaos/restaurant \
  -H "Content-Type: application/json" \
  -d '{"failure_rate":0.75,"min_delay_ms":500,"max_delay_ms":2500}'
```

Recover restaurant:

```bash
curl -X POST http://localhost:8080/api/chaos/restaurant \
  -H "Content-Type: application/json" \
  -d '{"failure_rate":0,"min_delay_ms":100,"max_delay_ms":700}'
```

Degrade courier:

```bash
curl -X POST http://localhost:8080/api/chaos/courier \
  -H "Content-Type: application/json" \
  -d '{"failure_rate":0.65,"min_delay_ms":500,"max_delay_ms":3000}'
```

Recover courier:

```bash
curl -X POST http://localhost:8080/api/chaos/courier \
  -H "Content-Type: application/json" \
  -d '{"failure_rate":0,"min_delay_ms":100,"max_delay_ms":800}'
```

## Kill and recover a worker

```bash
docker compose stop worker
# watch backlog grow in dashboard

docker compose start worker
# watch processing recover
```

## Useful dev commands

```bash
make up
make down
make logs
make test
make fmt
make rush
```

## Demo script

1. Start the stack: `docker compose up --build`.
2. Open the dashboard.
3. Click or call small load and watch normal lifecycle progress.
4. Trigger dinner rush and show backlog/in-flight metrics increase.
5. Degrade restaurant and show retries/stuck stages/failures become visible.
6. Recover restaurant and show orders resume.
7. Stop worker and show orders are not lost; start worker and show recovery.

## Current limitations and next improvements

- Redis Streams pending-message claiming is basic. A production version would add `XAUTOCLAIM` for messages abandoned by dead consumers.
- Retry scheduling is stored in Postgres for clarity. A larger system might use a dedicated delayed queue.
- Metrics are simple JSON-style counters. Production would expose Prometheus metrics and dashboards.
- Simulator is intentionally simple but configurable enough for a live demo.
