package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seat-booking/system/internal/auth"
	"github.com/seat-booking/system/internal/cache"
	"github.com/seat-booking/system/internal/config"
	"github.com/seat-booking/system/internal/database"
	internalHttp "github.com/seat-booking/system/internal/http"
	"github.com/seat-booking/system/internal/messaging"
	"github.com/seat-booking/system/internal/outbox"
)

func main() {
	// Initialize structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting Seat Reservation and Booking System API...")

	cfg := config.LoadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize Database (Postgres / Supabase with in-memory fallback)
	repo, engineType, err := database.InitRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("Failed to initialize database", "err", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("Database engine active", "type", engineType)

	// 2. Initialize Cache & Rate Limiting
	cacheSvc := cache.NewCacheService(ctx, cfg.RedisURL)

	// 3. Initialize Message Bus
	bus := messaging.NewMessageBus(ctx, cfg.RabbitMQURL)
	defer bus.Close()

	// 4. Initialize Auth Service
	authSvc := auth.NewAuthService(cfg.JWTSecret)

	// 5. Start Transactional Outbox Background Processor
	outboxProc := outbox.NewProcessor(repo, bus)
	outboxProc.Start(2 * time.Second)
	defer outboxProc.Stop()

	// 6. Start Periodic Reservation Expiry Reconciliation Worker
	expiryTicker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				expiryTicker.Stop()
				return
			case <-expiryTicker.C:
				reconcileCtx, rCancel := context.WithTimeout(context.Background(), 5*time.Second)
				expiredCount, err := repo.ExpireOverdueReservations(reconcileCtx)
				rCancel()
				if err == nil && expiredCount > 0 {
					slog.Info("Periodic reconciliation: auto-expired overdue reservations", "count", expiredCount)
					_ = cacheSvc.Delete(context.Background(), "events:all")
				}
			}
		}
	}()

	// 7. Setup Router
	router := internalHttp.SetupRouter(
		repo,
		cacheSvc,
		bus,
		authSvc,
		cfg.SystemMaxConcurrency,
		cfg.QueueCapacity,
		cfg.QueueTimeout,
	)

	// 8. Setup HTTP Server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("HTTP Server listening", "port", cfg.Port, "swagger_url", fmt.Sprintf("http://localhost:%s/swagger", cfg.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server error", "err", err)
		}
	}()

	// Graceful shutdown handling
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down API server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "err", err)
	}

	slog.Info("Server stopped cleanly.")
}
