package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/cache"
	"github.com/seat-booking/system/internal/database"
	"github.com/seat-booking/system/internal/domain"
	"github.com/seat-booking/system/internal/messaging"
	"github.com/seat-booking/system/internal/outbox"
)

func TestSingleSeat100ConcurrentReservations(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	// Pick session 1 and seat 1
	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, err := repo.ListSeatsBySessionID(ctx, sessID)
	if err != nil || len(seats) == 0 {
		t.Fatalf("failed to retrieve seats: %v", err)
	}
	targetSeat := seats[0]

	concurrency := 100
	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32
	var otherCount int32
	var winningReservationID uuid.UUID
	var mu sync.Mutex

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier // synchronize start

			uID := uuid.New()
			res := &domain.Reservation{
				SessionID: sessID,
				UserID:    uID,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}

			created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{targetSeat.ID})
			if err == nil {
				atomic.AddInt32(&successCount, 1)
				mu.Lock()
				winningReservationID = created.ID
				mu.Unlock()
			} else if errors.Is(err, domain.ErrSeatUnavailable) {
				atomic.AddInt32(&conflictCount, 1)
			} else {
				atomic.AddInt32(&otherCount, 1)
				t.Logf("Unexpected error in worker %d: %v", workerID, err)
			}
		}(i)
	}

	// Trigger all 100 goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	// Assertions
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful reservation, got %d", successCount)
	}
	if conflictCount != int32(concurrency-1) {
		t.Errorf("Expected %d conflicts, got %d", concurrency-1, conflictCount)
	}
	if otherCount != 0 {
		t.Errorf("Expected 0 unexpected errors, got %d", otherCount)
	}

	// Verify seat status in database
	updatedSeat, err := repo.GetSeatByID(ctx, targetSeat.ID)
	if err != nil {
		t.Fatalf("failed to get updated seat: %v", err)
	}
	if updatedSeat.Status != domain.SeatStatusHeld {
		t.Errorf("Expected seat status to be HELD, got %s", updatedSeat.Status)
	}

	// Verify winning reservation
	winningRes, err := repo.GetReservationByID(ctx, winningReservationID)
	if err != nil {
		t.Fatalf("failed to get winning reservation: %v", err)
	}
	if winningRes.Status != domain.ReservationStatusPending {
		t.Errorf("Expected winning reservation to be PENDING, got %s", winningRes.Status)
	}

	t.Logf("SUCCESS: 100 concurrent workers -> Exactly 1 won (%s), 99 rejected with conflict.", winningReservationID)
}

func TestMultiSeatOverlappingContention(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	if len(seats) < 3 {
		t.Fatalf("need at least 3 seats for overlapping test")
	}

	s1 := seats[0].ID
	s2 := seats[1].ID
	s3 := seats[2].ID

	// 3 combinations competing for overlapping seats:
	// Worker A: {s1, s2}
	// Worker B: {s2, s3}
	// Worker C: {s1, s3}
	requests := [][]uuid.UUID{
		{s1, s2},
		{s2, s3},
		{s1, s3},
	}

	var wg sync.WaitGroup
	var successCount int32
	startBarrier := make(chan struct{})

	for i, reqSeats := range requests {
		wg.Add(1)
		go func(wID int, seatList []uuid.UUID) {
			defer wg.Done()
			<-startBarrier

			res := &domain.Reservation{
				SessionID: sessID,
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}
			_, err := repo.CreateReservationTx(ctx, res, seatList)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}(i, reqSeats)
	}

	close(startBarrier)
	wg.Wait()

	// Exactly 1 of the overlapping pairs can succeed because they all share seats
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful pair, got %d", successCount)
	}

	t.Logf("SUCCESS: Overlapping multi-seat requests resolved with zero deadlocks and zero duplicate holds.")
}

