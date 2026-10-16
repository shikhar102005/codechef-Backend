# Concurrency-Safe Seat Reservation & Booking System

A production-grade, distributed seat reservation and booking system built with **Go**, **PostgreSQL (Supabase)**, **Redis**, **RabbitMQ**, and **React (Vite + TypeScript + Tailwind CSS)**.

Designed to eliminate double bookings, race conditions, and overselling during high-concurrency ticket release surges.

---

## 1. System Architecture

```
                       +-------------------------------+
                       |      Nginx Reverse Proxy      |
                       |       (Port 80 / Docker)      |
                       +---------------+---------------+
                                       |
                +----------------------+----------------------+
                |                                             |
                v                                             v
     +---------------------+                       +--------------------+
     |  React SPA (Vite)   |                       |    Go HTTP API     |
     |  (TypeScript/Tail)  |                       |     (Gin / pgx)    |
     +---------------------+                       +---------+----------+
                                                             |
            +--------------------+---------------------------+--------------------+
            |                    |                           |                    |
            v                    v                           v                    v
   +-----------------+   +---------------+          +------------------+  +---------------+
   |   PostgreSQL    |   |  Redis Cache  |          | RabbitMQ Queues  |  |   Go Worker   |
   | (Authoritative) |   |  & Rate Limit |          | & Publisher Conf |  | (Outbox/Recon)|
   +-----------------+   +---------------+          +------------------+  +---------------+
```

### Core Architectural Guarantees
- **Authoritative Database Source of Truth**: PostgreSQL (Supabase) enforces all seat states and holds. Client-side state and caches are never trusted for booking mutations.
- **Zero Double-Bookings**: Guaranteed through atomic row-level locks (`SELECT ... FOR UPDATE`), transaction rollback on any seat unavailability, and physical database primary key constraints on `confirmed_seat_assignments(seat_id)` and `active_seat_holds(seat_id)`.
- **Deadlock-Free Contention**: All requested seat IDs are sorted in strict ascending order before lock acquisition, eliminating cyclic wait deadlocks.
- **Temporary Seat Holds (5-Minute TTL)**: Automatic expiration backed by database timestamps and background reconciliation.
- **Transactional Outbox Pattern**: Asynchronous messages are written into the database within the exact same transaction as the booking mutation, preventing lost events.
- **Idempotency & Safe Retries**: Network retries using `Idempotency-Key` return identical cached results with `X-Cache-Lookup: HIT-IDEMPOTENCY`.
- **Graceful Degradation**: Operates with integrated in-memory cache and local bus if Redis or RabbitMQ are offline.

---

## 2. Repository Structure

