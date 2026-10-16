package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/domain"
)

type MemoryRepository struct {
	mu           sync.RWMutex
	users        map[uuid.UUID]*domain.User
	usersByEmail map[string]*domain.User

	events   map[uuid.UUID]*domain.Event
	sessions map[uuid.UUID]*domain.Session
	seats    map[uuid.UUID]*domain.Seat

	// Concurrency control structures:
	seatLocks              map[uuid.UUID]*sync.Mutex
	activeSeatHolds        map[uuid.UUID]uuid.UUID // seatID -> reservationID
	confirmedAssignments  map[uuid.UUID]uuid.UUID // seatID -> bookingID

	reservations      map[uuid.UUID]*domain.Reservation
	reservationItems  map[uuid.UUID][]domain.ReservationItem
	bookings          map[uuid.UUID]*domain.Booking
	bookingItems      map[uuid.UUID][]domain.BookingItem
	payments          map[uuid.UUID]*domain.Payment
	idempotencyKeys   map[string]*domain.IdempotencyRecord
	outboxEvents      []*domain.OutboxEvent
	auditLogs         []*domain.AuditLog
}

func NewMemoryRepository() *MemoryRepository {
	repo := &MemoryRepository{
		users:                make(map[uuid.UUID]*domain.User),
		usersByEmail:         make(map[string]*domain.User),
		events:               make(map[uuid.UUID]*domain.Event),
		sessions:             make(map[uuid.UUID]*domain.Session),
		seats:                make(map[uuid.UUID]*domain.Seat),
		seatLocks:            make(map[uuid.UUID]*sync.Mutex),
		activeSeatHolds:      make(map[uuid.UUID]uuid.UUID),
		confirmedAssignments: make(map[uuid.UUID]uuid.UUID),
		reservations:         make(map[uuid.UUID]*domain.Reservation),
		reservationItems:     make(map[uuid.UUID][]domain.ReservationItem),
		bookings:             make(map[uuid.UUID]*domain.Booking),
		bookingItems:         make(map[uuid.UUID][]domain.BookingItem),
		payments:             make(map[uuid.UUID]*domain.Payment),
		idempotencyKeys:      make(map[string]*domain.IdempotencyRecord),
		outboxEvents:         make([]*domain.OutboxEvent, 0),
		auditLogs:            make([]*domain.AuditLog, 0),
	}
	repo.seed()
	return repo
}