func TestReservationExpiryAndRelease(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	seatID := seats[3].ID

	// 1. Create reservation that expires in 10 milliseconds
	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    uuid.New(),
		ExpiresAt: time.Now().Add(10 * time.Millisecond),
	}
	created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("failed to create reservation: %v", err)
	}

	// 2. Wait for expiry
	time.Sleep(30 * time.Millisecond)

	// 3. Try to confirm expired reservation -> Must Fail
	_, err = repo.ConfirmReservationTx(ctx, created.ID, "BK-TEST-EXPIRED")
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("Expected ErrReservationExpired, got %v", err)
	}

	// 4. Run background reconciler
	count, err := repo.ExpireOverdueReservations(ctx)
	if err != nil {
		t.Fatalf("reconciler failed: %v", err)
	}
	t.Logf("Reconciler processed %d expired reservations", count)

	// 5. Seat must now be AVAILABLE again for another user
	updatedSeat, _ := repo.GetSeatByID(ctx, seatID)
	if updatedSeat.Status != domain.SeatStatusAvailable {
		t.Errorf("Expected seat to be AVAILABLE after expiry, got %s", updatedSeat.Status)
	}

	// 6. Another user successfully reserves the released seat
	newUserRes := &domain.Reservation{
		SessionID: sessID,
		UserID:    uuid.New(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	newHold, err := repo.CreateReservationTx(ctx, newUserRes, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("Failed to reserve newly reallocated seat: %v", err)
	}
	if newHold == nil {
		t.Fatalf("Expected valid reservation object")
	}

	t.Logf("SUCCESS: Expired reservation safely released seat and allowed reallocation.")
}

func TestBookingCancellationAndSeatReallocation(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	seatID := seats[4].ID
	userID := uuid.New()

	// 1. Reserve seat
	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	created, _ := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})

	// 2. Confirm reservation
	booking, err := repo.ConfirmReservationTx(ctx, created.ID, "BK-REALLOC-1234")
	if err != nil {
		t.Fatalf("confirm failed: %v", err)
	}

	// Seat status should be BOOKED
	s, _ := repo.GetSeatByID(ctx, seatID)
	if s.Status != domain.SeatStatusBooked {
		t.Fatalf("Expected seat BOOKED, got %s", s.Status)
	}

	// 3. Cancel booking
	err = repo.CancelBookingTx(ctx, booking.ID, userID)
	if err != nil {
		t.Fatalf("cancel booking failed: %v", err)
	}

	// Seat should be back to AVAILABLE
	sAfter, _ := repo.GetSeatByID(ctx, seatID)
	if sAfter.Status != domain.SeatStatusAvailable {
		t.Fatalf("Expected seat to be AVAILABLE after cancellation, got %s", sAfter.Status)
	}

	// 4. Another user acquires the reallocated seat
	user2ID := uuid.New()
	res2 := &domain.Reservation{
		SessionID: sessID,
		UserID:    user2ID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	created2, err := repo.CreateReservationTx(ctx, res2, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("second user failed to reserve reallocated seat: %v", err)
	}
	if created2.UserID != user2ID {
		t.Fatalf("mismatched user")
	}

	t.Logf("SUCCESS: Booking cancellation verified with instant seat reallocation.")
}

// -------------------------------------------------------------------------------------
// Scenario 3: A reservation expires while payment is processing
// -------------------------------------------------------------------------------------
func TestReservationExpiresWhilePaymentProcessing(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, err := repo.ListSeatsBySessionID(ctx, sessID)
	if err != nil || len(seats) == 0 {
		t.Fatalf("failed to retrieve seats: %v", err)
	}
	seatID := seats[5].ID

	// 1. Create short-lived reservation (20ms TTL)
	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    uuid.New(),
		ExpiresAt: time.Now().Add(20 * time.Millisecond),
	}
	created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("failed to create reservation: %v", err)
	}

	// 2. Simulate payment processor delay exceeding the expiry TTL
	time.Sleep(40 * time.Millisecond)

	// 3. Payment completes after expiry and attempts to confirm booking
	booking, confirmErr := repo.ConfirmReservationTx(ctx, created.ID, "BK-LATE-PAYMENT")

	// 4. Assert: Confirmation MUST fail with ErrReservationExpired
	if !errors.Is(confirmErr, domain.ErrReservationExpired) {
		t.Fatalf("Expected ErrReservationExpired on late payment, got: %v", confirmErr)
	}
	if booking != nil {
		t.Fatalf("Expired reservation must never produce a confirmed booking")
	}

	// 5. Seat must be available again
	st, _ := repo.GetSeatByID(ctx, seatID)
	if st.Status != domain.SeatStatusAvailable {
		t.Errorf("Expected seat to revert to AVAILABLE, got: %s", st.Status)
	}

	t.Logf("SUCCESS Scenario 3: Expired reservation rejected during payment confirmation; seat safely released.")
}

