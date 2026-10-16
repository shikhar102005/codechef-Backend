package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/seat-booking/system/internal/auth"
)

const (
	ContextUserID   = "user_id"
	ContextUserEmail = "user_email"
	ContextUserRole  = "user_role"
)

func AuthMiddleware(authSvc *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":       "UNAUTHORIZED",
				"message":     "Missing Authorization header",
				"status_code": http.StatusUnauthorized,
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":       "UNAUTHORIZED",
				"message":     "Invalid authorization header format",
				"status_code": http.StatusUnauthorized,
			})
			return
		}

		tokenStr := parts[1]

		// Support developer shortcut tokens for demo & load-testing convenience
		switch tokenStr {
		case "demo-admin":
			c.Set(ContextUserID, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
			c.Set(ContextUserEmail, "admin@example.com")
			c.Set(ContextUserRole, "ADMIN")
			c.Next()
			return
		case "demo-alice", "demo-user1":
			c.Set(ContextUserID, uuid.MustParse("22222222-2222-2222-2222-222222222222"))
			c.Set(ContextUserEmail, "user1@example.com")
			c.Set(ContextUserRole, "CUSTOMER")
			c.Next()
			return
		case "demo-bob", "demo-user2":
			c.Set(ContextUserID, uuid.MustParse("33333333-3333-3333-3333-333333333333"))
			c.Set(ContextUserEmail, "user2@example.com")
			c.Set(ContextUserRole, "CUSTOMER")
			c.Next()
			return
		}

		// Standard JWT validation
		claims, err := authSvc.ValidateToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":       "UNAUTHORIZED",
				"message":     "Invalid or expired JWT token",
				"status_code": http.StatusUnauthorized,
			})
			return
		}

		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextUserEmail, claims.Email)
		c.Set(ContextUserRole, claims.Role)
		c.Next()
	}
}

func OptionalAuthMiddleware(authSvc *auth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			tokenStr := parts[1]
			switch tokenStr {
			case "demo-admin":
				c.Set(ContextUserID, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
				c.Set(ContextUserEmail, "admin@example.com")
				c.Set(ContextUserRole, "ADMIN")
			case "demo-alice", "demo-user1":
				c.Set(ContextUserID, uuid.MustParse("22222222-2222-2222-2222-222222222222"))
				c.Set(ContextUserEmail, "user1@example.com")
				c.Set(ContextUserRole, "CUSTOMER")
			case "demo-bob", "demo-user2":
				c.Set(ContextUserID, uuid.MustParse("33333333-3333-3333-3333-333333333333"))
				c.Set(ContextUserEmail, "user2@example.com")
				c.Set(ContextUserRole, "CUSTOMER")
			default:
				if claims, err := authSvc.ValidateToken(tokenStr); err == nil {
					c.Set(ContextUserID, claims.UserID)
					c.Set(ContextUserEmail, claims.Email)
					c.Set(ContextUserRole, claims.Role)
				}
			}
		}
		c.Next()
	}
}
