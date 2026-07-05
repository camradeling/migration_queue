package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
	SMSAdapter          string // "console" or "http"
	RegistrationBaseURL string // base URL encoded into registration QR codes
	MigrationsPath      string
	AdminUsername       string
	AdminPassword       string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                getenv("PORT", "8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		SMSAdapter:          getenv("SMS_ADAPTER", "console"),
		RegistrationBaseURL: getenv("REGISTRATION_BASE_URL", "http://localhost:8080"),
		MigrationsPath:      getenv("MIGRATIONS_PATH", "migrations"),
		AdminUsername:       os.Getenv("ADMIN_USERNAME"),
		AdminPassword:       os.Getenv("ADMIN_PASSWORD"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
