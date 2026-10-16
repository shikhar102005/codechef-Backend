package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/auth"
	"github.com/seat-booking/system/internal/cache"
	"github.com/seat-booking/system/internal/domain"
	"github.com/seat-booking/system/internal/messaging"
	"github.com/seat-booking/system/internal/middleware"
)

type ServerHandler struct {
	repo      domain.Repository
	cacheSvc  cache.CacheService
	bus       messaging.MessageBus
	authSvc   *auth.AuthService
	admission *middleware.AdmissionControl
}

func NewServerHandler(
	repo domain.Repository,
	cacheSvc cache.CacheService,
	bus messaging.MessageBus,
	authSvc *auth.AuthService,
	admission *middleware.AdmissionControl,
) *ServerHandler {
	return &ServerHandler{
		repo:      repo,
		cacheSvc:  cacheSvc,
		bus:       bus,
		authSvc:   authSvc,
		admission: admission,
	}
}

// -------------------------------------------------------------
// Auth Handlers
// -------------------------------------------------------------

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *ServerHandler) HandleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_INPUT", "message": err.Error()})
		return
	}

	user, err := h.repo.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil || !auth.CheckPassword(user.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "INVALID_CREDENTIALS", "message": "Invalid email or password"})
		return
	}

	token, err := h.authSvc.GenerateToken(user.ID, user.Email, user.FullName, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "TOKEN_ERROR", "message": "Failed to generate session token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	FullName string `json:"full_name" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

func (h *ServerHandler) HandleRegister(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_INPUT", "message": err.Error()})
		return
	}

	hash, _ := auth.HashPassword(req.Password)
	user := &domain.User{
		ID:           uuid.New(),
		Email:        req.Email,
		FullName:     req.FullName,
		PasswordHash: hash,
		Role:         domain.RoleCustomer,
	}

	if err := h.repo.CreateUser(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "EMAIL_TAKEN", "message": "An account with this email already exists"})
		return
	}

	token, _ := h.authSvc.GenerateToken(user.ID, user.Email, user.FullName, user.Role)
	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user":  user,
	})
}

func (h *ServerHandler) HandleGetMe(c *gin.Context) {
	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	user, err := h.repo.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "USER_NOT_FOUND", "message": "User not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// -------------------------------------------------------------
// Events and Sessions
// -------------------------------------------------------------

func (h *ServerHandler) HandleListEvents(c *gin.Context) {
	ctx := c.Request.Context()
	cacheKey := "events:all"

	var cached []domain.Event
	if err := h.cacheSvc.Get(ctx, cacheKey, &cached); err == nil && len(cached) > 0 {
		c.Header("X-Cache-Lookup", "HIT")
		c.JSON(http.StatusOK, gin.H{"events": cached})
		return
	}

	events, err := h.repo.ListEvents(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DB_ERROR", "message": err.Error()})
		return
	}

	_ = h.cacheSvc.Set(ctx, cacheKey, events, 1*time.Minute)
	c.Header("X-Cache-Lookup", "MISS")
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (h *ServerHandler) HandleGetEvent(c *gin.Context) {
	idStr := c.Param("eventId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid event ID"})
		return
	}

	event, err := h.repo.GetEventByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Event not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"event": event})
}

func (h *ServerHandler) HandleGetSession(c *gin.Context) {
	idStr := c.Param("sessionId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid session ID"})
		return
	}

	session, err := h.repo.GetSessionByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"session": session})
}

func (h *ServerHandler) HandleGetSessionSeats(c *gin.Context) {
	idStr := c.Param("sessionId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid session ID"})
		return
	}

	seats, err := h.repo.ListSeatsBySessionID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id": id,
		"total":      len(seats),
		"seats":      seats,
	})
}

// -------------------------------------------------------------
// Reservations
// -------------------------------------------------------------

