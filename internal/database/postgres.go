package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/seat-booking/system/internal/domain"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(ctx context.Context, dbURL string) (*PostgresRepository, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	cfg.MaxConns = 30
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	repo := &PostgresRepository{pool: pool}
	return repo, nil
}

func (r *PostgresRepository) RunMigrations(ctx context.Context, migrationFiles ...string) error {
	for _, file := range migrationFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}
		slog.Info("Executing migration", "file", file)
		if _, err := r.pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("failed executing migration %s: %w", file, err)
		}
	}
	return nil
}

func (r *PostgresRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

// User Operations
func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `SELECT id, email, full_name, password_hash, role, created_at, updated_at FROM users WHERE email = $1`
	var u domain.User
	err := r.pool.QueryRow(ctx, query, email).Scan(&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PostgresRepository) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := `SELECT id, email, full_name, password_hash, role, created_at, updated_at FROM users WHERE id = $1`
	var u domain.User
	err := r.pool.QueryRow(ctx, query, id).Scan(&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("user not found")
	}
	return &u, err
}

func (r *PostgresRepository) CreateUser(ctx context.Context, user *domain.User) error {
	query := `INSERT INTO users (id, email, full_name, password_hash, role, created_at, updated_at) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`
	now := time.Now()
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	user.CreatedAt = now
	user.UpdatedAt = now
	_, err := r.pool.Exec(ctx, query, user.ID, user.Email, user.FullName, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt)
	return err
}

