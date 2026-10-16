package domain

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines all data operations required by the booking system
type Repository interface {
	// Users
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*User, error)
	CreateUser(ctx context.Context, user *User) error

	// Events and Sessions
	ListEvents(ctx context.Context) ([]Event, error)
	GetEventByID(ctx context.Context, id uuid.UUID) (*Event, error)
	GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error)
	ListSessionsByEventID(ctx context.Context, eventID uuid.UUID) ([]Session, error)

	// Seats
	ListSeatsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]Seat, error)
	GetSeatByID(ctx context.Context, id uuid.UUID) (*Seat, error)

	// Concurrency-Safe Reservations
	// Atomically acquires row-level locks on requested seats in deterministic order (id ASC),
	// validates availability, sets seats to HELD, inserts reservation, reservation_items,
	// active_seat_holds, and enqueues outbox event.
	CreateReservationTx(ctx context.Context, res *Reservation, seatIDs []uuid.UUID) (*Reservation, error)
	GetReservationByID(ctx context.Context, id uuid.UUID) (*Reservation, error)
	CancelReservationTx(ctx context.Context, reservationID uuid.UUID, userID uuid.UUID) error
	ConfirmReservationTx(ctx context.Context, reservationID uuid.UUID, bookingRef string) (*Booking, error)

	// Background Expiry Reconciliation
	ExpireOverdueReservations(ctx context.Context) (int, error)

	// Bookings
	GetBookingByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	GetBookingByReference(ctx context.Context, ref string) (*Booking, error)
	ListBookingsByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Booking, error)
	CancelBookingTx(ctx context.Context, bookingID uuid.UUID, userID uuid.UUID) error

	// Payments
	CreatePaymentTx(ctx context.Context, payment *Payment) error
	UpdatePaymentStatus(ctx context.Context, paymentID uuid.UUID, status string, providerRef string, errMsg string, bookingID *uuid.UUID) error
	GetPaymentByID(ctx context.Context, id uuid.UUID) (*Payment, error)

	// Idempotency
	GetIdempotencyRecord(ctx context.Context, key string) (*IdempotencyRecord, error)
	SaveIdempotencyRecord(ctx context.Context, record *IdempotencyRecord) error

	// Transactional Outbox
	FetchPendingOutboxEvents(ctx context.Context, limit int) ([]OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID) error

	// Audit Logs
	CreateAuditLog(ctx context.Context, log *AuditLog) error

	// Health Check
	Ping(ctx context.Context) error
	Close()
}