// -------------------------------------------------------------------------------------
// Scenario 4: A payment succeeds but the client times out and retries (Idempotency)
// -------------------------------------------------------------------------------------
func TestPaymentIdempotentRetryOnTimeout(t *testing.T) {
	router := setupTestRouter()

	sessID := "b0000000-0000-0000-0000-000000000001"
	// Get available seats
	reqSeats, _ := http.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessID+"/seats", nil)
	wSeats := httptest.NewRecorder()
	router.ServeHTTP(wSeats, reqSeats)

	var seatsResp struct {
		Seats []struct {
			ID string `json:"id"`
		} `json:"seats"`
	}
	_ = json.Unmarshal(wSeats.Body.Bytes(), &seatsResp)
	seatID := seatsResp.Seats[6].ID

	// Create Hold
	resPayload, _ := json.Marshal(map[string]interface{}{
		"session_id": sessID,
		"seat_ids":   []string{seatID},
	})
	reqHold, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(resPayload))
	reqHold.Header.Set("Content-Type", "application/json")
	reqHold.Header.Set("Authorization", "Bearer demo-alice")
	wHold := httptest.NewRecorder()
	router.ServeHTTP(wHold, reqHold)

	var holdResp struct {
		Reservation struct {
			ID string `json:"id"`
		} `json:"reservation"`
	}
	_ = json.Unmarshal(wHold.Body.Bytes(), &holdResp)
	resID := holdResp.Reservation.ID

	idempotencyKey := "pay-timeout-retry-" + uuid.New().String()
	payPayload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": resID,
		"amount_cents":   25000,
		"simulation":     "SUCCESS",
	})

	// First Payment Request (Succeeds on server, but client simulates network timeout before receiving response)
	req1, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBuffer(payPayload))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer demo-alice")
	req1.Header.Set("Idempotency-Key", idempotencyKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("First payment request failed with %d: %s", w1.Code, w1.Body.String())
	}

	var firstResp struct {
		Booking struct {
			ID               string `json:"id"`
			BookingReference string `json:"booking_reference"`
		} `json:"booking"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &firstResp)

	// Second Payment Request: Client retries with identical Idempotency-Key
	req2, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBuffer(payPayload))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer demo-alice")
	req2.Header.Set("Idempotency-Key", idempotencyKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// Must return HTTP 200 with exact same response and HIT-IDEMPOTENCY header
	if w2.Code != http.StatusOK {
		t.Fatalf("Idempotent retry failed with code %d: %s", w2.Code, w2.Body.String())
	}
	if w2.Header().Get("X-Cache-Lookup") != "HIT-IDEMPOTENCY" {
		t.Errorf("Expected X-Cache-Lookup: HIT-IDEMPOTENCY")
	}

	var retryResp struct {
		Booking struct {
			ID               string `json:"id"`
			BookingReference string `json:"booking_reference"`
		} `json:"booking"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &retryResp)

	if retryResp.Booking.ID != firstResp.Booking.ID {
		t.Errorf("Mismatch in booking ID across idempotent retries! Original: %s, Retry: %s",
			firstResp.Booking.ID, retryResp.Booking.ID)
	}

	t.Logf("SUCCESS Scenario 4: Retried payment returned idempotent cached response without duplicate booking.")
}

