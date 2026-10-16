# Database Design & Entity Relationship Diagram (ERD)

## 1. Relational Architecture Overview

The database design adheres to 3NF relational normalization while enforcing transactional integrity through database-level constraints. PostgreSQL (Supabase) serves as the authoritative source of truth for all seat inventories, holds, bookings, and payments.

### Invariant Protections
1. **Zero Double-Bookings Guarantee**: Enforced by primary key uniqueness on `confirmed_seat_assignments(seat_id)`.
2. **Single Active Hold Guarantee**: Enforced by primary key uniqueness on `active_seat_holds(seat_id)`.
3. **Deadlock Elimination**: Queries acquiring multiple seat locks sort seat IDs in strict ascending order (`ORDER BY id ASC FOR UPDATE`).
4. **Reliable Asynchronous Operations**: Transactional outbox table (`outbox_events`) populated in the exact same database transaction as bookings or cancellations.

---

## 2. Mermaid Entity Relationship Diagram

```mermaid
erDiagram
    USERS ||--o{ RESERVATIONS : places
    USERS ||--o{ BOOKINGS : owns
    USERS ||--o{ PAYMENTS : pays
    USERS ||--o{ AUDIT_LOGS : triggers

    EVENTS ||--o{ SESSIONS : schedules
    SESSIONS ||--o{ SEATS : contains
    SESSIONS ||--o{ RESERVATIONS : hosts
    SESSIONS ||--o{ BOOKINGS : hosts

    SEATS ||--o{ RESERVATION_ITEMS : referenced_in
    SEATS ||--o{ BOOKING_ITEMS : assigned_in
    SEATS ||--o| ACTIVE_SEAT_HOLDS : locked_by
    SEATS ||--o| CONFIRMED_SEAT_ASSIGNMENTS : secured_by

    RESERVATIONS ||--|{ RESERVATION_ITEMS : includes
    RESERVATIONS ||--o| BOOKINGS : converts_to
    RESERVATIONS ||--o{ PAYMENTS : settles
    RESERVATIONS ||--o{ ACTIVE_SEAT_HOLDS : holds

    BOOKINGS ||--|{ BOOKING_ITEMS : contains
    BOOKINGS ||--o{ CONFIRMED_SEAT_ASSIGNMENTS : claims

    USERS {
        uuid id PK
        string email UK
        string full_name
        string password_hash
        string role
        timestamptz created_at
        timestamptz updated_at
    }

    EVENTS {
        uuid id PK
        string title
        text description
        string category
        string venue
        timestamptz created_at
        timestamptz updated_at
    }

    SESSIONS {
        uuid id PK
        uuid event_id FK
        string title
        timestamptz start_time
        timestamptz end_time
        string status
        timestamptz created_at
        timestamptz updated_at
    }

    SEATS {
        uuid id PK
        uuid session_id FK
        string row_label
        int seat_number
        string seat_category
        int base_price_cents
        string status
        bigint version
        timestamptz created_at
        timestamptz updated_at
    }

    RESERVATIONS {
        uuid id PK
        uuid session_id FK
        uuid user_id FK
        string status
        timestamptz expires_at
        int total_price_cents
        timestamptz created_at
        timestamptz updated_at
    }

    RESERVATION_ITEMS {
        uuid id PK
        uuid reservation_id FK
        uuid seat_id FK
        int price_cents
        timestamptz created_at
    }

    ACTIVE_SEAT_HOLDS {
        uuid seat_id PK, FK
        uuid session_id FK
        uuid reservation_id FK
        timestamptz expires_at
        timestamptz created_at
    }

    BOOKINGS {
        uuid id PK
        uuid reservation_id FK
        uuid session_id FK
        uuid user_id FK
        string booking_reference UK
        string status
        int total_price_cents
        timestamptz confirmed_at
        timestamptz cancelled_at
        timestamptz created_at
        timestamptz updated_at
    }

    BOOKING_ITEMS {
        uuid id PK
        uuid booking_id FK
        uuid seat_id FK
        int price_cents
        timestamptz created_at
    }

    CONFIRMED_SEAT_ASSIGNMENTS {
        uuid seat_id PK, FK
        uuid session_id FK
        uuid booking_id FK
        timestamptz created_at
    }

    PAYMENTS {
        uuid id PK
        uuid reservation_id FK
        uuid booking_id FK
        uuid user_id FK
        int amount_cents
        string currency
        string status
        string idempotency_key UK
        string provider_ref
        text error_message
        timestamptz created_at
        timestamptz updated_at
    }

    IDEMPOTENCY_KEYS {
        string key PK
        uuid user_id FK
        string request_fingerprint
        int response_code
        text response_body
        timestamptz created_at
        timestamptz expires_at
    }

    OUTBOX_EVENTS {
        uuid id PK
        string aggregate_type
        uuid aggregate_id
        string event_type
        jsonb payload
        string status
        int retry_count
        timestamptz created_at
        timestamptz published_at
    }

    AUDIT_LOGS {
        uuid id PK
        uuid user_id
        string action
        string entity_type
        uuid entity_id
        jsonb metadata
        timestamptz created_at
    }
```

---

## 3. Database Constraints & Schema Design Decisions

### A. Two-Tier Protection Mechanism
1. **Tier 1: Application Row Locking (`SELECT ... FOR UPDATE`)**:
   - Ensures that only one transaction examines and mutates an available seat at any instant.
   - Prevents read-modify-write race conditions.
2. **Tier 2: Database Table-Level Physical Constraints (`PRIMARY KEY`)**:
   - `active_seat_holds` has `seat_id` as its primary key. Even if application code failed, PostgreSQL would physically reject any second concurrent hold with error code `23505` (unique_violation).
   - `confirmed_seat_assignments` has `seat_id` as its primary key. A seat can physically never be assigned to more than one confirmed booking.

### B. Natural Seat Uniqueness
```sql
CONSTRAINT uq_session_row_seat UNIQUE (session_id, row_label, seat_number);
```
Guarantees that duplicate seat definitions within the same session cannot exist.

### C. State Machine Constraints
```sql
CONSTRAINT chk_seat_status CHECK (status IN ('AVAILABLE', 'HELD', 'BOOKED', 'UNAVAILABLE'));
CONSTRAINT chk_reservation_status CHECK (status IN ('PENDING', 'CONFIRMED', 'EXPIRED', 'CANCELLED'));
CONSTRAINT chk_booking_status CHECK (status IN ('CONFIRMED', 'CANCELLED'));
CONSTRAINT chk_payment_status CHECK (status IN ('PENDING', 'SUCCESS', 'FAILED', 'TIMEOUT'));
```
Enforces valid state machine values across all tables.