// Events and Sessions
func (r *PostgresRepository) ListEvents(ctx context.Context) ([]domain.Event, error) {
	query := `SELECT id, title, description, venue, category, COALESCE(banner_url, ''), created_at, updated_at FROM events ORDER BY created_at ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Venue, &e.Category, &e.BannerURL, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	for i := range events {
		sessions, err := r.ListSessionsByEventID(ctx, events[i].ID)
		if err == nil {
			events[i].Sessions = sessions
		}
	}

	return events, nil
}

func (r *PostgresRepository) GetEventByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	query := `SELECT id, title, description, venue, category, COALESCE(banner_url, ''), created_at, updated_at FROM events WHERE id = $1`
	var e domain.Event
	err := r.pool.QueryRow(ctx, query, id).Scan(&e.ID, &e.Title, &e.Description, &e.Venue, &e.Category, &e.BannerURL, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("event not found")
	}
	if err != nil {
		return nil, err
	}
	sessions, err := r.ListSessionsByEventID(ctx, id)
	if err == nil {
		e.Sessions = sessions
	}
	return &e, nil
}

func (r *PostgresRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	query := `
		SELECT s.id, s.event_id, s.title, s.start_time, s.end_time, s.status, s.created_at, s.updated_at,
		       COUNT(st.id) AS total_seats,
		       COUNT(st.id) FILTER (WHERE st.status = 'AVAILABLE') AS available_seats
		FROM sessions s
		LEFT JOIN seats st ON st.session_id = s.id
		WHERE s.id = $1
		GROUP BY s.id`
	var s domain.Session
	err := r.pool.QueryRow(ctx, query, id).Scan(&s.ID, &s.EventID, &s.Title, &s.StartTime, &s.EndTime, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.TotalSeats, &s.AvailableSeats)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("session not found")
	}
	return &s, err
}

func (r *PostgresRepository) ListSessionsByEventID(ctx context.Context, eventID uuid.UUID) ([]domain.Session, error) {
	query := `
		SELECT s.id, s.event_id, s.title, s.start_time, s.end_time, s.status, s.created_at, s.updated_at,
		       COUNT(st.id) AS total_seats,
		       COUNT(st.id) FILTER (WHERE st.status = 'AVAILABLE') AS available_seats
		FROM sessions s
		LEFT JOIN seats st ON st.session_id = s.id
		WHERE s.event_id = $1
		GROUP BY s.id
		ORDER BY s.start_time ASC`
	rows, err := r.pool.Query(ctx, query, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var s domain.Session
		if err := rows.Scan(&s.ID, &s.EventID, &s.Title, &s.StartTime, &s.EndTime, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.TotalSeats, &s.AvailableSeats); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// Seats
func (r *PostgresRepository) ListSeatsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domain.Seat, error) {
	query := `SELECT id, session_id, row_label, seat_number, seat_category, base_price_cents, status, version, created_at, updated_at 
	          FROM seats WHERE session_id = $1 ORDER BY row_label ASC, seat_number ASC`
	rows, err := r.pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seats []domain.Seat
	for rows.Next() {
		var s domain.Seat
		if err := rows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		seats = append(seats, s)
	}
	return seats, nil
}

func (r *PostgresRepository) GetSeatByID(ctx context.Context, id uuid.UUID) (*domain.Seat, error) {
	query := `SELECT id, session_id, row_label, seat_number, seat_category, base_price_cents, status, version, created_at, updated_at 
	          FROM seats WHERE id = $1`
	var s domain.Seat
	err := r.pool.QueryRow(ctx, query, id).Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSeatNotFound
	}
	return &s, err
}

// Concurrency-Safe Reservations
func (r *PostgresRepository) CreateReservationTx(ctx context.Context, res *domain.Reservation, seatIDs []uuid.UUID) (*domain.Reservation, error) {
	if len(seatIDs) == 0 {
		return nil, domain.ErrEmptySeatSelection
	}

	// 1. Sort seat IDs to guarantee strict ascending lock acquisition and prevent deadlocks across concurrent requests
	sortedSeatIDs := make([]uuid.UUID, len(seatIDs))
	copy(sortedSeatIDs, seatIDs)
	sort.Slice(sortedSeatIDs, func(i, j int) bool {
		return sortedSeatIDs[i].String() < sortedSeatIDs[j].String()
	})

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 2. Lock the seats in deterministic order using SELECT ... FOR UPDATE
	rows, err := tx.Query(ctx, `
		SELECT id, session_id, row_label, seat_number, seat_category, base_price_cents, status, version, created_at, updated_at 
		FROM seats 
		WHERE id = ANY($1) 
		ORDER BY id ASC 
		FOR UPDATE`, sortedSeatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to lock seats: %w", err)
	}
	defer rows.Close()

	var lockedSeats []domain.Seat
	totalPrice := 0
	for rows.Next() {
		var s domain.Seat
		if err := rows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if s.Status != domain.SeatStatusAvailable {
			return nil, domain.ErrSeatUnavailable
		}
		if s.SessionID != res.SessionID {
			return nil, fmt.Errorf("seat %s belongs to a different session", s.ID)
		}
		totalPrice += s.BasePriceCents
		lockedSeats = append(lockedSeats, s)
	}
	rows.Close()

	if len(lockedSeats) != len(sortedSeatIDs) {
		return nil, domain.ErrSeatNotFound
	}

	// 3. Insert reservation record
	now := time.Now()
	res.ID = uuid.New()
	res.Status = domain.ReservationStatusPending
	res.CreatedAt = now
	res.UpdatedAt = now
	res.TotalPriceCents = totalPrice
	res.Seats = lockedSeats
	if res.ExpiresAt.IsZero() {
		res.ExpiresAt = now.Add(5 * time.Minute)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO reservations (id, session_id, user_id, status, expires_at, total_price_cents, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		res.ID, res.SessionID, res.UserID, res.Status, res.ExpiresAt, res.TotalPriceCents, res.CreatedAt, res.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert reservation: %w", err)
	}

	// 4. Insert reservation items and active_seat_holds
	for _, seat := range lockedSeats {
		_, err = tx.Exec(ctx, `
			INSERT INTO reservation_items (id, reservation_id, seat_id, price_cents, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			uuid.New(), res.ID, seat.ID, seat.BasePriceCents, now)
		if err != nil {
			return nil, fmt.Errorf("failed to insert reservation item: %w", err)
		}

		// Insert into active_seat_holds (Primary Key: seat_id enforces single holder constraint)
		_, err = tx.Exec(ctx, `
			INSERT INTO active_seat_holds (seat_id, session_id, reservation_id, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			seat.ID, res.SessionID, res.ID, res.ExpiresAt, now)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation
				return nil, domain.ErrSeatUnavailable
			}
			return nil, fmt.Errorf("failed to record active hold: %w", err)
		}
	}

	// 5. Update seat status to HELD
	_, err = tx.Exec(ctx, `
		UPDATE seats 
		SET status = $1, version = version + 1, updated_at = $2 
		WHERE id = ANY($3)`,
		domain.SeatStatusHeld, now, sortedSeatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to update seat status: %w", err)
	}

	// 6. Transactional Outbox Event for reservation creation
	payload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": res.ID,
		"session_id":     res.SessionID,
		"user_id":        res.UserID,
		"expires_at":     res.ExpiresAt,
		"seat_ids":       sortedSeatIDs,
		"total_price":    res.TotalPriceCents,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 'PENDING', $6)`,
		uuid.New(), "RESERVATION", res.ID, "ReservationCreated", payload, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return res, nil
}

func (r *PostgresRepository) GetReservationByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	var res domain.Reservation
	err := r.pool.QueryRow(ctx, `
		SELECT id, session_id, user_id, status, expires_at, total_price_cents, created_at, updated_at 
		FROM reservations WHERE id = $1`, id).Scan(
		&res.ID, &res.SessionID, &res.UserID, &res.Status, &res.ExpiresAt, &res.TotalPriceCents, &res.CreatedAt, &res.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReservationNotFound
	}
	if err != nil {
		return nil, err
	}

	// Check if pending reservation is overdue
	if res.Status == domain.ReservationStatusPending && time.Now().After(res.ExpiresAt) {
		res.Status = domain.ReservationStatusExpired
	}

	// Fetch seats
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.session_id, s.row_label, s.seat_number, s.seat_category, ri.price_cents, s.status, s.version, s.created_at, s.updated_at
		FROM reservation_items ri
		JOIN seats s ON s.id = ri.seat_id
		WHERE ri.reservation_id = $1
		ORDER BY s.row_label ASC, s.seat_number ASC`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s domain.Seat
			if err := rows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err == nil {
				res.Seats = append(res.Seats, s)
			}
		}
	}

	left := time.Until(res.ExpiresAt).Seconds()
	if left < 0 {
		left = 0
	}
	res.SecondsLeft = int64(left)

	return &res, nil
}

func (r *PostgresRepository) ConfirmReservationTx(ctx context.Context, reservationID uuid.UUID, bookingRef string) (*domain.Booking, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock the reservation row
	var res domain.Reservation
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, user_id, status, expires_at, total_price_cents, created_at, updated_at
		FROM reservations
		WHERE id = $1
		FOR UPDATE`, reservationID).Scan(
		&res.ID, &res.SessionID, &res.UserID, &res.Status, &res.ExpiresAt, &res.TotalPriceCents, &res.CreatedAt, &res.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReservationNotFound
	}
	if err != nil {
		return nil, err
	}

	if res.Status == domain.ReservationStatusConfirmed {
		return nil, domain.ErrReservationAlreadyConfirmed
	}
	if res.Status != domain.ReservationStatusPending {
		return nil, domain.ErrReservationNotPending
	}
	if time.Now().After(res.ExpiresAt) {
		// Clean up expired hold
		_, _ = tx.Exec(ctx, `DELETE FROM active_seat_holds WHERE reservation_id = $1`, res.ID)
		_, _ = tx.Exec(ctx, `
			UPDATE seats SET status = 'AVAILABLE', updated_at = NOW() 
			WHERE id IN (SELECT seat_id FROM reservation_items WHERE reservation_id = $1)`, res.ID)
		_, _ = tx.Exec(ctx, `UPDATE reservations SET status = 'EXPIRED', updated_at = NOW() WHERE id = $1`, res.ID)
		_ = tx.Commit(ctx)
		return nil, domain.ErrReservationExpired
	}

	// 2. Fetch and lock seats for this reservation
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.session_id, s.row_label, s.seat_number, s.seat_category, ri.price_cents, s.status, s.version, s.created_at, s.updated_at
		FROM reservation_items ri
		JOIN seats s ON s.id = ri.seat_id
		WHERE ri.reservation_id = $1
		ORDER BY s.id ASC
		FOR UPDATE`, res.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to lock reservation seats: %w", err)
	}
	defer rows.Close()

	var seats []domain.Seat
	var seatIDs []uuid.UUID
	for rows.Next() {
		var s domain.Seat
		if err := rows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		seats = append(seats, s)
		seatIDs = append(seatIDs, s.ID)
	}
	rows.Close()

	if len(seats) == 0 {
		return nil, errors.New("no seats associated with reservation")
	}

	now := time.Now()
	bookingID := uuid.New()
	booking := &domain.Booking{
		ID:               bookingID,
		ReservationID:    res.ID,
		SessionID:        res.SessionID,
		UserID:           res.UserID,
		BookingReference: bookingRef,
		Status:           domain.BookingStatusConfirmed,
		TotalPriceCents:  res.TotalPriceCents,
		ConfirmedAt:      now,
		CreatedAt:        now,
		UpdatedAt:        now,
		Seats:            seats,
	}

	// 3. Insert into bookings
	_, err = tx.Exec(ctx, `
		INSERT INTO bookings (id, reservation_id, session_id, user_id, booking_reference, status, total_price_cents, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		booking.ID, booking.ReservationID, booking.SessionID, booking.UserID, booking.BookingReference, booking.Status, booking.TotalPriceCents, booking.ConfirmedAt, booking.CreatedAt, booking.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create booking: %w", err)
	}

	// 4. Insert booking items and confirmed_seat_assignments
	for _, seat := range seats {
		_, err = tx.Exec(ctx, `
			INSERT INTO booking_items (id, booking_id, seat_id, price_cents, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			uuid.New(), booking.ID, seat.ID, seat.BasePriceCents, now)
		if err != nil {
			return nil, fmt.Errorf("failed to insert booking item: %w", err)
		}

		// Primary Key on seat_id in confirmed_seat_assignments prevents any duplicate booking
		_, err = tx.Exec(ctx, `
			INSERT INTO confirmed_seat_assignments (seat_id, session_id, booking_id, created_at)
			VALUES ($1, $2, $3, $4)`,
			seat.ID, res.SessionID, booking.ID, now)
		if err != nil {
			return nil, fmt.Errorf("seat already booked: %w", err)
		}
	}

	// 5. Remove active seat holds
	_, err = tx.Exec(ctx, `DELETE FROM active_seat_holds WHERE reservation_id = $1`, res.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to release active hold: %w", err)
	}

	// 6. Update seats to BOOKED
	_, err = tx.Exec(ctx, `
		UPDATE seats 
		SET status = $1, version = version + 1, updated_at = $2 
		WHERE id = ANY($3)`,
		domain.SeatStatusBooked, now, seatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to update seat status to booked: %w", err)
	}

	// 7. Update reservation status to CONFIRMED
	_, err = tx.Exec(ctx, `
		UPDATE reservations 
		SET status = $1, updated_at = $2 
		WHERE id = $3`,
		domain.ReservationStatusConfirmed, now, res.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update reservation status: %w", err)
	}

	// 8. Outbox event
	payload, _ := json.Marshal(map[string]interface{}{
		"booking_id":        booking.ID,
		"booking_reference": booking.BookingReference,
		"reservation_id":    res.ID,
		"user_id":           res.UserID,
		"seat_ids":          seatIDs,
		"total_price":       booking.TotalPriceCents,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 'PENDING', $6)`,
		uuid.New(), "BOOKING", booking.ID, "BookingConfirmed", payload, now)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit confirmation failed: %w", err)
	}

	return booking, nil
}

