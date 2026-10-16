# Booking Workflows & State Machine Architecture

This document diagrams all core transactional and asynchronous workflows implemented in the Seat Reservation System.

---

## 1. Normal Reservation & Successful Payment

```mermaid
sequenceDiagram
    autonumber
    actor User as Customer Client
    participant API as Go HTTP Handler
    participant DB as PostgreSQL (Supabase)
    participant Outbox as Outbox Processor
    participant MQ as RabbitMQ Bus

    User->>API: POST /api/v1/reservations {sessionId, [seatId]}
    Note over API,DB: Transaction Begins (Read Committed)
    API->>DB: SELECT * FROM seats WHERE id = ANY(...) ORDER BY id ASC FOR UPDATE
    DB-->>API: Seat Locked (status: AVAILABLE)
    API->>DB: INSERT INTO reservations (status: PENDING, expires_at: NOW() + 5m)
    API->>DB: INSERT INTO active_seat_holds (seat_id, reservation_id)
    API->>DB: UPDATE seats SET status = 'HELD', version = version + 1
    API->>DB: INSERT INTO outbox_events (ReservationCreated)
    Note over API,DB: Transaction Committed
    API-->>User: HTTP 201 Created {reservation, seconds_left: 300}

    User->>API: POST /api/v1/payments {reservationId, amount, simulation: 'SUCCESS'}
    Note over API,DB: Transaction Begins (Read Committed)
    API->>DB: SELECT * FROM reservations WHERE id = ... FOR UPDATE
    DB-->>API: Valid Pending Reservation
    API->>DB: SELECT * FROM seats WHERE id IN (...) FOR UPDATE
    API->>DB: INSERT INTO bookings (booking_reference: BK-XXXX, status: CONFIRMED)
    API->>DB: INSERT INTO confirmed_seat_assignments (seat_id, booking_id)
    API->>DB: DELETE FROM active_seat_holds WHERE reservation_id = ...
    API->>DB: UPDATE seats SET status = 'BOOKED', version = version + 1
    API->>DB: UPDATE reservations SET status = 'CONFIRMED'
    API->>DB: INSERT INTO outbox_events (BookingConfirmed)
    Note over API,DB: Transaction Committed
    API-->>User: HTTP 200 OK {booking, payment: SUCCESS}

    Outbox->>DB: Fetch pending outbox events
    Outbox->>MQ: Publish BookingConfirmed to booking_notifications_queue
    Outbox->>DB: Mark outbox event PUBLISHED
```

---

## 2. Concurrent Contention for the Same Seat (100 Simultaneous Requests)

```mermaid
sequenceDiagram
    autonumber
    actor Alice as User Alice (Worker 1)
    actor Bob as User Bob (Worker 2..100)
    participant DB as PostgreSQL Engine

    par Simultaneous Requests
        Alice->>DB: BEGIN TX: SELECT * FROM seats WHERE id = 'A1' FOR UPDATE
    and
        Bob->>DB: BEGIN TX: SELECT * FROM seats WHERE id = 'A1' FOR UPDATE
    end

    Note over DB: Lock granted to Alice first.<br/>Bob's transaction BLOCKS on row lock.
    Alice->>DB: Status check: AVAILABLE -> Passes
    Alice->>DB: INSERT active_seat_holds ('A1')
    Alice->>DB: UPDATE seats SET status = 'HELD'
    Alice->>DB: COMMIT TX
    Note over Alice: Alice receives HTTP 201 Created

    Note over DB: Bob's blocked query unblocks.<br/>Re-reads committed row for 'A1'.
    DB-->>Bob: Status is now 'HELD'
    Note over Bob: Status check fails: Seat not available!
    Bob->>DB: ROLLBACK TX
    Note over Bob: Bob receives HTTP 409 Conflict<br/>("Seat is unavailable")
```

---

## 3. Temporary Reservation Expiry & Reconciliation