// -------------------------------------------------------------------------------------
// Scenario 5: A RabbitMQ message is delivered more than once (Consumer Idempotency)
// -------------------------------------------------------------------------------------
func TestIdempotentMessageBusDelivery(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	seatID := seats[7].ID

	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    uuid.New(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("create reservation failed: %v", err)
	}

	// First confirmation message delivery
	booking1, err1 := repo.ConfirmReservationTx(ctx, created.ID, "BK-IDEM-001")
	if err1 != nil {
		t.Fatalf("First delivery confirmation failed: %v", err1)
	}
	if booking1 == nil {
		t.Fatalf("Expected booking on first delivery")
	}

	// Redelivery of exact same confirmation message from RabbitMQ
	booking2, err2 := repo.ConfirmReservationTx(ctx, created.ID, "BK-IDEM-001")
	if !errors.Is(err2, domain.ErrReservationAlreadyConfirmed) {
		t.Errorf("Expected ErrReservationAlreadyConfirmed on redelivered message, got: %v", err2)
	}
	if booking2 != nil {
		t.Errorf("Redelivered message created duplicate booking!")
	}

	// Verify authoritative seat state remains BOOKED
	st, _ := repo.GetSeatByID(ctx, seatID)
	if st.Status != domain.SeatStatusBooked {
		t.Errorf("Expected seat status BOOKED, got: %s", st.Status)
	}

	t.Logf("SUCCESS Scenario 5: Duplicate message redelivery safely rejected without state corruption.")
}

// -------------------------------------------------------------------------------------
// Scenario 6: Redis becomes unavailable (Graceful fallback)
// -------------------------------------------------------------------------------------
func TestRedisFailureGracefulFallback(t *testing.T) {
	ctx := context.Background()

	// Provide an invalid / unreachable Redis address to simulate an outage
	cacheSvc := cache.NewCacheService(ctx, "redis://127.0.0.1:65534")

	// Cache must not crash and fallback to local cache
	err := cacheSvc.Set(ctx, "test-key-fallback", map[string]string{"status": "ok"}, 1*time.Minute)
	if err != nil {
		t.Fatalf("Set failed under fallback mode: %v", err)
	}

	var result map[string]string
	err = cacheSvc.Get(ctx, "test-key-fallback", &result)
	if err != nil {
		t.Fatalf("Get failed under fallback mode: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", result["status"])
	}

	// Rate limiting fallback check (sliding window)
	for i := 0; i < 5; i++ {
		allowed, err := cacheSvc.AllowRateLimit(ctx, "rate-limit-test-user", 5, 1*time.Minute)
		if err != nil || !allowed {
			t.Fatalf("Iteration %d should be allowed, err: %v", i, err)
		}
	}
	// 6th request must exceed limit
	blocked, _ := cacheSvc.AllowRateLimit(ctx, "rate-limit-test-user", 5, 1*time.Minute)
	if blocked {
		t.Errorf("6th request should be blocked under rate-limit fallback")
	}

	t.Logf("SUCCESS Scenario 6: Redis outage gracefully handled via in-memory fallback cache and rate-limiter.")
}

// -------------------------------------------------------------------------------------
// Scenario 7: A background worker restarts while expired reservations are pending
// -------------------------------------------------------------------------------------
func TestWorkerRestartPendingExpiryReconciliation(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)

	// Create 3 reservations that expired while worker was down
	var expiredSeatIDs []uuid.UUID
	for i := 8; i < 11; i++ {
		sid := seats[i].ID
		expiredSeatIDs = append(expiredSeatIDs, sid)
		res := &domain.Reservation{
			SessionID: sessID,
			UserID:    uuid.New(),
			ExpiresAt: time.Now().Add(-10 * time.Minute), // Expired in past
		}
		_, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{sid})
		if err != nil {
			t.Fatalf("failed to create pending reservation: %v", err)
		}
	}

	// Verify seats are currently HELD
	for _, sid := range expiredSeatIDs {
		s, _ := repo.GetSeatByID(ctx, sid)
		if s.Status != domain.SeatStatusHeld {
			t.Fatalf("Seat %s should be HELD before reconciler runs", sid)
		}
	}

	// Simulate Worker Startup Reconciler running
	expiredCount, err := repo.ExpireOverdueReservations(ctx)
	if err != nil {
		t.Fatalf("Reconciler failed: %v", err)
	}
	if expiredCount < 3 {
		t.Errorf("Expected at least 3 expired reservations reconciled, got %d", expiredCount)
	}

	// Verify all seats were safely restored to AVAILABLE
	for _, sid := range expiredSeatIDs {
		s, _ := repo.GetSeatByID(ctx, sid)
		if s.Status != domain.SeatStatusAvailable {
			t.Errorf("Seat %s should be AVAILABLE after worker restart reconciler, got %s", sid, s.Status)
		}
	}

	t.Logf("SUCCESS Scenario 7: Worker restart reconciled %d pending expired reservations and released seats.", expiredCount)
}