func (m *MemoryRepository) seed() {
	now := time.Now()

	// Seed Users
	uAdmin := &domain.User{
		ID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Email:        "admin@example.com",
		FullName:     "System Administrator",
		PasswordHash: "$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l",
		Role:         domain.RoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	uAlice := &domain.User{
		ID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Email:        "user1@example.com",
		FullName:     "Alice Engineer",
		PasswordHash: "$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l",
		Role:         domain.RoleCustomer,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	uBob := &domain.User{
		ID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Email:        "user2@example.com",
		FullName:     "Bob Architect",
		PasswordHash: "$2a$10$w8I6tTms1bYF7mQ8.m0p3eR1o5p19iPqA0r3g6N8b5f3d7h9j1k2l",
		Role:         domain.RoleCustomer,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.users[uAdmin.ID] = uAdmin
	m.usersByEmail[uAdmin.Email] = uAdmin
	m.users[uAlice.ID] = uAlice
	m.usersByEmail[uAlice.Email] = uAlice
	m.users[uBob.ID] = uBob
	m.usersByEmail[uBob.Email] = uBob

	// Seed Events
	e1 := &domain.Event{
		ID:          uuid.MustParse("a0000000-0000-0000-0000-000000000001"),
		Title:       "Global Tech Summit 2026",
		Description: "The premier international conference on distributed computing, systems architecture, and AI engineering.",
		Venue:       "Grand Convention Center, San Francisco",
		Category:    "Conference",
		BannerURL:   "https://images.unsplash.com/photo-1540575467063-178a50c2df87?auto=format&fit=crop&w=1200&q=80",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	e2 := &domain.Event{
		ID:          uuid.MustParse("a0000000-0000-0000-0000-000000000002"),
		Title:       "Symphony Under the Stars",
		Description: "An enchanting evening of classical masterpieces performed by the National Symphony Orchestra.",
		Venue:       "Royal Philharmonic Hall, London",
		Category:    "Concert",
		BannerURL:   "https://images.unsplash.com/photo-1514525253161-7a46d19cd819?auto=format&fit=crop&w=1200&q=80",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	e3 := &domain.Event{
		ID:          uuid.MustParse("a0000000-0000-0000-0000-000000000003"),
		Title:       "Championship Esports Arena Finals",
		Description: "The world championship grand finals featuring elite teams battling in tactical strategy games.",
		Venue:       "Cyber Arena, Tokyo",
		Category:    "Esports",
		BannerURL:   "https://images.unsplash.com/photo-1511512578047-dfb367046420?auto=format&fit=crop&w=1200&q=80",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.events[e1.ID] = e1
	m.events[e2.ID] = e2
	m.events[e3.ID] = e3

	// Seed Sessions
	sessList := []*domain.Session{
		{
			ID:        uuid.MustParse("b0000000-0000-0000-0000-000000000001"),
			EventID:   e1.ID,
			Title:     "Day 1 - Keynote & Systems Track",
			StartTime: now.Add(48 * time.Hour),
			EndTime:   now.Add(56 * time.Hour),
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.MustParse("b0000000-0000-0000-0000-000000000002"),
			EventID:   e1.ID,
			Title:     "Day 2 - AI Infrastructure Track",
			StartTime: now.Add(72 * time.Hour),
			EndTime:   now.Add(80 * time.Hour),
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.MustParse("b0000000-0000-0000-0000-000000000003"),
			EventID:   e2.ID,
			Title:     "Evening Gala Performance",
			StartTime: now.Add(120 * time.Hour),
			EndTime:   now.Add(123 * time.Hour),
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.MustParse("b0000000-0000-0000-0000-000000000004"),
			EventID:   e3.ID,
			Title:     "Grand Championship Finals",
			StartTime: now.Add(168 * time.Hour),
			EndTime:   now.Add(173 * time.Hour),
			Status:    "ACTIVE",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	for _, s := range sessList {
		m.sessions[s.ID] = s
		// Generate Seats for each session:
		// Row A: VIP ($250, 8 seats)
		for num := 1; num <= 8; num++ {
			sid := uuid.New()
			seat := &domain.Seat{
				ID:             sid,
				SessionID:      s.ID,
				RowLabel:       "A",
				SeatNumber:     num,
				SeatCategory:   "VIP",
				BasePriceCents: 25000,
				Status:         domain.SeatStatusAvailable,
				Version:        1,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			m.seats[sid] = seat
			m.seatLocks[sid] = &sync.Mutex{}
		}

		// Row B: PREMIUM ($150, 10 seats)
		for num := 1; num <= 10; num++ {
			sid := uuid.New()
			seat := &domain.Seat{
				ID:             sid,
				SessionID:      s.ID,
				RowLabel:       "B",
				SeatNumber:     num,
				SeatCategory:   "PREMIUM",
				BasePriceCents: 15000,
				Status:         domain.SeatStatusAvailable,
				Version:        1,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			m.seats[sid] = seat
			m.seatLocks[sid] = &sync.Mutex{}
		}

		// Rows C & D: STANDARD ($95, 10 seats each)
		for _, row := range []string{"C", "D"} {
			for num := 1; num <= 10; num++ {
				sid := uuid.New()
				seat := &domain.Seat{
					ID:             sid,
					SessionID:      s.ID,
					RowLabel:       row,
					SeatNumber:     num,
					SeatCategory:   "STANDARD",
					BasePriceCents: 9500,
					Status:         domain.SeatStatusAvailable,
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				m.seats[sid] = seat
				m.seatLocks[sid] = &sync.Mutex{}
			}
		}

		// Row E: BALCONY ($65, 12 seats)
		for num := 1; num <= 12; num++ {
			sid := uuid.New()
			seat := &domain.Seat{
				ID:             sid,
				SessionID:      s.ID,
				RowLabel:       "E",
				SeatNumber:     num,
				SeatCategory:   "BALCONY",
				BasePriceCents: 6500,
				Status:         domain.SeatStatusAvailable,
				Version:        1,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			m.seats[sid] = seat
			m.seatLocks[sid] = &sync.Mutex{}
		}
	}
}

func (m *MemoryRepository) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryRepository) Close() {}

func (m *MemoryRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.usersByEmail[email]
	if !ok {
		return nil, domain.ErrInvalidCredentials
	}
	copyUser := *u
	return &copyUser, nil
}

func (m *MemoryRepository) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	copyUser := *u
	return &copyUser, nil
}

func (m *MemoryRepository) CreateUser(ctx context.Context, user *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	m.users[user.ID] = user
	m.usersByEmail[user.Email] = user
	return nil
}

func (m *MemoryRepository) ListEvents(ctx context.Context) ([]domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]domain.Event, 0, len(m.events))
	for _, e := range m.events {
		eventCopy := *e
		// attach sessions
		var sessList []domain.Session
		for _, s := range m.sessions {
			if s.EventID == e.ID {
				sc := *s
				// count available seats
				total := 0
				avail := 0
				for _, seat := range m.seats {
					if seat.SessionID == s.ID {
						total++
						if seat.Status == domain.SeatStatusAvailable {
							avail++
						}
					}
				}
				sc.TotalSeats = total
				sc.AvailableSeats = avail
				sessList = append(sessList, sc)
			}
		}
		sort.Slice(sessList, func(i, j int) bool {
			return sessList[i].StartTime.Before(sessList[j].StartTime)
		})
		eventCopy.Sessions = sessList
		list = append(list, eventCopy)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Title < list[j].Title
	})
	return list, nil
}

func (m *MemoryRepository) GetEventByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.events[id]
	if !ok {
		return nil, errors.New("event not found")
	}
	copyEvent := *e
	var sessList []domain.Session
	for _, s := range m.sessions {
		if s.EventID == e.ID {
			sc := *s
			total := 0
			avail := 0
			for _, seat := range m.seats {
				if seat.SessionID == s.ID {
					total++
					if seat.Status == domain.SeatStatusAvailable {
						avail++
					}
				}
			}
			sc.TotalSeats = total
			sc.AvailableSeats = avail
			sessList = append(sessList, sc)
		}
	}
	sort.Slice(sessList, func(i, j int) bool {
		return sessList[i].StartTime.Before(sessList[j].StartTime)
	})
	copyEvent.Sessions = sessList
	return &copyEvent, nil
}

func (m *MemoryRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*domain.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	sc := *s
	total := 0
	avail := 0
	for _, seat := range m.seats {
		if seat.SessionID == s.ID {
			total++
			if seat.Status == domain.SeatStatusAvailable {
				avail++
			}
		}
	}
	sc.TotalSeats = total
	sc.AvailableSeats = avail
	return &sc, nil
}

func (m *MemoryRepository) ListSessionsByEventID(ctx context.Context, eventID uuid.UUID) ([]domain.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []domain.Session
	for _, s := range m.sessions {
		if s.EventID == eventID {
			sc := *s
			total := 0
			avail := 0
			for _, seat := range m.seats {
				if seat.SessionID == s.ID {
					total++
					if seat.Status == domain.SeatStatusAvailable {
						avail++
					}
				}
			}
			sc.TotalSeats = total
			sc.AvailableSeats = avail
			list = append(list, sc)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].StartTime.Before(list[j].StartTime)
	})
	return list, nil
}

func (m *MemoryRepository) ListSeatsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domain.Seat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []domain.Seat
	for _, s := range m.seats {
		if s.SessionID == sessionID {
			list = append(list, *s)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].RowLabel == list[j].RowLabel {
			return list[i].SeatNumber < list[j].SeatNumber
		}
		return list[i].RowLabel < list[j].RowLabel
	})
	return list, nil
}

func (m *MemoryRepository) GetSeatByID(ctx context.Context, id uuid.UUID) (*domain.Seat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.seats[id]
	if !ok {
		return nil, domain.ErrSeatNotFound
	}
	sc := *s
	return &sc, nil
}

// CreateReservationTx simulates PostgreSQL atomic transaction with row locking
func (m *MemoryRepository) CreateReservationTx(ctx context.Context, res *domain.Reservation, seatIDs []uuid.UUID) (*domain.Reservation, error) {
	if len(seatIDs) == 0 {
		return nil, domain.ErrEmptySeatSelection
	}

	// 1. Sort requested seat IDs to avoid deadlocks across concurrent requests
	sortedSeatIDs := make([]uuid.UUID, len(seatIDs))
	copy(sortedSeatIDs, seatIDs)
	sort.Slice(sortedSeatIDs, func(i, j int) bool {
		return sortedSeatIDs[i].String() < sortedSeatIDs[j].String()
	})

	// 2. Look up row-level mutexes deterministically under RLock
	m.mu.RLock()
	var locks []*sync.Mutex
	for _, sid := range sortedSeatIDs {
		lk, exists := m.seatLocks[sid]
		if !exists {
			m.mu.RUnlock()
			return nil, domain.ErrSeatNotFound
		}
		locks = append(locks, lk)
	}
	m.mu.RUnlock()

	// Lock all seats in order (without holding m.mu so we never deadlock)
	for _, lk := range locks {
		lk.Lock()
	}
	defer func() {
		for _, lk := range locks {
			lk.Unlock()
		}
	}()

	// 3. Acquire write lock to check statuses and mutate database state
	m.mu.Lock()
	defer m.mu.Unlock()

	var lockedSeats []domain.Seat
	totalPrice := 0
	for _, sid := range sortedSeatIDs {
		seat, ok := m.seats[sid]
		if !ok {
			return nil, domain.ErrSeatNotFound
		}
		// If seat is not available OR already held/assigned, fail atomically
		if seat.Status != domain.SeatStatusAvailable {
			return nil, domain.ErrSeatUnavailable
		}
		if _, held := m.activeSeatHolds[sid]; held {
			return nil, domain.ErrSeatUnavailable
		}
		if _, booked := m.confirmedAssignments[sid]; booked {
			return nil, domain.ErrSeatUnavailable
		}
		if seat.SessionID != res.SessionID {
			return nil, fmt.Errorf("seat %s belongs to a different session", seat.ID)
		}
		totalPrice += seat.BasePriceCents
		lockedSeats = append(lockedSeats, *seat)
	}

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

	// Persist reservation & items
	m.reservations[res.ID] = res
	var items []domain.ReservationItem
	for _, s := range lockedSeats {
		items = append(items, domain.ReservationItem{
			ID:            uuid.New(),
			ReservationID: res.ID,
			SeatID:        s.ID,
			PriceCents:    s.BasePriceCents,
			CreatedAt:     now,
		})
		// Record active seat hold
		m.activeSeatHolds[s.ID] = res.ID
		// Mark seat HELD
		seatRef := m.seats[s.ID]
		seatRef.Status = domain.SeatStatusHeld
		seatRef.Version++
		seatRef.UpdatedAt = now
	}
	m.reservationItems[res.ID] = items

	// Transactional outbox event
	payload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": res.ID,
		"session_id":     res.SessionID,
		"user_id":        res.UserID,
		"expires_at":     res.ExpiresAt,
		"seat_ids":       sortedSeatIDs,
		"total_price":    res.TotalPriceCents,
	})
	m.outboxEvents = append(m.outboxEvents, &domain.OutboxEvent{
		ID:            uuid.New(),
		AggregateType: "RESERVATION",
		AggregateID:   res.ID,
		EventType:     "ReservationCreated",
		Payload:       payload,
		Status:        "PENDING",
		CreatedAt:     now,
	})

	copyRes := *res
	return &copyRes, nil
}

func (m *MemoryRepository) GetReservationByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, ok := m.reservations[id]
	if !ok {
		return nil, domain.ErrReservationNotFound
	}

	// Auto check expiry
	now := time.Now()
	if res.Status == domain.ReservationStatusPending && now.After(res.ExpiresAt) {
		res.Status = domain.ReservationStatusExpired
		// release holds
		for _, item := range m.reservationItems[id] {
			delete(m.activeSeatHolds, item.SeatID)
			if seat, exists := m.seats[item.SeatID]; exists {
				seat.Status = domain.SeatStatusAvailable
				seat.Version++
				seat.UpdatedAt = now
			}
		}
	}

	copyRes := *res
	var seats []domain.Seat
	for _, item := range m.reservationItems[id] {
		if s, exists := m.seats[item.SeatID]; exists {
			seats = append(seats, *s)
		}
	}
	copyRes.Seats = seats
	left := time.Until(res.ExpiresAt).Seconds()
	if left < 0 {
		left = 0
	}
	copyRes.SecondsLeft = int64(left)

	return &copyRes, nil
}