func (r *PostgresRepository) CancelReservationTx(ctx context.Context, reservationID uuid.UUID, userID uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var res domain.Reservation
	err = tx.QueryRow(ctx, `SELECT id, user_id, status FROM reservations WHERE id = $1 FOR UPDATE`, reservationID).Scan(&res.ID, &res.UserID, &res.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrReservationNotFound
	}
	if err != nil {
		return err
	}

	if res.UserID != userID {
		return domain.ErrUnauthorized
	}
	if res.Status != domain.ReservationStatusPending {
		return fmt.Errorf("cannot cancel reservation in status %s", res.Status)
	}

	now := time.Now()
	// Release seats
	_, err = tx.Exec(ctx, `
		UPDATE seats 
		SET status = 'AVAILABLE', version = version + 1, updated_at = $1 
		WHERE id IN (SELECT seat_id FROM reservation_items WHERE reservation_id = $2)`,
		now, res.ID)
	if err != nil {
		return err
	}

	_, _ = tx.Exec(ctx, `DELETE FROM active_seat_holds WHERE reservation_id = $1`, res.ID)
	_, _ = tx.Exec(ctx, `UPDATE reservations SET status = 'CANCELLED', updated_at = $1 WHERE id = $2`, now, res.ID)

	payload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": res.ID,
		"user_id":        userID,
	})
	_, _ = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, 'RESERVATION', $2, 'ReservationCancelled', $3, 'PENDING', $4)`,
		uuid.New(), res.ID, payload, now)

	return tx.Commit(ctx)
}

func (r *PostgresRepository) ExpireOverdueReservations(ctx context.Context) (int, error) {
	// Find pending reservations that passed expires_at
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM reservations 
		WHERE status = 'PENDING' AND expires_at < NOW() 
		LIMIT 100`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var expiredIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			expiredIDs = append(expiredIDs, id)
		}
	}
	rows.Close()

	count := 0
	for _, id := range expiredIDs {
		tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			continue
		}

		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM reservations WHERE id = $1 FOR UPDATE`, id).Scan(&status)
		if err != nil || status != domain.ReservationStatusPending {
			tx.Rollback(ctx)
			continue
		}

		now := time.Now()
		// Release holds
		_, _ = tx.Exec(ctx, `DELETE FROM active_seat_holds WHERE reservation_id = $1`, id)
		// Reset seat statuses
		_, _ = tx.Exec(ctx, `
			UPDATE seats 
			SET status = 'AVAILABLE', version = version + 1, updated_at = $1 
			WHERE id IN (SELECT seat_id FROM reservation_items WHERE reservation_id = $2)`,
			now, id)
		// Mark reservation expired
		_, _ = tx.Exec(ctx, `UPDATE reservations SET status = 'EXPIRED', updated_at = $1 WHERE id = $2`, now, id)

		payload, _ := json.Marshal(map[string]interface{}{"reservation_id": id})
		_, _ = tx.Exec(ctx, `
			INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
			VALUES ($1, 'RESERVATION', $2, 'ReservationExpired', $3, 'PENDING', $4)`,
			uuid.New(), id, payload, now)

		if err := tx.Commit(ctx); err == nil {
			count++
		}
	}

	return count, nil
}

// Bookings
func (r *PostgresRepository) GetBookingByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	query := `
		SELECT b.id, b.reservation_id, b.session_id, b.user_id, b.booking_reference, b.status, b.total_price_cents,
		       b.confirmed_at, b.cancelled_at, b.created_at, b.updated_at,
		       e.title AS event_title, s.title AS session_title, e.venue, s.start_time
		FROM bookings b
		JOIN sessions s ON s.id = b.session_id
		JOIN events e ON e.id = s.event_id
		WHERE b.id = $1`
	var b domain.Booking
	var cancelledAt sql.NullTime
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&b.ID, &b.ReservationID, &b.SessionID, &b.UserID, &b.BookingReference, &b.Status, &b.TotalPriceCents,
		&b.ConfirmedAt, &cancelledAt, &b.CreatedAt, &b.UpdatedAt,
		&b.EventTitle, &b.SessionTitle, &b.Venue, &b.SessionStartTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBookingNotFound
	}
	if err != nil {
		return nil, err
	}
	if cancelledAt.Valid {
		b.CancelledAt = &cancelledAt.Time
	}

	// Fetch seats
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.session_id, s.row_label, s.seat_number, s.seat_category, bi.price_cents, s.status, s.version, s.created_at, s.updated_at
		FROM booking_items bi
		JOIN seats s ON s.id = bi.seat_id
		WHERE bi.booking_id = $1
		ORDER BY s.row_label ASC, s.seat_number ASC`, b.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s domain.Seat
			if err := rows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err == nil {
				b.Seats = append(b.Seats, s)
			}
		}
	}

	return &b, nil
}

