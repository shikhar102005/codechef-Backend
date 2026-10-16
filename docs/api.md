# REST API Reference & OpenAPI Documentation

All API endpoints are versioned under `/api/v1` and return consistent JSON structures.

Interactive Swagger UI documentation is available at:
- **Swagger UI**: [http://localhost:8080/swagger](http://localhost:8080/swagger)
- **OpenAPI 3.0 Spec**: [http://localhost:8080/swagger/doc.json](http://localhost:8080/swagger/doc.json)

---

## 1. Authentication & Security

All protected endpoints require a Bearer token:
```http
Authorization: Bearer <JWT_TOKEN>
```

### Pre-Seeded Development Credentials
| Role | Email | Password | Scope |
|---|---|---|---|
| Customer 1 | `alice@example.com` | `Password123!` | Customer reservations & personal bookings |
| Customer 2 | `bob@example.com` | `Password123!` | Customer reservations & personal bookings |
| Admin | `admin@example.com` | `AdminSecret123!` | Global inspection and administrative bypass |

---

## 2. API Endpoints

### 2.1 Authentication
- `POST /api/v1/auth/login`: Authenticate and obtain JWT token.
- `POST /api/v1/auth/register`: Register new customer account.
- `GET /api/v1/auth/me`: Get current authenticated user profile.

### 2.2 Events & Sessions
- `GET /api/v1/events`: List all available events with upcoming sessions.
- `GET /api/v1/events/{eventId}`: Get event details by ID.
- `GET /api/v1/sessions/{sessionId}`: Get specific session details and seat count.
- `GET /api/v1/sessions/{sessionId}/seats`: Get visual seat map with real-time statuses (`AVAILABLE`, `HELD`, `BOOKED`, `UNAVAILABLE`).

### 2.3 Reservations (5-Minute Temporary Hold)
- `POST /api/v1/reservations`: Atomically reserve 1 or more seats with row locks.
  - Header: `Idempotency-Key` (Optional, recommended)
  - Body: `{ "session_id": "UUID", "seat_ids": ["UUID"] }`
  - Returns: `201 Created` with reservation details and `seconds_left`.
  - Conflict: `409 Conflict` if any seat is already held or booked.
- `GET /api/v1/reservations/{reservationId}`: Get reservation details and live countdown.
- `POST /api/v1/reservations/{reservationId}/confirm`: Confirm reservation into a booking.
- `POST /api/v1/reservations/{reservationId}/cancel`: Voluntarily release held seats back to the availability pool.

### 2.4 Bookings
- `GET /api/v1/bookings`: List paginated bookings for authenticated user (`?limit=20&offset=0`).
- `GET /api/v1/bookings/{bookingId}`: Get single booking by ID or reference string (`BK-XXXX-XXXX`).
- `POST /api/v1/bookings/{bookingId}/cancel`: Cancel booking and reallocate seats back to available pool.

### 2.5 Payments Simulator
- `POST /api/v1/payments`: Process payment simulation for a reservation.
  - Header: `Idempotency-Key: pay-key-xyz`
  - Body:
    ```json
    {
      "reservation_id": "UUID",
      "amount_cents": 25000,
      "simulation": "SUCCESS | FAILURE | DELAYED | TIMEOUT"
    }
    ```
  - `SUCCESS`: Returns `200 OK` with confirmed `booking`.
  - `FAILURE`: Returns `402 Payment Required` (card declined).
  - `DELAYED`: Returns `202 Accepted` (enqueued to RabbitMQ worker).
  - `TIMEOUT`: Returns `504 Gateway Timeout` (safe to retry with same key).
- `GET /api/v1/payments/{paymentId}`: Check payment status.

### 2.6 Operational & System Endpoints
- `GET /health`: Liveness probe.
- `GET /ready`: Readiness probe checking DB, Redis, and RabbitMQ connectivity.
- `GET /metrics`: Prometheus-compatible memory, goroutine, and request metrics.
- `GET /api/v1/system/stats`: Comprehensive system dashboard statistics.
- `POST /api/v1/demo/concurrency-test`: Triggers 10-100 simultaneous concurrent goroutines on a single seat to verify zero double bookings.

---

## 3. Representative cURL Examples

### Reserve a Seat
```bash
curl -X POST http://localhost:8080/api/v1/reservations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer demo-alice" \
  -H "Idempotency-Key: hold-test-001" \
  -d '{
    "session_id": "b0000000-0000-0000-0000-000000000001",
    "seat_ids": ["c0000000-0000-0000-0000-000000000001"]
  }'
```

### Confirm with Payment Simulator
```bash
curl -X POST http://localhost:8080/api/v1/payments \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer demo-alice" \
  -H "Idempotency-Key: pay-test-001" \
  -d '{
    "reservation_id": "RESERVATION_UUID",
    "amount_cents": 25000,
    "simulation": "SUCCESS"
  }'
```