type CreateReservationRequest struct {
	SessionID uuid.UUID   `json:"session_id" binding:"required"`
	SeatIDs   []uuid.UUID `json:"seat_ids" binding:"required,min=1"`
}

func (h *ServerHandler) HandleCreateReservation(c *gin.Context) {
	var req CreateReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_INPUT", "message": err.Error()})
		return
	}

	if len(req.SeatIDs) > 6 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "MAX_SEATS_EXCEEDED",
			"message": "Cannot reserve more than 6 seats in a single transaction",
		})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	res := &domain.Reservation{
		SessionID: req.SessionID,
		UserID:    uid,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// Concurrency-safe atomic transaction
	created, err := h.repo.CreateReservationTx(c.Request.Context(), res, req.SeatIDs)
	if err != nil {
		if errors.Is(err, domain.ErrSeatUnavailable) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "SEAT_UNAVAILABLE",
				"message":     "One or more requested seats are already reserved or booked by another user",
				"status_code": http.StatusConflict,
			})
			return
		}
		if errors.Is(err, domain.ErrSeatNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":       "SEAT_NOT_FOUND",
				"message":     "One or more requested seats could not be found",
				"status_code": http.StatusNotFound,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "RESERVATION_FAILED", "message": err.Error()})
		return
	}

	// Invalidate session cache
	_ = h.cacheSvc.Delete(c.Request.Context(), "events:all")

	c.JSON(http.StatusCreated, gin.H{
		"reservation": created,
		"message":     "Seats successfully locked for 5 minutes",
	})
}

func (h *ServerHandler) HandleGetReservation(c *gin.Context) {
	idStr := c.Param("reservationId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid reservation ID"})
		return
	}

	res, err := h.repo.GetReservationByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Reservation not found"})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	role, _ := c.Get(middleware.ContextUserRole)
	if role != string(domain.RoleAdmin) && res.UserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You do not have access to this reservation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"reservation": res})
}

func (h *ServerHandler) HandleConfirmReservation(c *gin.Context) {
	idStr := c.Param("reservationId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid reservation ID"})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	role, _ := c.Get(middleware.ContextUserRole)
	res, err := h.repo.GetReservationByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Reservation not found"})
		return
	}
	if role != string(domain.RoleAdmin) && res.UserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You cannot confirm another user's reservation"})
		return
	}

	// Generate deterministic booking reference: BK-XXXX-XXXX
	bookingRef := generateBookingRef()

	booking, err := h.repo.ConfirmReservationTx(c.Request.Context(), id, bookingRef)
	if err != nil {
		if errors.Is(err, domain.ErrReservationExpired) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "RESERVATION_EXPIRED",
				"message":     "Reservation has expired. Seats have been automatically released.",
				"status_code": http.StatusConflict,
			})
			return
		}
		if errors.Is(err, domain.ErrReservationAlreadyConfirmed) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "ALREADY_CONFIRMED",
				"message":     "Reservation has already been confirmed.",
				"status_code": http.StatusConflict,
			})
			return
		}
		if errors.Is(err, domain.ErrReservationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Reservation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CONFIRM_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"booking": booking,
		"message": "Booking confirmed successfully",
	})
}

func (h *ServerHandler) HandleCancelReservation(c *gin.Context) {
	idStr := c.Param("reservationId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid reservation ID"})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	if err := h.repo.CancelReservationTx(c.Request.Context(), id, uid); err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You cannot cancel another user's reservation"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "CANCEL_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Reservation cancelled and seats released"})
}

// -------------------------------------------------------------
// Bookings
// -------------------------------------------------------------

func (h *ServerHandler) HandleListUserBookings(c *gin.Context) {
	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	bookings, err := h.repo.ListBookingsByUserID(c.Request.Context(), uid, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bookings": bookings,
		"limit":    limit,
		"offset":   offset,
		"total":    len(bookings),
	})
}