func (r *PostgresRepository) GetBookingByReference(ctx context.Context, ref string) (*domain.Booking, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT id FROM bookings WHERE booking_reference = $1`, ref).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrBookingNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetBookingByID(ctx, id)
}

func (r *PostgresRepository) ListBookingsByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT b.id, b.reservation_id, b.session_id, b.user_id, b.booking_reference, b.status, b.total_price_cents,
		       b.confirmed_at, b.cancelled_at, b.created_at, b.updated_at,
		       e.title AS event_title, s.title AS session_title, e.venue, s.start_time
		FROM bookings b
		JOIN sessions s ON s.id = b.session_id
		JOIN events e ON e.id = s.event_id
		WHERE b.user_id = $1
		ORDER BY b.confirmed_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []domain.Booking
	for rows.Next() {
		var b domain.Booking
		var cancelledAt sql.NullTime
		if err := rows.Scan(
			&b.ID, &b.ReservationID, &b.SessionID, &b.UserID, &b.BookingReference, &b.Status, &b.TotalPriceCents,
			&b.ConfirmedAt, &cancelledAt, &b.CreatedAt, &b.UpdatedAt,
			&b.EventTitle, &b.SessionTitle, &b.Venue, &b.SessionStartTime); err != nil {
			return nil, err
		}
		if cancelledAt.Valid {
			b.CancelledAt = &cancelledAt.Time
		}
		bookings = append(bookings, b)
	}

	for i := range bookings {
		srows, err := r.pool.Query(ctx, `
			SELECT s.id, s.session_id, s.row_label, s.seat_number, s.seat_category, bi.price_cents, s.status, s.version, s.created_at, s.updated_at
			FROM booking_items bi
			JOIN seats s ON s.id = bi.seat_id
			WHERE bi.booking_id = $1`, bookings[i].ID)
		if err == nil {
			for srows.Next() {
				var s domain.Seat
				if err := srows.Scan(&s.ID, &s.SessionID, &s.RowLabel, &s.SeatNumber, &s.SeatCategory, &s.BasePriceCents, &s.Status, &s.Version, &s.CreatedAt, &s.UpdatedAt); err == nil {
					bookings[i].Seats = append(bookings[i].Seats, s)
				}
			}
			srows.Close()
		}
	}

	return bookings, nil
}

func (r *PostgresRepository) CancelBookingTx(ctx context.Context, bookingID uuid.UUID, userID uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var b domain.Booking
	err = tx.QueryRow(ctx, `SELECT id, user_id, status FROM bookings WHERE id = $1 FOR UPDATE`, bookingID).Scan(&b.ID, &b.UserID, &b.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrBookingNotFound
	}
	if err != nil {
		return err
	}

	if b.UserID != userID {
		return domain.ErrUnauthorized
	}
	if b.Status != domain.BookingStatusConfirmed {
		return domain.ErrBookingAlreadyCancelled
	}

	now := time.Now()
	// 1. Remove confirmed assignments
	_, err = tx.Exec(ctx, `DELETE FROM confirmed_seat_assignments WHERE booking_id = $1`, bookingID)
	if err != nil {
		return err
	}

	// 2. Set seats back to AVAILABLE
	_, err = tx.Exec(ctx, `
		UPDATE seats 
		SET status = 'AVAILABLE', version = version + 1, updated_at = $1 
		WHERE id IN (SELECT seat_id FROM booking_items WHERE booking_id = $2)`,
		now, bookingID)
	if err != nil {
		return err
	}

	// 3. Mark booking CANCELLED
	_, err = tx.Exec(ctx, `
		UPDATE bookings 
		SET status = 'CANCELLED', cancelled_at = $1, updated_at = $1 
		WHERE id = $2`,
		now, bookingID)
	if err != nil {
		return err
	}

	// 4. Outbox event
	payload, _ := json.Marshal(map[string]interface{}{
		"booking_id": bookingID,
		"user_id":    userID,
	})
	_, _ = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, created_at)
		VALUES ($1, 'BOOKING', $2, 'BookingCancelled', $3, 'PENDING', $4)`,
		uuid.New(), bookingID, payload, now)

	return tx.Commit(ctx)
}

