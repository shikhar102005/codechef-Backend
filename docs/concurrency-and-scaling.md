# Concurrency Control, High Availability & Scaling Architecture

## 1. The Double-Booking Problem

In high-concurrency event ticketing systems, hundreds or thousands of simultaneous requests can arrive within the same millisecond targeting high-demand seats. If an application relies on naive patterns (e.g. `SELECT status WHERE status = 'AVAILABLE'`, then in application memory checking availability, followed by `UPDATE seats SET status = 'BOOKED'`), a classic **Time-of-Check to Time-of-Use (TOCTOU)** race condition occurs:
1. Thread A checks seat 1 -> AVAILABLE.
2. Thread B checks seat 1 -> AVAILABLE.
3. Thread A writes BOOKED.
4. Thread B writes BOOKED.
**Result**: Two distinct users believe they have secured the same seat.

---

## 2. Our Concurrency & Locking Strategy

### A. Strict Row-Level Locking (`SELECT ... FOR UPDATE`)
In PostgreSQL, transactions operate at the `Read Committed` isolation level combined with explicit row-level locks:
```sql
SELECT id, session_id, status, base_price_cents, version 
FROM seats 
WHERE id = ANY($1) 
ORDER BY id ASC 
FOR UPDATE;
```
- When Request A locks Seat 1, Request B attempting to lock Seat 1 is suspended at the database engine level until Request A commits or rolls back.
- When Request B unblocks, it reads the updated, committed state of Seat 1 (which is now `HELD`), and immediately fails validation with `ErrSeatUnavailable`, triggering an HTTP 409 Conflict.

### B. Deadlock Prevention: Deterministic Lock Acquisition Ordering
If User A requests seats `[Seat 1, Seat 2]` and User B requests seats `[Seat 2, Seat 1]`, concurrent execution can cause a cyclic wait deadlock.
To eliminate deadlocks:
```go
sort.Slice(sortedSeatIDs, func(i, j int) bool {
    return sortedSeatIDs[i].String() < sortedSeatIDs[j].String()
})
```
By enforcing strict ascending lock acquisition order, a circular lock dependency is mathematically impossible.

### C. Physical Database Constraint Enforcement
Even in the event of an application logic bug or network split, database primary key constraints act as the absolute final safeguard:
- `active_seat_holds` has `seat_id` as primary key.
- `confirmed_seat_assignments` has `seat_id` as primary key.
PostgreSQL will physically reject any simultaneous insert with unique violation error `23505`.

---

## 3. Idempotency & Safe Network Retries

Clients facing network drops or timeouts can safely retry requests using the `Idempotency-Key` HTTP header.
1. `IdempotencyMiddleware` hashes the request body, path, and user ID (`SHA-256`).
2. Checks PostgreSQL `idempotency_keys` table.
3. If an identical request was already processed, the middleware returns the exact cached HTTP response with `X-Cache-Lookup: HIT-IDEMPOTENCY`.
4. If the client reuses an idempotency key with a conflicting payload, HTTP `409 Conflict` is returned.

---

## 4. Temporary Reservations & Expiry Architecture

Seat holds default to a 5-minute TTL.
1. The expiration timestamp is stored authoritatively in PostgreSQL: `expires_at TIMESTAMPTZ`.
2. **Two-Tier Expiration Model**:
   - **Lazy Expiry on Mutation**: Any read or confirmation check automatically verifies `NOW() > expires_at`. An expired hold is rejected immediately even if background workers have not yet processed it.
   - **Periodic Reconciler**: Background worker queries `WHERE status = 'PENDING' AND expires_at < NOW()` every 5 seconds, releases holds, and sets seats back to `AVAILABLE`.

---

## 5. Transactional Outbox Pattern & Reliable Messaging

Directly publishing to RabbitMQ inside an HTTP handler introduces the dual-write problem (DB commits, but RabbitMQ publish fails, or vice versa).
We implement the **Transactional Outbox Pattern**:
1. During reservation creation or booking confirmation, an `outbox_events` record is written inside the **exact same database transaction** as the domain mutation.
2. The `outbox.Processor` polls pending outbox records, publishes them to RabbitMQ using **publisher confirms**, and marks them `PUBLISHED` upon confirmation.
3. Message consumers implement consumer acknowledgements and dead-letter queues (`seat_booking_dlq`).
4. Message processing is strictly idempotent.

---

## 6. Redis Role & Fallback Resilience

- Redis caches read-heavy event and session catalog data with TTL invalidation.
- Redis provides sliding-window rate limiting.
- **Fail-Safe Design**: If Redis becomes unavailable, the system automatically switches to an in-memory sliding window rate limiter and queries PostgreSQL directly. PostgreSQL remains authoritative at all times.

---

## 7. Admission Control & Capacity Limits

To protect database connection pools from saturation during ticket release surges:
- `AdmissionControl` middleware limits max active concurrent requests (default: 100).
- Overflow requests are placed in an in-process FIFO wait queue (capacity: 500) with a 5-second queue timeout.
- Saturated requests receive HTTP `429 Too Many Requests` or `503 Service Unavailable` with `Retry-After: 3` header rather than exhausting database connections.