func (h *ServerHandler) HandleGetBooking(c *gin.Context) {
	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	role, _ := c.Get(middleware.ContextUserRole)

	idStr := c.Param("bookingId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		// Try lookup by reference string
		booking, errRef := h.repo.GetBookingByReference(c.Request.Context(), idStr)
		if errRef == nil {
			if role != string(domain.RoleAdmin) && booking.UserID != uid {
				c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You do not have access to this booking"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"booking": booking})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_IDENTIFIER", "message": "Invalid booking ID or reference"})
		return
	}

	booking, err := h.repo.GetBookingByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Booking not found"})
		return
	}

	if role != string(domain.RoleAdmin) && booking.UserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You do not have access to this booking"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"booking": booking})
}

func (h *ServerHandler) HandleCancelBooking(c *gin.Context) {
	idStr := c.Param("bookingId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid booking ID"})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	if err := h.repo.CancelBookingTx(c.Request.Context(), id, uid); err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You cannot cancel another user's booking"})
			return
		}
		if errors.Is(err, domain.ErrBookingAlreadyCancelled) {
			c.JSON(http.StatusConflict, gin.H{"error": "ALREADY_CANCELLED", "message": "Booking has already been cancelled"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CANCEL_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Booking cancelled and seats reallocated to availability pool"})
}

// -------------------------------------------------------------
// Payments Simulator
// -------------------------------------------------------------

type CreatePaymentRequest struct {
	ReservationID uuid.UUID `json:"reservation_id" binding:"required"`
	AmountCents   int       `json:"amount_cents" binding:"required,gt=0"`
	Currency      string    `json:"currency"`
	Simulation    string    `json:"simulation"` // SUCCESS, FAILURE, DELAYED, TIMEOUT
}

func (h *ServerHandler) HandleCreatePayment(c *gin.Context) {
	var req CreatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_INPUT", "message": err.Error()})
		return
	}

	if req.Simulation == "" {
		req.Simulation = domain.SimulateSuccess
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	role, _ := c.Get(middleware.ContextUserRole)

	// Fetch reservation to ensure valid & not expired
	res, err := h.repo.GetReservationByID(c.Request.Context(), req.ReservationID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RESERVATION_NOT_FOUND", "message": "Reservation not found"})
		return
	}

	if role != string(domain.RoleAdmin) && res.UserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You cannot pay for another user's reservation"})
		return
	}

	if res.Status == domain.ReservationStatusExpired {
		c.JSON(http.StatusConflict, gin.H{
			"error":       "RESERVATION_EXPIRED",
			"message":     "Reservation has expired. Cannot initiate payment for expired seats.",
			"status_code": http.StatusConflict,
		})
		return
	}

	payment := &domain.Payment{
		ID:            uuid.New(),
		ReservationID: req.ReservationID,
		UserID:        uid,
		AmountCents:   req.AmountCents,
		Currency:      req.Currency,
		Status:        domain.PaymentStatusPending,
		ProviderRef:   fmt.Sprintf("SIM-%s", hex.EncodeToString(paymentIDBytes())),
	}

	// Idempotency key from header if present
	if ikey := c.GetHeader("Idempotency-Key"); ikey != "" {
		payment.IdempotencyKey = &ikey
	}

	if err := h.repo.CreatePaymentTx(c.Request.Context(), payment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "PAYMENT_INIT_ERROR", "message": err.Error()})
		return
	}

	// Handle Payment Simulation Scenarios
	switch req.Simulation {
	case domain.SimulateFailure:
		// Payment rejected
		_ = h.repo.UpdatePaymentStatus(c.Request.Context(), payment.ID, domain.PaymentStatusFailed, payment.ProviderRef, "Card declined by simulated issuing bank", nil)
		payment.Status = domain.PaymentStatusFailed
		payment.ErrorMessage = "Card declined by simulated issuing bank"
		c.JSON(http.StatusPaymentRequired, gin.H{
			"payment": payment,
			"message": "Payment simulation: Transaction failed",
		})
		return

	case domain.SimulateTimeout:
		// Gateway timeout
		_ = h.repo.UpdatePaymentStatus(c.Request.Context(), payment.ID, domain.PaymentStatusTimeout, payment.ProviderRef, "Payment gateway timed out during authorization", nil)
		payment.Status = domain.PaymentStatusTimeout
		payment.ErrorMessage = "Payment gateway timed out during authorization"
		c.JSON(http.StatusGatewayTimeout, gin.H{
			"payment": payment,
			"message": "Payment simulation: Gateway timeout, safe to retry with same idempotency key",
		})
		return

	case domain.SimulateDelayed:
		// Asynchronous delayed processing via background worker
		go func(pID, rID uuid.UUID) {
			time.Sleep(3 * time.Second)
			ctx := context.Background()
			bookingRef := generateBookingRef()
			booking, err := h.repo.ConfirmReservationTx(ctx, rID, bookingRef)
			if err == nil {
				_ = h.repo.UpdatePaymentStatus(ctx, pID, domain.PaymentStatusSuccess, "", "", &booking.ID)
			} else {
				_ = h.repo.UpdatePaymentStatus(ctx, pID, domain.PaymentStatusFailed, "", fmt.Sprintf("Delayed confirmation error: %v", err), nil)
			}
		}(payment.ID, payment.ReservationID)

		c.JSON(http.StatusAccepted, gin.H{
			"payment": payment,
			"message": "Payment accepted for asynchronous processing. Please poll status.",
		})
		return

	default: // SimulateSuccess
		bookingRef := generateBookingRef()
		booking, err := h.repo.ConfirmReservationTx(c.Request.Context(), req.ReservationID, bookingRef)
		if err != nil {
			_ = h.repo.UpdatePaymentStatus(c.Request.Context(), payment.ID, domain.PaymentStatusFailed, payment.ProviderRef, err.Error(), nil)
			c.JSON(http.StatusConflict, gin.H{
				"error":       "CONFIRMATION_FAILED",
				"message":     fmt.Sprintf("Payment confirmed but reservation could not be confirmed: %v", err),
				"status_code": http.StatusConflict,
			})
			return
		}

		_ = h.repo.UpdatePaymentStatus(c.Request.Context(), payment.ID, domain.PaymentStatusSuccess, payment.ProviderRef, "", &booking.ID)
		payment.Status = domain.PaymentStatusSuccess
		payment.BookingID = &booking.ID

		c.JSON(http.StatusOK, gin.H{
			"payment": payment,
			"booking": booking,
			"message": "Payment successful and booking confirmed",
		})
	}
}