func (m *MemoryRepository) ConfirmReservationTx(ctx context.Context, reservationID uuid.UUID, bookingRef string) (*domain.Booking, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, ok := m.reservations[reservationID]
	if !ok {
		return nil, domain.ErrReservationNotFound
	}

	if res.Status == domain.ReservationStatusConfirmed {
		return nil, domain.ErrReservationAlreadyConfirmed
	}
	if res.Status != domain.ReservationStatusPending {
		return nil, domain.ErrReservationNotPending
	}

	now := time.Now()
	if now.After(res.ExpiresAt) {
		res.Status = domain.ReservationStatusExpired
		// release holds
		for _, item := range m.reservationItems[reservationID] {
			delete(m.activeSeatHolds, item.SeatID)
			if seat, exists := m.seats[item.SeatID]; exists {
				seat.Status = domain.SeatStatusAvailable
				seat.Version++
				seat.UpdatedAt = now
			}
		}
		return nil, domain.ErrReservationExpired
	}

	items := m.reservationItems[reservationID]
	var seats []domain.Seat
	var seatIDs []uuid.UUID
	for _, item := range items {
		seat, exists := m.seats[item.SeatID]
		if !exists {
			return nil, errors.New("seat missing")
		}
		seats = append(seats, *seat)
		seatIDs = append(seatIDs, seat.ID)
	}

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

	// Fetch event details for display
	if sess, ok := m.sessions[res.SessionID]; ok {
		booking.SessionTitle = sess.Title
		booking.SessionStartTime = sess.StartTime
		if ev, ok := m.events[sess.EventID]; ok {
			booking.EventTitle = ev.Title
			booking.Venue = ev.Venue
		}
	}

	// Save booking & confirmed seat assignments
	m.bookings[booking.ID] = booking
	for _, s := range seats {
		delete(m.activeSeatHolds, s.ID)
		m.confirmedAssignments[s.ID] = booking.ID
		seatRef := m.seats[s.ID]
		seatRef.Status = domain.SeatStatusBooked
		seatRef.Version++
		seatRef.UpdatedAt = now
	}

	res.Status = domain.ReservationStatusConfirmed
	res.UpdatedAt = now

	// Outbox event
	payload, _ := json.Marshal(map[string]interface{}{
		"booking_id":        booking.ID,
		"booking_reference": booking.BookingReference,
		"reservation_id":    res.ID,
		"user_id":           res.UserID,
		"seat_ids":          seatIDs,
		"total_price":       booking.TotalPriceCents,
	})
	m.outboxEvents = append(m.outboxEvents, &domain.OutboxEvent{
		ID:            uuid.New(),
		AggregateType: "BOOKING",
		AggregateID:   booking.ID,
		EventType:     "BookingConfirmed",
		Payload:       payload,
		Status:        "PENDING",
		CreatedAt:     now,
	})

	copyBooking := *booking
	return &copyBooking, nil
}

