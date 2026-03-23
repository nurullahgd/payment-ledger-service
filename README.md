# Payment Ledger Service

A multi-tenant payment ledger service built in Go. Each merchant operates in an isolated PostgreSQL schema. Transactions are processed asynchronously via a worker pool, with webhook notifications delivered on terminal states.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    HTTP Server (chi)                 │
│                                                     │
│  TenantMiddleware (X-API-Key → Merchant)            │
│  RateLimitMiddleware (sliding window, Redis)        │
│                                                     │
│  POST /api/v1/transactions                          │
│  GET  /api/v1/transactions                          │
│  GET  /api/v1/transactions/{id}                     │
│  GET  /api/v1/balance                               │
│  GET  /api/v1/ledger                                │
│  GET  /health                                       │
└──────────────┬──────────────────────────────────────┘
               │
       ┌───────▼────────┐        ┌──────────────────┐
       │  LedgerService │        │  IdempotencyRepo │
       │  (business     │        │  (Redis SET NX)  │
       │   logic)       │        └──────────────────┘
       └───────┬────────┘
               │
       ┌───────▼────────┐
       │  Worker Pool   │──── ProcessTransaction ────► PostgreSQL
       │  (N goroutines)│                             (schema-per-tenant)
       └───────┬────────┘
               │
       ┌───────▼────────┐
       │ WebhookNotifier│──── HTTP POST + retry ────► Merchant endpoint
       └────────────────┘
```

### Key Design Decisions

**Schema-per-tenant isolation**

Each merchant gets a dedicated PostgreSQL schema (`tenant_{merchant_id}`), containing its own `transactions`, `balances`, and `ledger` tables. This provides complete data isolation at the SQL level — a query in one tenant's schema cannot access another tenant's data, unlike row-level security which requires careful filter management.

**SELECT FOR UPDATE for balance consistency**

When processing a transaction, the worker acquires a row-level lock on the `balances` row before reading the balance:

```sql
SELECT available_balance FROM tenant_x.balances WHERE merchant_id = $1 FOR UPDATE
```

This ensures two concurrent debits cannot both read the same balance and both succeed — the second waits for the first to commit before reading the updated value. Proven by `TestProcessTransaction_ParallelDebit_NoOverdraft`.

**Sliding window rate limiting**

Uses a Redis sorted set per merchant. Each request adds a timestamped entry, old entries outside the window are removed, and the count is compared against the limit — all in a single pipeline. Unlike a fixed window (which allows burst at window boundary), the sliding window enforces a consistent rate across any 60-second span.

**Idempotency via Redis**

Before inserting a transaction, the handler checks a Redis key `idempotency:{merchantID}:{reference}`. If it exists, the cached response is returned with `Idempotency-Replayed: true`. If not, the transaction is inserted, queued, and the key is set with 24h TTL. The `Get` and `Set` are intentionally separate: the key is only set after the DB insert succeeds, so the transaction ID is available to cache.

**Asynchronous processing with graceful shutdown**

Transactions are accepted immediately (HTTP 202), inserted as `pending`, then queued to the worker pool. On `SIGTERM`, the HTTP server stops accepting new requests, the task channel is closed, and the pool waits for all in-flight workers to finish before the process exits.

**Webhook delivery with retry**

After a transaction reaches a terminal state (`completed` or `failed`), the worker fires an HTTP POST to the merchant's `webhook_url` with exponential backoff (1s → 2s → 4s, max 3 attempts). Webhook failure does not affect transaction state.

## Project Structure

```
.
├── cmd/server/           # Entry point
├── internal/
│   ├── config/           # Environment variable loading
│   ├── domain/           # Merchant, Transaction, LedgerEntry models
│   ├── handler/          # HTTP handlers and routing
│   ├── middleware/        # TenantMiddleware, RateLimitMiddleware
│   ├── ratelimit/         # SlidingWindowLimiter (Redis sorted sets)
│   ├── repository/        # PostgreSQL and Redis implementations
│   ├── service/           # Business logic layer
│   └── webhook/           # HTTPNotifier with retry
└── pkg/worker/            # Worker pool with graceful shutdown
```

## Running Locally

**Prerequisites:** Docker, Docker Compose

```bash
cp .env.example .env
docker-compose up --build -d
```

The service starts on `http://localhost:8080`. Two test merchants are seeded automatically:

| Merchant | API Key | Currency |
|---|---|---|
| Merchant 1 | `sk_test_12345` | USD |
| Merchant 2 | `sk_test_67890` | EUR |

## API Reference

All endpoints (except `/health`) require the `X-API-Key` header.

### Submit Transaction

```
POST /api/v1/transactions
X-API-Key: sk_test_12345
Content-Type: application/json

{
  "reference": "order-001",
  "type": "credit",
  "amount": 1500,
  "description": "Payment received"
}
```

Response `202 Accepted`:
```json
{ "id": "uuid", "status": "pending", "reference": "order-001" }
```

Submitting the same `reference` again returns the original response with `Idempotency-Replayed: true`.

### Get Balance

```
GET /api/v1/balance
X-API-Key: sk_test_12345
```

### List Transactions

```
GET /api/v1/transactions?status=completed&page=1&limit=20
```

`status` filter: `pending` | `completed` | `failed`

### Get Transaction

```
GET /api/v1/transactions/{id}
```

### List Ledger Entries

```
GET /api/v1/ledger?page=1&limit=20
```

### Health Check

```
GET /health
```

```json
{ "db": "ok", "cache": "ok" }
```

Returns `503` if either dependency is unavailable.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://ledger_user:ledger_password@localhost:5433/ledger_db?sslmode=disable` | PostgreSQL connection string |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `PORT` | `8080` | HTTP listen port |
| `WORKER_COUNT` | `5` | Number of background worker goroutines |
| `RATE_LIMIT_PER_MINUTE` | `60` | Max requests per merchant per 60 seconds |

## Testing

```bash
# Unit tests
go test ./...

# With race detector
go test -race ./...

# Integration tests (requires running PostgreSQL)
DATABASE_URL=postgres://... go test ./internal/repository/... -v -run Integration
```

Integration tests are skipped automatically when `DATABASE_URL` is not set.

## CI/CD

GitHub Actions runs three parallel jobs on every push:

- **lint** — `golangci-lint` (errcheck, staticcheck, govet, gofmt, unused)
- **test** — `go test -race -coverprofile=coverage.out ./...` with PostgreSQL and Redis services
- **build** — Docker multi-stage image build

## Design Tradeoffs

| Decision | Alternative | Reason |
|---|---|---|
| Schema-per-tenant | Row-level security | Complete SQL-level isolation, no risk of missing a WHERE clause |
| Sliding window rate limit | Token bucket | No burst at window boundary; counts real requests in rolling window |
| Redis idempotency (Get + Set) | Single atomic SETNX | Allows caching the real transaction ID after DB insert |
| Worker pool queue (channel) | External queue (e.g. Kafka) | Simpler operationally; acceptable for moderate throughput |
| Async processing (202) | Sync processing (200/201) | Decouples HTTP latency from DB transaction time |

## Future Improvements

- Persistent job queue (Kafka / SQS) for webhook delivery across restarts
- Webhook delivery log table for audit and replay
- Prometheus metrics (queue depth, processing latency, webhook success rate)
- Pagination cursor (keyset) instead of offset for large datasets
- OpenAPI / Swagger documentation