// -------------------------------------------------------------------------------------
// Scenario 8: A user attempts to access or modify another user's booking/reservation
// -------------------------------------------------------------------------------------
func TestUnauthorizedCrossUserBookingAccess(t *testing.T) {
	router := setupTestRouter()

	sessID := "b0000000-0000-0000-0000-000000000001"
	reqSeats, _ := http.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessID+"/seats", nil)
	wSeats := httptest.NewRecorder()
	router.ServeHTTP(wSeats, reqSeats)

	var seatsResp struct {
		Seats []struct {
			ID string `json:"id"`
		} `json:"seats"`
	}
	_ = json.Unmarshal(wSeats.Body.Bytes(), &seatsResp)
	targetSeatID := seatsResp.Seats[11].ID

	// 1. User Alice reserves and confirms a booking
	holdBody, _ := json.Marshal(map[string]interface{}{
		"session_id": sessID,
		"seat_ids":   []string{targetSeatID},
	})
	reqHold, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(holdBody))
	reqHold.Header.Set("Content-Type", "application/json")
	reqHold.Header.Set("Authorization", "Bearer demo-alice")
	wHold := httptest.NewRecorder()
	router.ServeHTTP(wHold, reqHold)

	var holdResp struct {
		Reservation struct {
			ID string `json:"id"`
		} `json:"reservation"`
	}
	_ = json.Unmarshal(wHold.Body.Bytes(), &holdResp)
	aliceResID := holdResp.Reservation.ID

	// Alice confirms booking
	reqConf, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations/"+aliceResID+"/confirm", nil)
	reqConf.Header.Set("Authorization", "Bearer demo-alice")
	wConf := httptest.NewRecorder()
	router.ServeHTTP(wConf, reqConf)

	var confResp struct {
		Booking struct {
			ID string `json:"id"`
		} `json:"booking"`
	}
	_ = json.Unmarshal(wConf.Body.Bytes(), &confResp)
	aliceBookingID := confResp.Booking.ID

	// 2. User Bob attempts to GET Alice's booking details -> Must return 403 Forbidden
	reqBobGet, _ := http.NewRequest(http.MethodGet, "/api/v1/bookings/"+aliceBookingID, nil)
	reqBobGet.Header.Set("Authorization", "Bearer demo-bob")
	wBobGet := httptest.NewRecorder()
	router.ServeHTTP(wBobGet, reqBobGet)

	if wBobGet.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden when Bob accesses Alice's booking, got: %d", wBobGet.Code)
	}

	// 3. User Bob attempts to CANCEL Alice's booking -> Must return 403 Forbidden
	reqBobCancel, _ := http.NewRequest(http.MethodPost, "/api/v1/bookings/"+aliceBookingID+"/cancel", nil)
	reqBobCancel.Header.Set("Authorization", "Bearer demo-bob")
	wBobCancel := httptest.NewRecorder()
	router.ServeHTTP(wBobCancel, reqBobCancel)

	if wBobCancel.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden when Bob cancels Alice's booking, got: %d", wBobCancel.Code)
	}

	t.Logf("SUCCESS Scenario 8: Unauthorized cross-user booking access and cancellation blocked with HTTP 403.")
}

