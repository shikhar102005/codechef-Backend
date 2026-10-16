package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/seat-booking/system/internal/cache"
)

func RateLimitMiddleware(cacheSvc cache.CacheService, requestsPerMinute int) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Identify client by user_id if authenticated, else IP
		var clientKey string
		if uid, exists := c.Get(ContextUserID); exists {
			clientKey = fmt.Sprintf("rate_limit:%v", uid)
		} else {
			clientKey = fmt.Sprintf("rate_limit:ip:%s", c.ClientIP())
		}

		allowed, err := cacheSvc.AllowRateLimit(c.Request.Context(), clientKey, requestsPerMinute, 1*time.Minute)
		if err == nil && !allowed {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "RATE_LIMIT_EXCEEDED",
				"message":     "Too many requests. Please slow down and try again later.",
				"status_code": http.StatusTooManyRequests,
			})
			return
		}

		c.Next()
	}
}