```
Codechef-Backend/
├── cmd/
│   ├── api/main.go               # Go REST API entrypoint
│   └── worker/main.go            # Background outbox & reconciliation worker
├── internal/
│   ├── auth/auth.go              # JWT authentication & bcrypt password hashing
│   ├── cache/redis.go            # Redis caching & in-memory sliding-window fallback
│   ├── config/config.go          # Environment configuration layer
│   ├── database/
│   │   ├── factory.go            # Database engine initialization (PostgreSQL / In-Memory)
│   │   ├── memory.go             # In-memory repository with simulated row mutexes
│   │   └── postgres.go           # PostgreSQL repository with pgx connection pooling
│   ├── domain/
│   │   ├── models.go             # Domain entities and status types
│   │   └── repository.go         # Repository interfaces & domain errors
│   ├── http/
│   │   ├── handlers.go           # REST API handlers, ownership checks, payment simulator
│   │   └── router.go             # Gin engine, routes, OpenAPI Swagger setup
│   ├── messaging/rabbitmq.go     # RabbitMQ connection, publisher confirms, in-mem fallback
│   ├── middleware/
│   │   ├── admission.go          # Capacity limit & FIFO wait queue admission control
│   │   ├── auth.go               # JWT verification & context extraction
│   │   ├── idempotency.go        # SHA-256 request fingerprinting & replay cache
│   │   ├── logging.go            # Structured slog HTTP logger with trace IDs
│   │   └── ratelimit.go          # Sliding-window rate limiter
│   └── outbox/processor.go       # Transactional outbox event processor
├── migrations/
│   ├── 001_initial_schema.sql    # Schema, foreign keys, unique & check constraints
│   └── 002_seed_data.sql         # Seed events, sessions, seats, demo users
├── frontend/                     # React + TypeScript + Vite demo application
│   ├── src/
│   │   ├── api/client.ts         # Typed API client
│   │   ├── types/api.ts          # TypeScript domain models
│   │   ├── App.tsx               # Full single-page ticket booking & dashboard application
│   │   └── index.css             # Tailwind CSS design system
│   ├── Dockerfile
│   └── nginx.conf
├── nginx/
│   └── nginx.conf                # Root Nginx reverse proxy configuration
├── tests/
│   ├── api_test.go               # REST API integration & idempotency tests
│   ├── concurrency_test.go       # 10 priority failure & race-condition test suite
│   └── load/                     # Runnable k6 load test scripts
├── docs/
│   ├── erd.md                    # Mermaid ERD & constraint documentation
│   ├── booking-workflows.md      # Mermaid sequence diagrams for all 7 workflows
│   ├── api.md                    # REST API documentation & curl examples
│   ├── concurrency-and-scaling.md# Concurrency control & scaling guide
│   └── load-testing.md           # Benchmark results & k6 guide
├── docker-compose.yml            # Docker orchestration
├── Dockerfile                    # Backend multi-stage container
├── .env.example                  # Environment template
└── Makefile                      # Build & test task runner
```

---

## 3. Quickstart: One-Command Master Launcher

You can start **everything** (Go Backend API, Background Worker, and React Frontend) with a single command:

### Windows (PowerShell or Double-Click):
```powershell
# In PowerShell:
.\start-all.ps1

# Or simply double-click:
start-all.bat
```

### Linux / macOS / WSL:
```bash
chmod +x start-all.sh
./start-all.sh
```

### What the Script Does Automatically:
1. **Validates & Sets Up Environment**: Automatically creates `.env` from `.env.example` if missing.
2. **Checks Dependencies**: Verifies Go 1.22+ and Node.js 18+. Runs `npm install` in `frontend/` if needed.
3. **Boots Backend API (`:8081`)**: Starts the Go API server in fallback-safe in-memory or Postgres mode.
4. **Boots Background Worker**: Starts the event listener, reconciliation sweeper, and transactional outbox processor.
5. **Awaits Health Readiness**: Polls `http://localhost:8081/health` until the API is online.
6. **Boots Frontend (`:5173`)**: Starts the Vite dev server with proxy to the backend API.
7. **Launches Browser**: Automatically opens `http://localhost:5173`.
8. **Graceful Teardown**: Pressing `Q` or `Ctrl+C` cleanly terminates all child process trees without leaving orphaned background services.

---

### Manual Step-by-Step (Alternative)

If you prefer running services in separate terminal windows:

1. **Start the Go Backend**:
   ```bash
   go run ./cmd/api
   ```
   *The server starts on port `8081`. If PostgreSQL, Redis, or RabbitMQ are not running locally, the server automatically boots into resilient in-memory mode with pre-seeded demo data.*

2. **Start the Background Worker**:
   ```bash
   go run ./cmd/worker
   ```

