package database

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/seat-booking/system/internal/domain"
)

// InitRepository initializes either PostgreSQL or in-memory fallback
func InitRepository(ctx context.Context, dbURL string) (domain.Repository, string, error) {
	if dbURL != "" && dbURL != "memory" {
		slog.Info("Attempting connection to PostgreSQL/Supabase database...")
		repo, err := NewPostgresRepository(ctx, dbURL)
		if err == nil {
			slog.Info("PostgreSQL connection pool established successfully")
			// Try running migrations if migration files exist
			mig1 := filepath.Join("migrations", "001_initial_schema.sql")
			mig2 := filepath.Join("migrations", "002_seed_data.sql")
			if _, err1 := os.Stat(mig1); err1 == nil {
				if err := repo.RunMigrations(ctx, mig1); err != nil {
					slog.Warn("Migration 001 notice", "err", err)
				}
			}
			if _, err2 := os.Stat(mig2); err2 == nil {
				if err := repo.RunMigrations(ctx, mig2); err != nil {
					slog.Warn("Migration 002 notice", "err", err)
				}
			}
			return repo, "postgres", nil
		}
		slog.Warn("Could not connect to PostgreSQL, falling back to concurrency-safe memory engine", "error", err)
	}

	slog.Info("Using high-performance in-memory repository (pre-seeded with demo events, sessions, seats, and users)")
	return NewMemoryRepository(), "memory", nil
}