// Payments
func (r *PostgresRepository) CreatePaymentTx(ctx context.Context, p *domain.Payment) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err := r.pool.Exec(ctx, `
		INSERT INTO payments (id, reservation_id, booking_id, user_id, amount_cents, currency, status, idempotency_key, provider_ref, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.ID, p.ReservationID, p.BookingID, p.UserID, p.AmountCents, p.Currency, p.Status, p.IdempotencyKey, p.ProviderRef, p.ErrorMessage, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpdatePaymentStatus(ctx context.Context, paymentID uuid.UUID, status string, providerRef string, errMsg string, bookingID *uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE payments 
		SET status = $1, provider_ref = COALESCE(NULLIF($2, ''), provider_ref), error_message = $3, booking_id = COALESCE($4, booking_id), updated_at = NOW() 
		WHERE id = $5`,
		status, providerRef, errMsg, bookingID, paymentID)
	return err
}

func (r *PostgresRepository) GetPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	var p domain.Payment
	err := r.pool.QueryRow(ctx, `
		SELECT id, reservation_id, booking_id, user_id, amount_cents, currency, status, idempotency_key, COALESCE(provider_ref, ''), COALESCE(error_message, ''), created_at, updated_at 
		FROM payments WHERE id = $1`, id).Scan(
		&p.ID, &p.ReservationID, &p.BookingID, &p.UserID, &p.AmountCents, &p.Currency, &p.Status, &p.IdempotencyKey, &p.ProviderRef, &p.ErrorMessage, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("payment not found")
	}
	return &p, err
}

