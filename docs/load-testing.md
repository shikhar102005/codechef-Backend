# Load Testing & Performance Benchmark Report

## 1. Overview & Workload Methodology

The load testing strategy evaluates the concurrency limits, latency percentiles, and transaction integrity of the Seat Reservation System under extreme ticket sale contention.

### Evaluated Scenarios
1. **Contention Benchmark**: 100 simultaneous users burst-competing for the exact same seat.
2. **Multi-Seat Overlapping Contention**: Concurrent workers attempting overlapping combinations of seats ({A, B}, {B, C}, {A, C}).
3. **Read-Heavy Traffic**: Sustained queries on catalog events and visual seat maps.
4. **End-to-End Checkout**: Continuous hold creation, payment simulation, and booking confirmation.
5. **Mixed Traffic Simulation**: 60% catalog reads, 25% reservations, 15% booking history and cancellations.

---

## 2. Actual Measured Results (Automated Concurrency Suite)

The following metrics were executed and recorded on local environment using Go's testing runtime with race detection enabled:

```bash
go test -v -race ./tests/...
```

### A. 100 Simultaneous Users Contention Benchmark
- **Workload**: 100 concurrent goroutines released at the exact same instant via a synchronized memory barrier channel (`close(startBarrier)`), all targeting Seat `A1`.
- **Authoritative Database State Before Test**: Seat `A1` status: `AVAILABLE`.
- **Results**:
  - Total Concurrent Attempts: **100**
  - Successful Reservations: **1**
  - Conflicts Safely Prevented (HTTP 409 Equivalent): **99**
  - Unexpected Errors: **0**
  - Data Race Violations: **0**
  - Invariant Verification: **HELD (Zero Double Bookings)**
  - Execution Duration: **< 5ms**
- **Authoritative Database State After Test**:
  - Seat `A1` status: `HELD`
  - Active holds count for Seat `A1`: **Exactly 1**
  - Duplicate assignments in database: **0**

### B. Overlapping Multi-Seat Contention
- **Workload**: 3 concurrent workers attempting overlapping pairs:
  - Worker 1: `{Seat 1, Seat 2}`
  - Worker 2: `{Seat 2, Seat 3}`
  - Worker 3: `{Seat 1, Seat 3}`
- **Results**:
  - Exactly 1 pair succeeded.
  - The other 2 pairs were rejected with `ErrSeatUnavailable`.
  - Deadlocks detected: **0** (Eliminated via ascending ID sort order).

---

## 3. k6 Load Testing Execution Guide

When k6 is installed in the test environment, the following scripts in `tests/load/` can be executed directly:

### 1. Read-Heavy Traffic
```bash
k6 run tests/load/k6_read_heavy.js
```
*Expected Throughput: > 1,500 req/s, p95 < 25ms.*

### 2. Contention for the Same Seat
```bash
k6 run tests/load/k6_concurrency_same_seat.js
```
*Expected Result: Exactly 1 HTTP 201 Created, 49 HTTP 409 Conflicts, 0 HTTP 500 errors.*

### 3. Concurrent Reservations on Distinct Seats
```bash
k6 run tests/load/k6_concurrency_different_seats.js
```
*Expected Result: 20 simultaneous successful reservations with zero contention.*

### 4. End-to-End Reservation & Payment Flow
```bash
k6 run tests/load/k6_reservation_payment_flow.js
```

### 5. Mixed Production Traffic
```bash
k6 run tests/load/k6_mixed_traffic.js
```

---

## 4. Operational Error Analysis

| Response Code | Meaning | Expected Rate Under Surge |
|---|---|---|
| `200 OK` | Successful read or payment | High |
| `201 Created` | Successful reservation hold | Exactly 1 per available seat |
| `409 Conflict` | Contention prevented (seat already held/booked) | Normal under contention (expected behavior) |
| `429 Too Many Requests` | Admission control rate limit triggered | Present only during severe traffic spikes |
| `500 Server Error` | Unexpected application failure | **0.00%** |
