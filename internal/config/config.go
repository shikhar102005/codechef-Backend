package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                     string
	Environment              string
	DatabaseURL              string
	RedisURL                 string
	RabbitMQURL              string
	JWTSecret                string
	ReservationExpiryMinutes int
	SystemMaxConcurrency    int
	QueueCapacity            int
	QueueTimeout             time.Duration
}

func LoadConfig() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		Port:                     getEnv("PORT", "8080"),
		Environment:              getEnv("ENVIRONMENT", "development"),
		DatabaseURL:              getEnv("DATABASE_URL", ""),
		RedisURL:                 getEnv("REDIS_URL", "localhost:6379"),
		RabbitMQURL:              getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		JWTSecret:                getEnv("JWT_SECRET", "super-secret-seat-booking-jwt-key-2026"),
		ReservationExpiryMinutes: getEnvAsInt("RESERVATION_EXPIRY_MINUTES", 5),
		SystemMaxConcurrency:    getEnvAsInt("SYSTEM_MAX_CONCURRENCY", 100),
		QueueCapacity:            getEnvAsInt("QUEUE_CAPACITY", 500),
		QueueTimeout:             time.Duration(getEnvAsInt("QUEUE_TIMEOUT_MS", 3000)) * time.Millisecond,
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if val, exists := os.LookupEnv(key); exists {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}
