package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Seat Status constants
const (
	SeatStatusAvailable   = "AVAILABLE"
	SeatStatusHeld        = "HELD"
	SeatStatusBooked      = "BOOKED"
	SeatStatusUnavailable = "UNAVAILABLE"
)

// Reservation Status constants
const (
	ReservationStatusPending   = "PENDING"
	ReservationStatusConfirmed = "CONFIRMED"
	ReservationStatusExpired   = "EXPIRED"
	ReservationStatusCancelled = "CANCELLED"
)

// Booking Status constants
const (
	BookingStatusConfirmed = "CONFIRMED"
	BookingStatusCancelled = "CANCELLED"
)

// Payment Status constants
const (
	PaymentStatusPending  = "PENDING"
	PaymentStatusSuccess  = "SUCCESS"
	PaymentStatusFailed   = "FAILED"
	PaymentStatusTimeout  = "TIMEOUT"
	PaymentStatusRefunded = "REFUNDED"
)

// Payment Simulation modes
const (
	SimulateSuccess = "SUCCESS"
	SimulateFailure = "FAILURE"
	SimulateDelayed = "DELAYED"
	SimulateTimeout = "TIMEOUT"
)

// User roles
const (
	RoleCustomer = "CUSTOMER"
	RoleAdmin    = "ADMIN"
)

// Common Domain Errors
var (
	ErrSeatNotFound                 = errors.New("one or more seats not found")
	ErrSeatUnavailable              = errors.New("one or more requested seats are not available")
	ErrReservationNotFound          = errors.New("reservation not found")
	ErrReservationExpired           = errors.New("reservation has expired")
	ErrReservationAlreadyConfirmed  = errors.New("reservation is already confirmed")
	ErrReservationNotPending        = errors.New("reservation is not in pending status")
	ErrBookingNotFound              = errors.New("booking not found")
	ErrBookingAlreadyCancelled      = errors.New("booking is already cancelled")
	ErrUnauthorized                 = errors.New("unauthorized action for resource")
	ErrInvalidCredentials           = errors.New("invalid email or password")
	ErrPaymentFailed                = errors.New("payment failed")
	ErrDuplicateIdempotencyKey      = errors.New("duplicate request currently being processed or completed")
	ErrIdempotencyPayloadMismatch   = errors.New("idempotency key reused with different request payload")
	ErrCapacityExceeded             = errors.New("system capacity reached, please try again")
	ErrEmptySeatSelection           = errors.New("at least one seat must be selected")
	ErrMaxSeatsExceeded             = errors.New("cannot reserve more than 6 seats per transaction")
)

// User represents a system account
type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"full_name"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Event represents a bookable show, conference or performance
type Event struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Venue       string    `json:"venue"`
	Category    string    `json:"category"`
	BannerURL   string    `json:"banner_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Sessions    []Session `json:"sessions,omitempty"`
}

// Session represents a specific scheduled timeslot for an Event
type Session struct {
	ID             uuid.UUID `json:"id"`
	EventID        uuid.UUID `json:"event_id"`
	Title          string    `json:"title"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	TotalSeats     int       `json:"total_seats,omitempty"`
	AvailableSeats int       `json:"available_seats,omitempty"`
}

// Seat represents a distinct seat in a session
type Seat struct {
	ID             uuid.UUID `json:"id"`
	SessionID      uuid.UUID `json:"session_id"`
	RowLabel       string    `json:"row_label"`
	SeatNumber     int       `json:"seat_number"`
	SeatCategory   string    `json:"seat_category"` // VIP, PREMIUM, STANDARD, BALCONY
	BasePriceCents int       `json:"base_price_cents"`
	Status         string    `json:"status"` // AVAILABLE, HELD, BOOKED, UNAVAILABLE
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Reservation represents a temporary lock on seats
type Reservation struct {
	ID              uuid.UUID         `json:"id"`
	SessionID       uuid.UUID         `json:"session_id"`
	UserID          uuid.UUID         `json:"user_id"`
	Status          string            `json:"status"` // PENDING, CONFIRMED, EXPIRED, CANCELLED
	ExpiresAt       time.Time         `json:"expires_at"`
	TotalPriceCents int               `json:"total_price_cents"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Seats           []Seat            `json:"seats,omitempty"`
	SecondsLeft     int64             `json:"seconds_left,omitempty"`
}

// ReservationItem links a reservation to a seat
type ReservationItem struct {
	ID            uuid.UUID `json:"id"`
	ReservationID uuid.UUID `json:"reservation_id"`
	SeatID        uuid.UUID `json:"seat_id"`
	PriceCents    int       `json:"price_cents"`
	CreatedAt     time.Time `json:"created_at"`
}

// Booking represents a confirmed transaction
type Booking struct {
	ID               uuid.UUID `json:"id"`
	ReservationID    uuid.UUID `json:"reservation_id"`
	SessionID        uuid.UUID `json:"session_id"`
	UserID           uuid.UUID `json:"user_id"`
	BookingReference string    `json:"booking_reference"`
	Status           string    `json:"status"` // CONFIRMED, CANCELLED
	TotalPriceCents  int       `json:"total_price_cents"`
	ConfirmedAt      time.Time `json:"confirmed_at"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Seats            []Seat    `json:"seats,omitempty"`
	EventTitle       string    `json:"event_title,omitempty"`
	SessionTitle     string    `json:"session_title,omitempty"`
	Venue            string    `json:"venue,omitempty"`
	SessionStartTime time.Time `json:"session_start_time,omitempty"`
}

// BookingItem links a confirmed booking to a seat
type BookingItem struct {
	ID         uuid.UUID `json:"id"`
	BookingID  uuid.UUID `json:"booking_id"`
	SeatID     uuid.UUID `json:"seat_id"`
	PriceCents int       `json:"price_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

// Payment represents a simulated transaction record
type Payment struct {
	ID             uuid.UUID  `json:"id"`
	ReservationID  uuid.UUID  `json:"reservation_id"`
	BookingID      *uuid.UUID `json:"booking_id,omitempty"`
	UserID         uuid.UUID  `json:"user_id"`
	AmountCents    int        `json:"amount_cents"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"` // PENDING, SUCCESS, FAILED, TIMEOUT, REFUNDED
	IdempotencyKey *string    `json:"idempotency_key,omitempty"`
	ProviderRef    string     `json:"provider_ref,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// IdempotencyRecord stores request fingerprints and cached responses
type IdempotencyRecord struct {
	Key                string    `json:"key"`
	UserID             uuid.UUID `json:"user_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	ResponseCode       int       `json:"response_code"`
	ResponseBody       string    `json:"response_body"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}

// OutboxEvent represents an asynchronous message to be dispatched reliably
type OutboxEvent struct {
	ID            uuid.UUID `json:"id"`
	AggregateType string    `json:"aggregate_type"`
	AggregateID   uuid.UUID `json:"aggregate_id"`
	EventType     string    `json:"event_type"`
	Payload       []byte    `json:"payload"`
	Status        string    `json:"status"` // PENDING, PUBLISHED, FAILED
	RetryCount    int       `json:"retry_count"`
	CreatedAt     time.Time `json:"created_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

// AuditLog captures state changes for compliance and monitoring
type AuditLog struct {
	ID         uuid.UUID `json:"id"`
	UserID     *uuid.UUID `json:"user_id,omitempty"`
	Action     string    `json:"action"`
	EntityType string    `json:"entity_type"`
	EntityID   uuid.UUID `json:"entity_id"`
	Metadata   string    `json:"metadata,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