func (m *MemoryRepository) CancelReservationTx(ctx context.Context, reservationID uuid.UUID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, ok := m.reservations[reservationID]
	if !ok {
		return domain.ErrReservationNotFound
	}
	if res.UserID != userID {
		return domain.ErrUnauthorized
	}
	if res.Status != domain.ReservationStatusPending {
		return fmt.Errorf("cannot cancel reservation in status %s", res.Status)
	}

	now := time.Now()
	res.Status = domain.ReservationStatusCancelled
	res.UpdatedAt = now

	for _, item := range m.reservationItems[reservationID] {
		delete(m.activeSeatHolds, item.SeatID)
		if seat, exists := m.seats[item.SeatID]; exists {
			seat.Status = domain.SeatStatusAvailable
			seat.Version++
			seat.UpdatedAt = now
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": res.ID,
		"user_id":        userID,
	})
	m.outboxEvents = append(m.outboxEvents, &domain.OutboxEvent{
		ID:            uuid.New(),
		AggregateType: "RESERVATION",
		AggregateID:   res.ID,
		EventType:     "ReservationCancelled",
		Payload:       payload,
		Status:        "PENDING",
		CreatedAt:     now,
	})

	return nil
}

func (m *MemoryRepository) ExpireOverdueReservations(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0
	for id, res := range m.reservations {
		if res.Status == domain.ReservationStatusPending && now.After(res.ExpiresAt) {
			res.Status = domain.ReservationStatusExpired
			res.UpdatedAt = now
			for _, item := range m.reservationItems[id] {
				delete(m.activeSeatHolds, item.SeatID)
				if seat, exists := m.seats[item.SeatID]; exists {
					seat.Status = domain.SeatStatusAvailable
					seat.Version++
					seat.UpdatedAt = now
				}
			}

			payload, _ := json.Marshal(map[string]interface{}{"reservation_id": id})
			m.outboxEvents = append(m.outboxEvents, &domain.OutboxEvent{
				ID:            uuid.New(),
				AggregateType: "RESERVATION",
				AggregateID:   id,
				EventType:     "ReservationExpired",
				Payload:       payload,
				Status:        "PENDING",
				CreatedAt:     now,
			})
			count++
		}
	}
	return count, nil
}

