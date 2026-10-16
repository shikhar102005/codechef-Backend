package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/auth"
	"github.com/seat-booking/system/internal/cache"
	"github.com/seat-booking/system/internal/database"
	internalHttp "github.com/seat-booking/system/internal/http"
	"github.com/seat-booking/system/internal/messaging"
)

func setupTestRouter() http.Handler {
	ctx := context.Background()
	repo := database.NewMemoryRepository()
	cacheSvc := cache.NewCacheService(ctx, "")
	bus := messaging.NewMessageBus(ctx, "")
	authSvc := auth.NewAuthService("test-secret-key-123456789012345678")

	return internalHttp.SetupRouter(repo, cacheSvc, bus, authSvc, 50, 100, 3*time.Second)
}

func TestAPIEventsEndpoint(t *testing.T) {
	router := setupTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/events", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var resp struct {
		Events []interface{} `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse json: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Errorf("Expected seeded events, got 0")
	}
}

func TestAPIRunConcurrencyTestEndpoint(t *testing.T) {
	router := setupTestRouter()

	sessID := "b0000000-0000-0000-0000-000000000001"
	// Get seats
	reqSeats, _ := http.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessID+"/seats", nil)
	wSeats := httptest.NewRecorder()
	router.ServeHTTP(wSeats, reqSeats)

	var seatsResp struct {
		Seats []struct {
			ID string `json:"id"`
		} `json:"seats"`
	}
	_ = json.Unmarshal(wSeats.Body.Bytes(), &seatsResp)
	if len(seatsResp.Seats) == 0 {
		t.Fatalf("no seats returned")
	}

	targetSeatID := seatsResp.Seats[0].ID

	// Run concurrency test with 50 simultaneous requests
	body, _ := json.Marshal(map[string]interface{}{
		"session_id": sessID,
		"seat_id":    targetSeatID,
		"count":      50,
	})
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/demo/concurrency-test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var res struct {
		Results struct {
			TotalAttempts      int   `json:"total_attempts"`
			SuccessfulHolds    int   `json:"successful_holds"`
			ConflictsPrevented int   `json:"conflicts_prevented"`
			InvariantHeld      bool  `json:"invariant_held"`
		} `json:"results"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)

	if !res.Results.InvariantHeld || res.Results.SuccessfulHolds != 1 {
		t.Errorf("Expected invariant held with 1 hold, got %d", res.Results.SuccessfulHolds)
	}
	if res.Results.ConflictsPrevented != 49 {
		t.Errorf("Expected 49 conflicts, got %d", res.Results.ConflictsPrevented)
	}
}

func TestAPIIdempotencyKeyReplay(t *testing.T) {
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
	targetSeatID := seatsResp.Seats[1].ID

	idempotencyKey := "idem-test-key-" + uuid.New().String()
	payload := map[string]interface{}{
		"session_id": sessID,
		"seat_ids":   []string{targetSeatID},
	}
	body, _ := json.Marshal(payload)

	// First Request
	req1, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer demo-alice")
	req1.Header.Set("Idempotency-Key", idempotencyKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Request 1 failed: %d: %s", w1.Code, w1.Body.String())
	}

	// Second Identical Request with same Idempotency-Key
	req2, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer demo-alice")
	req2.Header.Set("Idempotency-Key", idempotencyKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// Must return the exact same status 201 without throwing conflict!
	if w2.Code != http.StatusCreated {
		t.Fatalf("Request 2 expected replayed 201, got %d: %s", w2.Code, w2.Body.String())
	}
	if w2.Header().Get("X-Cache-Lookup") != "HIT-IDEMPOTENCY" {
		t.Errorf("Expected X-Cache-Lookup HIT-IDEMPOTENCY")
	}

	// Third Request with SAME key but DIFFERENT payload -> Must return 409 Conflict
	diffPayload, _ := json.Marshal(map[string]interface{}{
		"session_id": sessID,
		"seat_ids":   []string{seatsResp.Seats[2].ID},
	})
	req3, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(diffPayload))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer demo-alice")
	req3.Header.Set("Idempotency-Key", idempotencyKey)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	if w3.Code != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for payload mismatch, got %d", w3.Code)
	}
}

func TestAPIPaymentSimulatorModes(t *testing.T) {
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

	// 1. Create a hold for seat 2
	resPayload, _ := json.Marshal(map[string]interface{}{
		"session_id": sessID,
		"seat_ids":   []string{seatsResp.Seats[2].ID},
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

	// Test Payment FAILURE simulation
	failPayload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": resID,
		"amount_cents":   25000,
		"simulation":     "FAILURE",
	})
	reqFail, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBuffer(failPayload))
	reqFail.Header.Set("Content-Type", "application/json")
	reqFail.Header.Set("Authorization", "Bearer demo-alice")
	wFail := httptest.NewRecorder()
	router.ServeHTTP(wFail, reqFail)

	if wFail.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402 Payment Required on simulated failure, got %d", wFail.Code)
	}

	// Test Payment SUCCESS simulation
	succPayload, _ := json.Marshal(map[string]interface{}{
		"reservation_id": resID,
		"amount_cents":   25000,
		"simulation":     "SUCCESS",
	})
	reqSucc, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBuffer(succPayload))
	reqSucc.Header.Set("Content-Type", "application/json")
	reqSucc.Header.Set("Authorization", "Bearer demo-alice")
	wSucc := httptest.NewRecorder()
	router.ServeHTTP(wSucc, reqSucc)

	if wSucc.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on payment success, got %d: %s", wSucc.Code, wSucc.Body.String())
	}
}