// Idempotency
func (r *PostgresRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	err := r.pool.QueryRow(ctx, `
		SELECT key, user_id, request_fingerprint, response_code, response_body, created_at, expires_at 
		FROM idempotency_keys WHERE key = $1`, key).Scan(
		&rec.Key, &rec.UserID, &rec.RequestFingerprint, &rec.ResponseCode, &rec.ResponseBody, &rec.CreatedAt, &rec.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PostgresRepository) SaveIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (key, user_id, request_fingerprint, response_code, response_body, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (key) DO UPDATE 
		SET response_code = EXCLUDED.response_code, response_body = EXCLUDED.response_body, expires_at = EXCLUDED.expires_at`,
		record.Key, record.UserID, record.RequestFingerprint, record.ResponseCode, record.ResponseBody, record.CreatedAt, record.ExpiresAt)
	return err
}

// Transactional Outbox
func (r *PostgresRepository) FetchPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, payload, status, retry_count, created_at, published_at 
		FROM outbox_events 
		WHERE status = 'PENDING' 
		ORDER BY created_at ASC 
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		var pubAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &e.Payload, &e.Status, &e.RetryCount, &e.CreatedAt, &pubAt); err != nil {
			return nil, err
		}
		if pubAt.Valid {
			e.PublishedAt = &pubAt.Time
		}
		events = append(events, e)
	}
	return events, nil
}

func (r *PostgresRepository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox_events SET status = 'PUBLISHED', published_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *PostgresRepository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox_events SET retry_count = retry_count + 1 WHERE id = $1`, id)
	return err
}

// Audit Logs
func (r *PostgresRepository) CreateAuditLog(ctx context.Context, log *domain.AuditLog) error {
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	log.CreatedAt = time.Now()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, user_id, action, entity_type, entity_id, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
		log.ID, log.UserID, log.Action, log.EntityType, log.EntityID, log.Metadata, log.CreatedAt)
	return err
}
