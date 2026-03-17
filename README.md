# Multi-Tenant Payment Ledger Service

A robust, highly concurrent, and strictly isolated payment ledger service built with Go and PostgreSQL. This service asynchronously processes payment transactions, maintains accurate account balances per tenant, and exposes a RESTful API.

## Architecture Overview

The application follows Clean Architecture principles, ensuring a strict separation of concerns between the HTTP handling, business logic, and data access layers. It is built using idiomatic Go practices, relying on the standard library's `context` and `net/http` (via `go-chi` for routing) to remain lightweight and dependency-free at the core.

### 1. Multi-Tenancy (Schema-per-Tenant)
To guarantee zero data leakage between merchants, the system employs a schema-per-tenant isolation strategy using PostgreSQL schemas. 
* A shared `public` schema holds global merchant metadata.
* Each merchant operates within its own dedicated schema (e.g., `tenant_<merchant_id>`) containing its transactions, ledger entries, and balance data.
* New tenants can be onboarded dynamically without code changes; registering a new merchant programmatically executes migrations to generate their schema and tables.

### 2. Concurrency & Data Integrity
Balance updates must be safe under concurrent access. When multiple background workers process transactions for the same merchant simultaneously, the service utilizes pessimistic database-level locking (`SELECT FOR UPDATE`) to prevent race conditions. 
* Every balance change creates an immutable ledger entry detailing the timestamp, transaction reference, previous balance, new balance, and the change amount.

### 3. Async Processing Pipeline
Transactions are not processed synchronously in the HTTP handler. 
* The API accepts the payload, pushes it to an internal queue (Buffered Channel), and immediately returns a `202 Accepted` status.
* A background worker pool picks up pending transactions from the queue. The number of workers is configurable via the `WORKER_COUNT` environment variable.
* **Graceful Shutdown:** On receiving SIGTERM/SIGINT signals, the application stops accepting new work, drains the queue, and finishes all in-flight transactions before exiting safely.

## Tech Stack
* **Language:** Go 1.24+
* **Database:** PostgreSQL (with `pgxpool` for optimal connection management)
* **Router:** `go-chi/chi` (100% standard library compatible)
* **Configuration:** `joho/godotenv`

## How to Run

The entire service, including the PostgreSQL database, is containerized.

1. Clone the repository.
2. Ensure Docker and Docker Compose are installed.
3. Run the following command:

```bash
docker-compose up --build -d