func (m *MemoryRepository) GetBookingByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bookings[id]
	if !ok {
		return nil, domain.ErrBookingNotFound
	}
	copyBooking := *b
	return &copyBooking, nil
}

func (m *MemoryRepository) GetBookingByReference(ctx context.Context, ref string) (*domain.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, b := range m.bookings {
		if b.BookingReference == ref {
			copyBooking := *b
			return &copyBooking, nil
		}
	}
	return nil, domain.ErrBookingNotFound
}

func (m *MemoryRepository) ListBookingsByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []domain.Booking
	for _, b := range m.bookings {
		if b.UserID == userID {
			list = append(list, *b)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ConfirmedAt.After(list[j].ConfirmedAt)
	})
	if offset > len(list) {
		return []domain.Booking{}, nil
	}
	end := offset + limit
	if end > len(list) || limit <= 0 {
		end = len(list)
	}
	return list[offset:end], nil
}

func (m *MemoryRepository) CancelBookingTx(ctx context.Context, bookingID uuid.UUID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.bookings[bookingID]
	if !ok {
		return domain.ErrBookingNotFound
	}
	if b.UserID != userID {
		return domain.ErrUnauthorized
	}
	if b.Status != domain.BookingStatusConfirmed {
		return domain.ErrBookingAlreadyCancelled
	}

	now := time.Now()
	b.Status = domain.BookingStatusCancelled
	b.CancelledAt = &now
	b.UpdatedAt = now

	for _, s := range b.Seats {
		delete(m.confirmedAssignments, s.ID)
		if seat, exists := m.seats[s.ID]; exists {
			seat.Status = domain.SeatStatusAvailable
			seat.Version++
			seat.UpdatedAt = now
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"booking_id": bookingID,
		"user_id":    userID,
	})
	m.outboxEvents = append(m.outboxEvents, &domain.OutboxEvent{
		ID:            uuid.New(),
		AggregateType: "BOOKING",
		AggregateID:   bookingID,
		EventType:     "BookingCancelled",
		Payload:       payload,
		Status:        "PENDING",
		CreatedAt:     now,
	})

	return nil
}

