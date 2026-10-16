package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seat-booking/system/internal/config"
	"github.com/seat-booking/system/internal/database"
	"github.com/seat-booking/system/internal/messaging"
	"github.com/seat-booking/system/internal/outbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting Seat Booking Background Worker...")

	cfg := config.LoadConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo, engineType, err := database.InitRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("Worker failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("Worker database connected", "engine", engineType)

	bus := messaging.NewMessageBus(ctx, cfg.RabbitMQURL)
	defer bus.Close()

	// Subscribe to Expiry Queue
	_ = bus.Subscribe(messaging.QueueExpirations, func(ctx context.Context, eventType string, payload []byte) error {
		slog.Info("Worker received reservation event", "type", eventType)
		return nil
	})

	// Subscribe to Notifications Queue
	_ = bus.Subscribe(messaging.QueueNotifications, func(ctx context.Context, eventType string, payload []byte) error {
		slog.Info("Worker received booking confirmation event", "type", eventType)
		return nil
	})

	// Start Outbox processor in worker
	processor := outbox.NewProcessor(repo, bus)
	processor.Start(2 * time.Second)
	defer processor.Stop()

	// Periodic Reconciliation Loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcileCtx, rCancel := context.WithTimeout(context.Background(), 5*time.Second)
				count, err := repo.ExpireOverdueReservations(reconcileCtx)
				rCancel()
				if err == nil && count > 0 {
					slog.Info("Worker reconciled expired reservations", "expired_count", count)
				}
			}
		}
	}()

	slog.Info("Worker is actively running. Press Ctrl+C to stop.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("Worker stopping...")
}