3. **Start the React Frontend**:
   ```bash
   cd frontend
   npm install
   npm run dev
   ```
   *Frontend is accessible at [http://localhost:5173](http://localhost:5173).*

---

## 4. Running with Docker Compose

To start the complete multi-container stack (PostgreSQL, Redis, RabbitMQ, Go API, Background Worker, React Frontend, and Nginx reverse proxy):

```bash
docker compose up -d --build
```

### Access URLs
- **Web Application**: [http://localhost](http://localhost) (routed via Nginx)
- **Direct API**: [http://localhost:8080](http://localhost:8080)
- **Swagger UI**: [http://localhost:8080/swagger](http://localhost:8080/swagger)
- **RabbitMQ Management**: [http://localhost:15672](http://localhost:15672) (`guest`/`guest`)
- **Health Check**: [http://localhost:8080/health](http://localhost:8080/health)

---

## 5. Pre-Seeded Demo Accounts

The application includes pre-seeded demo accounts with bcrypt password hashes:

| User | Email | Password | Role |
|---|---|---|---|
| Alice | `alice@example.com` | `Password123!` | Customer |
| Bob | `bob@example.com` | `Password123!` | Customer |
| Admin | `admin@example.com` | `AdminSecret123!` | Administrator |

*The top navigation bar of the frontend includes 1-click account switching between Alice, Bob, and Admin to easily test ownership and cross-user authorization.*

---

## 6. How to Configure Supabase PostgreSQL

To connect to a managed Supabase PostgreSQL instance:

1. Copy `.env.example` to `.env`:
   ```bash
   cp .env.example .env
   ```
2. Retrieve your database connection string from the Supabase Dashboard:
   - Go to **Project Settings** > **Database**.
   - Use the **Direct Connection** (Port 5432) for running migrations.
   - Use the **Transaction Pooler** (Port 6543) for production application scaling.
3. Update `DATABASE_URL` in `.env`:
   ```ini
   DATABASE_URL=postgres://postgres:[YOUR-PASSWORD]@db.[YOUR-PROJECT-REF].supabase.co:5432/postgres?sslmode=require
   ```
4. Run migrations using psql, Supabase SQL Editor, or the application migrator:
   ```bash
   # Executing migrations against Supabase
   psql "$DATABASE_URL" -f migrations/001_initial_schema.sql
   psql "$DATABASE_URL" -f migrations/002_seed_data.sql
   ```

---

## 7. Running Tests & Concurrency Verification

Run the comprehensive test suite with race detection enabled:

```bash
go test -v -race ./tests/...
```

### Verified Failure Scenarios
The automated test suite explicitly asserts the 10 failure scenarios:
1. `TestSingleSeat100ConcurrentReservations`: 100 simultaneous workers burst-target Seat `A1` -> Exactly 1 succeeds, 99 rejected with conflict, zero duplicate holds.
2. `TestMultiSeatOverlappingContention`: 3 workers request overlapping seat pairs -> Exactly 1 succeeds, zero deadlocks.
3. `TestReservationExpiresWhilePaymentProcessing`: Late payment confirmation on expired hold is rejected with `ErrReservationExpired`.
4. `TestPaymentIdempotentRetryOnTimeout`: Client times out and retries with same `Idempotency-Key` -> Cached response returned with `X-Cache-Lookup: HIT-IDEMPOTENCY`.
5. `TestIdempotentMessageBusDelivery`: RabbitMQ redelivers confirmation message -> Idempotently handled with zero duplicate bookings.
6. `TestRedisFailureGracefulFallback`: Redis unreachable -> Graceful fallback to in-memory cache and rate limiting without downtime.
7. `TestWorkerRestartPendingExpiryReconciliation`: Worker restarts while holds are expired -> Reconciler restores seats to `AVAILABLE`.
8. `TestUnauthorizedCrossUserBookingAccess`: User Bob attempts to read or cancel Alice's booking -> Rejected with `403 Forbidden`.
9. `TestCancellationRacesWithPaymentConfirmation`: Simultaneous cancellation vs confirmation race -> Exactly one wins, zero state corruption.
10. `TestApplicationRestartPendingOutboxRecovery`: App crashes before outbox publish -> Recovered from database and published upon restart.

---

## 8. Interactive Concurrency Demonstration

The frontend includes a dedicated **Dev & Stress Demo** tab:
1. Navigate to the **Dev & Stress Demo** tab in the running frontend.
2. Choose a worker concurrency burst: **10**, **25**, **50**, or **100** simultaneous goroutines.
3. Click **Run Live Concurrency Stress Test**.
4. The backend fires concurrent workers at the selected seat in a single burst, verifying that exactly one user claims the seat and all other requests receive HTTP 409 conflicts, with zero database inconsistencies.