func (m *MemoryRepository) CreatePaymentTx(ctx context.Context, p *domain.Payment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	m.payments[p.ID] = p
	return nil
}

func (m *MemoryRepository) UpdatePaymentStatus(ctx context.Context, paymentID uuid.UUID, status string, providerRef string, errMsg string, bookingID *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.payments[paymentID]
	if !ok {
		return errors.New("payment not found")
	}
	p.Status = status
	if providerRef != "" {
		p.ProviderRef = providerRef
	}
	p.ErrorMessage = errMsg
	if bookingID != nil {
		p.BookingID = bookingID
	}
	p.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryRepository) GetPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.payments[id]
	if !ok {
		return nil, errors.New("payment not found")
	}
	copyPayment := *p
	return &copyPayment, nil
}

func (m *MemoryRepository) GetIdempotencyRecord(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.idempotencyKeys[key]
	if !ok {
		return nil, nil
	}
	copyRec := *rec
	return &copyRec, nil
}

func (m *MemoryRepository) SaveIdempotencyRecord(ctx context.Context, record *domain.IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idempotencyKeys[record.Key] = record
	return nil
}

func (m *MemoryRepository) FetchPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []domain.OutboxEvent
	for _, e := range m.outboxEvents {
		if e.Status == "PENDING" {
			list = append(list, *e)
			if limit > 0 && len(list) >= limit {
				break
			}
		}
	}
	return list, nil
}

func (m *MemoryRepository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.outboxEvents {
		if e.ID == id {
			e.Status = "PUBLISHED"
			now := time.Now()
			e.PublishedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.outboxEvents {
		if e.ID == id {
			e.RetryCount++
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) CreateAuditLog(ctx context.Context, log *domain.AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if log.ID == uuid.Nil {
		log.ID = uuid.New()
	}
	log.CreatedAt = time.Now()
	m.auditLogs = append(m.auditLogs, log)
	return nil
}
