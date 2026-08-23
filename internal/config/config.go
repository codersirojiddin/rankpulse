// Package config loads and validates environment variables required
// to run the RankPulse API server and background worker.
package config

import (
	"log"
	"os"
)

type Config struct {
	DatabaseURL         string // Supabase pooled connection string (pgbouncer, port 6543)
	SupabaseURL         string // used to build the JWKS endpoint for JWT verification
	SerperAPIKey        string
	PaddleWebhookSecret string
	PaddleAPIKey        string
	FrontendOrigin      string
	Port                string
}

// Load reads all required environment variables and fails fast with a
// clear error if any mandatory value is missing. This is intentional:
// a misconfigured production deploy should never boot silently.
func Load() *Config {
	cfg := &Config{
		DatabaseURL:         mustGet("DATABASE_URL"),
		SupabaseURL:         mustGet("SUPABASE_URL"),
		SerperAPIKey:        mustGet("SERPER_API_KEY"),
		PaddleWebhookSecret: mustGet("PADDLE_WEBHOOK_SECRET"),
		PaddleAPIKey:        os.Getenv("PADDLE_API_KEY"),
		FrontendOrigin:      getOr("FRONTEND_ORIGIN", "*"),
		Port:                getOr("PORT", "8080"),
	}
	return cfg
}

func mustGet(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("config: missing required env var: %s", key)
	}
	return v
}

func getOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}