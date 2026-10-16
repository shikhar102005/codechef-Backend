package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/domain"
)

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r responseBodyWriter) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func IdempotencyMiddleware(repo domain.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		idempotencyKey := c.GetHeader("Idempotency-Key")
		if idempotencyKey == "" {
			c.Next()
			return
		}

		userIDVal, exists := c.Get(ContextUserID)
		if !exists {
			c.Next()
			return
		}
		userID := userIDVal.(uuid.UUID)

		// Read and compute request body SHA256 fingerprint
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}
		hash := sha256.Sum256(bodyBytes)
		fingerprint := hex.EncodeToString(hash[:])

		ctx := c.Request.Context()
		record, err := repo.GetIdempotencyRecord(ctx, idempotencyKey)
		if err == nil && record != nil {
			// Key already exists! Check fingerprint
			if record.RequestFingerprint != fingerprint {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{
					"error":       "IDEMPOTENCY_PAYLOAD_MISMATCH",
					"message":     "Idempotency key has already been used with different request parameters",
					"status_code": http.StatusConflict,
				})
				return
			}

			// Key matches fingerprint: replay the exact cached response!
			c.Header("X-Cache-Lookup", "HIT-IDEMPOTENCY")
			c.Data(record.ResponseCode, "application/json; charset=utf-8", []byte(record.ResponseBody))
			c.Abort()
			return
		}

		// First time seeing this idempotency key: wrap response writer to capture output
		w := &responseBodyWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = w

		c.Next()

		// Save response if successful or deterministic client error
		statusCode := c.Writer.Status()
		if statusCode < 500 {
			_ = repo.SaveIdempotencyRecord(ctx, &domain.IdempotencyRecord{
				Key:                idempotencyKey,
				UserID:             userID,
				RequestFingerprint: fingerprint,
				ResponseCode:       statusCode,
				ResponseBody:       w.body.String(),
				CreatedAt:          time.Now(),
				ExpiresAt:          time.Now().Add(24 * time.Hour),
			})
		}
	}
}
