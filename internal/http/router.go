package http

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/seat-booking/system/internal/auth"
	"github.com/seat-booking/system/internal/cache"
	"github.com/seat-booking/system/internal/domain"
	"github.com/seat-booking/system/internal/messaging"
	"github.com/seat-booking/system/internal/middleware"
)

func SetupRouter(
	repo domain.Repository,
	cacheSvc cache.CacheService,
	bus messaging.MessageBus,
	authSvc *auth.AuthService,
	maxConcurrency int,
	queueCapacity int,
	queueTimeout time.Duration,
) *gin.Engine {
	r := gin.New()

	// Global Middlewares
	r.Use(gin.Recovery())
	r.Use(middleware.StructuredLoggingMiddleware())

	// CORS Setup
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "Idempotency-Key", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "Idempotency-Key", "X-Request-ID", "X-Cache-Lookup", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	admission := middleware.NewAdmissionControl(maxConcurrency, queueCapacity, queueTimeout)
	handler := NewServerHandler(repo, cacheSvc, bus, authSvc, admission)

	// Operational Endpoints
	r.GET("/health", handler.HandleHealth)
	r.GET("/ready", handler.HandleReady)
	r.GET("/metrics", handler.HandleMetrics)

	// Swagger UI Embedded HTML & OpenAPI JSON endpoint
	r.GET("/swagger", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, swaggerHTML)
	})
	r.GET("/swagger/doc.json", func(c *gin.Context) {
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.String(http.StatusOK, openAPISpecJSON)
	})

	api := r.Group("/api/v1")
	{
		// Public Auth
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", handler.HandleLogin)
			authGroup.POST("/register", handler.HandleRegister)
			authGroup.GET("/me", middleware.AuthMiddleware(authSvc), handler.HandleGetMe)
		}

		// Public Events & Sessions
		api.GET("/events", handler.HandleListEvents)
		api.GET("/events/:eventId", handler.HandleGetEvent)
		api.GET("/sessions/:sessionId", handler.HandleGetSession)
		api.GET("/sessions/:sessionId/seats", handler.HandleGetSessionSeats)

		// System Stats & Concurrency Stress Demonstration
		api.GET("/system/stats", handler.HandleSystemStats)
		api.POST("/demo/concurrency-test", handler.HandleConcurrencyDemo)

		// Protected Routes (JWT required + Idempotency + Admission Control + Rate Limiting)
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(authSvc))
		protected.Use(middleware.RateLimitMiddleware(cacheSvc, 120))
		protected.Use(admission.Handler())
		protected.Use(middleware.IdempotencyMiddleware(repo))
		{
			// Reservations
			protected.POST("/reservations", handler.HandleCreateReservation)
			protected.GET("/reservations/:reservationId", handler.HandleGetReservation)
			protected.POST("/reservations/:reservationId/confirm", handler.HandleConfirmReservation)
			protected.POST("/reservations/:reservationId/cancel", handler.HandleCancelReservation)

			// Bookings
			protected.GET("/bookings", handler.HandleListUserBookings)
			protected.GET("/bookings/:bookingId", handler.HandleGetBooking)
			protected.POST("/bookings/:bookingId/cancel", handler.HandleCancelBooking)

			// Payments
			protected.POST("/payments", handler.HandleCreatePayment)
			protected.GET("/payments/:paymentId", handler.HandleGetPayment)
		}
	}

	return r
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Seat Reservation & Booking System API - Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
  <style>
    body { margin: 0; background: #0f172a; }
    .swagger-ui { filter: invert(88%) hue-rotate(180deg); }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: '/swagger/doc.json',
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis],
      layout: "BaseLayout",
      deepLinking: true
    });
  };
</script>
</body>
</html>`

const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Seat Reservation and Booking System API",
    "version": "1.0.0",
    "description": "High-concurrency, transaction-safe seat reservation and booking system built with Go, PostgreSQL, Redis, and RabbitMQ."
  },
  "servers": [
    { "url": "/api/v1", "description": "API v1 Endpoint" }
  ],
  "paths": {
    "/events": {
      "get": {
        "summary": "List all events",
        "responses": {
          "200": { "description": "List of events with session times and availability" }
        }
      }
    },
    "/sessions/{sessionId}/seats": {
      "get": {
        "summary": "List all seats for session",
        "parameters": [
          { "name": "sessionId", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": {
          "200": { "description": "Seat map with real-time status (AVAILABLE, HELD, BOOKED, UNAVAILABLE)" }
        }
      }
    },
    "/reservations": {
      "post": {
        "summary": "Create temporary seat reservation (5-min hold)",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "Idempotency-Key", "in": "header", "required": false, "schema": { "type": "string" } }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "session_id": { "type": "string" },
                  "seat_ids": { "type": "array", "items": { "type": "string" } }
                }
              }
            }
          }
        },
        "responses": {
          "201": { "description": "Reservation created and seats locked" },
          "409": { "description": "Seat already reserved or conflict detected" }
        }
      }
    },
    "/payments": {
      "post": {
        "summary": "Simulate payment transaction",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "reservation_id": { "type": "string" },
                  "amount_cents": { "type": "integer" },
                  "simulation": { "type": "string", "enum": ["SUCCESS", "FAILURE", "DELAYED", "TIMEOUT"] }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "Payment succeeded and booking confirmed" },
          "402": { "description": "Payment simulated failure" },
          "202": { "description": "Payment delayed accepted asynchronously" }
        }
      }
    },
    "/demo/concurrency-test": {
      "post": {
        "summary": "Run simultaneous concurrent booking stress test against 1 seat",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "session_id": { "type": "string" },
                  "seat_id": { "type": "string" },
                  "count": { "type": "integer", "example": 50 }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "Concurrency test outcome verifying zero double bookings" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      }
    }
  }
}`