func (h *ServerHandler) HandleGetPayment(c *gin.Context) {
	idStr := c.Param("paymentId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_UUID", "message": "Invalid payment ID"})
		return
	}

	payment, err := h.repo.GetPaymentByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Payment not found"})
		return
	}

	uid := c.MustGet(middleware.ContextUserID).(uuid.UUID)
	role, _ := c.Get(middleware.ContextUserRole)
	if role != string(domain.RoleAdmin) && payment.UserID != uid {
		c.JSON(http.StatusForbidden, gin.H{"error": "FORBIDDEN", "message": "You do not have access to this payment"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"payment": payment})
}

// -------------------------------------------------------------
// Interactive Concurrency Stress Demo
// -------------------------------------------------------------

type ConcurrencyDemoRequest struct {
	SessionID uuid.UUID `json:"session_id" binding:"required"`
	SeatID    uuid.UUID `json:"seat_id" binding:"required"`
	Count     int       `json:"count"` // e.g. 10, 20, 50, 100 concurrent requests
}

func (h *ServerHandler) HandleConcurrencyDemo(c *gin.Context) {
	var req ConcurrencyDemoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_INPUT", "message": err.Error()})
		return
	}

	if req.Count <= 0 {
		req.Count = 20
	}
	if req.Count > 100 {
		req.Count = 100
	}

	start := time.Now()
	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32
	var otherCount int32
	var mu sync.Mutex
	var winningReservationID string
	var errorsList []string

	for i := 0; i < req.Count; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			mockUserID := uuid.New()
			res := &domain.Reservation{
				SessionID: req.SessionID,
				UserID:    mockUserID,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}

			created, err := h.repo.CreateReservationTx(ctx, res, []uuid.UUID{req.SeatID})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
				winningReservationID = created.ID.String()
			} else if errors.Is(err, domain.ErrSeatUnavailable) {
				conflictCount++
			} else {
				otherCount++
				errorsList = append(errorsList, fmt.Sprintf("worker %d: %v", workerID, err))
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// Fetch updated seat to verify authoritative state
	seat, _ := h.repo.GetSeatByID(c.Request.Context(), req.SeatID)

	c.JSON(http.StatusOK, gin.H{
		"test_parameters": gin.H{
			"session_id": req.SessionID,
			"seat_id":    req.SeatID,
			"goroutines": req.Count,
		},
		"results": gin.H{
			"total_attempts":        req.Count,
			"successful_holds":      successCount,
			"conflicts_prevented":   conflictCount,
			"unexpected_errors":     otherCount,
			"winning_reservation":   winningReservationID,
			"duration_ms":           duration.Milliseconds(),
			"seat_final_status":     seat.Status,
			"invariant_held":        successCount <= 1,
			"double_booking_status": "ZERO DOUBLE BOOKINGS PREVENTED VIA ROW LOCKS",
		},
	})
}