```mermaid
sequenceDiagram
    autonumber
    actor User as Customer
    participant API as Go API
    participant DB as PostgreSQL
    participant Worker as Background Worker

    User->>API: POST /api/v1/reservations (Locks Seat A1 for 5 mins)
    API->>DB: Status = PENDING, ExpiresAt = T + 5m
    API-->>User: HTTP 201 Created

    Note over User,Worker: 5 Minutes Elapse without Payment

    alt Path A: Customer attempts payment after expiry
        User->>API: POST /api/v1/payments
        API->>DB: SELECT * FROM reservations WHERE id = ... FOR UPDATE
        Note over API: Condition: NOW() > expires_at
        API->>DB: DELETE FROM active_seat_holds
        API->>DB: UPDATE seats SET status = 'AVAILABLE'
        API->>DB: UPDATE reservations SET status = 'EXPIRED'
        API-->>User: HTTP 409 Conflict ("Reservation expired. Seats released.")
    else Path B: Worker Periodic Reconciliation
        Worker->>DB: SELECT id FROM reservations WHERE status='PENDING' AND expires_at < NOW()
        Worker->>DB: DELETE active_seat_holds, UPDATE seats AVAILABLE, UPDATE reservations EXPIRED
        Note over Worker: Seats instantly reservable by subsequent users
    end
```

---

## 4. Cancellation and Safe Seat Reallocation

```mermaid
sequenceDiagram
    autonumber
    actor Alice as Customer Alice
    actor Bob as Customer Bob
    participant API as Go API
    participant DB as PostgreSQL

    Alice->>API: POST /api/v1/bookings/:bookingId/cancel
    API->>DB: SELECT * FROM bookings WHERE id = ... FOR UPDATE
    Note over API: Ownership check: booking.user_id == Alice.id
    API->>DB: DELETE FROM confirmed_seat_assignments WHERE booking_id = ...
    API->>DB: UPDATE seats SET status = 'AVAILABLE', version = version + 1
    API->>DB: UPDATE bookings SET status = 'CANCELLED', cancelled_at = NOW()
    API->>DB: INSERT INTO outbox_events (BookingCancelled)
    API-->>Alice: HTTP 200 OK ("Booking cancelled and seats reallocated")

    Bob->>API: POST /api/v1/reservations (Selects recently cancelled seat)
    API->>DB: SELECT * FROM seats WHERE id = ... FOR UPDATE
    DB-->>API: Seat status is AVAILABLE
    API->>DB: Creates hold for Bob
    API-->>Bob: HTTP 201 Created (Instant reallocation)
```

---

## 5. Idempotent Retry on Network Timeout

```mermaid
sequenceDiagram
    autonumber
    actor Client as Client App
    participant Middleware as Idempotency Middleware
    participant Handler as Payment Handler
    participant DB as PostgreSQL

    Client->>Middleware: POST /api/v1/payments (Idempotency-Key: "pay-key-123")
    Middleware->>DB: SELECT * FROM idempotency_keys WHERE key = "pay-key-123"
    DB-->>Middleware: No record found (Cache Miss)
    Middleware->>Handler: Forward Request
    Handler->>DB: Commit payment and booking
    Handler-->>Middleware: HTTP 200 OK {booking: BK-123}
    Middleware->>DB: INSERT INTO idempotency_keys ("pay-key-123", hash, code: 200, body)
    Middleware--xClient: Simulated Network Packet Drop (Client times out!)

    Note over Client: Client automatically retries after timeout
    Client->>Middleware: POST /api/v1/payments (Same Idempotency-Key: "pay-key-123")
    Middleware->>DB: SELECT * FROM idempotency_keys WHERE key = "pay-key-123"
    DB-->>Middleware: Record Found! Hash Matches!
    Middleware-->>Client: HTTP 200 OK (Replayed from DB, X-Cache-Lookup: HIT-IDEMPOTENCY)
    Note over DB: ZERO duplicate charges, ZERO duplicate bookings!
```