// -------------------------------------------------------------------------------------
// Scenario 9: A cancellation races with payment confirmation
// -------------------------------------------------------------------------------------
func TestCancellationRacesWithPaymentConfirmation(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	seatID := seats[12].ID
	userID := uuid.New()

	// Create active reservation
	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("failed to create reservation: %v", err)
	}

	// Race Cancellation vs Confirmation
	var wg sync.WaitGroup
	wg.Add(2)
	startBarrier := make(chan struct{})

	var cancelErr error
	var confirmErr error

	go func() {
		defer wg.Done()
		<-startBarrier
		cancelErr = repo.CancelReservationTx(ctx, created.ID, userID)
	}()

	go func() {
		defer wg.Done()
		<-startBarrier
		_, confirmErr = repo.ConfirmReservationTx(ctx, created.ID, "BK-RACE-CONFIRM")
	}()

	close(startBarrier)
	wg.Wait()

	// Exactly ONE must succeed and the other must fail with conflict/error
	if cancelErr == nil && confirmErr == nil {
		t.Fatalf("Race invariant violated: Both cancellation and confirmation succeeded simultaneously!")
	}
	if cancelErr != nil && confirmErr != nil {
		t.Fatalf("Both cancellation and confirmation failed!")
	}

	// Verify database seat status is consistent
	seat, _ := repo.GetSeatByID(ctx, seatID)
	if cancelErr == nil {
		// Cancellation won -> seat must be AVAILABLE
		if seat.Status != domain.SeatStatusAvailable {
			t.Errorf("Cancellation won, but seat status is %s", seat.Status)
		}
		t.Logf("Race outcome: Cancellation won first. Confirmation properly rejected.")
	} else {
		// Confirmation won -> seat must be BOOKED
		if seat.Status != domain.SeatStatusBooked {
			t.Errorf("Confirmation won, but seat status is %s", seat.Status)
		}
		t.Logf("Race outcome: Confirmation won first. Cancellation properly rejected.")
	}

	t.Logf("SUCCESS Scenario 9: Cancellation vs Confirmation race resolved consistently with zero state corruption.")
}

// -------------------------------------------------------------------------------------
// Scenario 10: The application restarts while asynchronous operations are pending (Outbox recovery)
// -------------------------------------------------------------------------------------
func TestApplicationRestartPendingOutboxRecovery(t *testing.T) {
	repo := database.NewMemoryRepository()
	ctx := context.Background()

	// Manually insert simulated pending outbox events (as would occur before a sudden crash)
	sessID := uuid.MustParse("b0000000-0000-0000-0000-000000000001")
	seats, _ := repo.ListSeatsBySessionID(ctx, sessID)
	seatID := seats[13].ID

	res := &domain.Reservation{
		SessionID: sessID,
		UserID:    uuid.New(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	created, err := repo.CreateReservationTx(ctx, res, []uuid.UUID{seatID})
	if err != nil {
		t.Fatalf("failed to create reservation: %v", err)
	}

	// Confirm reservation -> this generates a PENDING outbox event
	_, err = repo.ConfirmReservationTx(ctx, created.ID, "BK-OUTBOX-001")
	if err != nil {
		t.Fatalf("failed to confirm reservation: %v", err)
	}

	// Verify outbox event exists in database with status PENDING
	pending, err := repo.FetchPendingOutboxEvents(ctx, 10)
	if err != nil || len(pending) == 0 {
		t.Fatalf("Expected pending outbox events before worker startup, got %d", len(pending))
	}

	// Simulate Worker Startup with MessageBus
	bus := messaging.NewMessageBus(ctx, "")
	defer bus.Close()

	var receivedCount int32
	var mu sync.Mutex
	receivedTypes := make([]string, 0)

	_ = bus.Subscribe(messaging.QueueNotifications, func(ctx context.Context, eventType string, payload []byte) error {
		atomic.AddInt32(&receivedCount, 1)
		mu.Lock()
		receivedTypes = append(receivedTypes, eventType)
		mu.Unlock()
		return nil
	})

	// Run outbox processor to recover pending events from database
	processor := outbox.NewProcessor(repo, bus)
	processor.Start(50 * time.Millisecond)

	// Wait for outbox processor to publish pending events
	time.Sleep(200 * time.Millisecond)
	processor.Stop()

	// Check that pending outbox events were dispatched and marked PUBLISHED
	remainingPending, _ := repo.FetchPendingOutboxEvents(ctx, 10)
	if len(remainingPending) != 0 {
		t.Errorf("Expected 0 pending events after recovery, got %d", len(remainingPending))
	}

	t.Logf("SUCCESS Scenario 10: Crash recovery successfully processed all pending outbox events.")
}