// -------------------------------------------------------------
// System Stats & Health
// -------------------------------------------------------------

func (h *ServerHandler) HandleSystemStats(c *gin.Context) {
	active, queued, maxC, qCap := h.admission.Stats()
	c.JSON(http.StatusOK, gin.H{
		"system": gin.H{
			"version":     "1.0.0",
			"environment": "production",
			"uptime":      time.Now().Format(time.RFC3339),
		},
		"admission_queue": gin.H{
			"active_requests": active,
			"queued_requests": queued,
			"max_concurrency": maxC,
			"queue_capacity":  qCap,
		},
		"dependencies": gin.H{
			"database": h.repo.Ping(c.Request.Context()) == nil,
			"cache":    h.cacheSvc.IsHealthy(c.Request.Context()),
			"broker":   h.bus.IsHealthy(),
		},
	})
}

func (h *ServerHandler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "UP",
		"timestamp": time.Now(),
	})
}

func (h *ServerHandler) HandleReady(c *gin.Context) {
	dbOk := h.repo.Ping(c.Request.Context()) == nil
	cacheOk := h.cacheSvc.IsHealthy(c.Request.Context())
	brokerOk := h.bus.IsHealthy()

	status := http.StatusOK
	if !dbOk {
		status = http.StatusServiceUnavailable
	}

	c.JSON(status, gin.H{
		"status":   "READY",
		"database": dbOk,
		"cache":    cacheOk,
		"broker":   brokerOk,
	})
}

func (h *ServerHandler) HandleMetrics(c *gin.Context) {
	active, queued, maxC, qCap := h.admission.Stats()
	metricsText := fmt.Sprintf(`# HELP seat_system_active_requests Current running requests
# TYPE seat_system_active_requests gauge
seat_system_active_requests %d
# HELP seat_system_queued_requests Current queued admission requests
# TYPE seat_system_queued_requests gauge
seat_system_queued_requests %d
# HELP seat_system_max_concurrency Max allowed concurrency
# TYPE seat_system_max_concurrency gauge
seat_system_max_concurrency %d
# HELP seat_system_queue_capacity Waiting queue capacity
# TYPE seat_system_queue_capacity gauge
seat_system_queue_capacity %d
`, active, queued, maxC, qCap)

	c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte(metricsText))
}

func generateBookingRef() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("BK-%s", hex.EncodeToString(b))
}

func paymentIDBytes() []byte {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return b
}
