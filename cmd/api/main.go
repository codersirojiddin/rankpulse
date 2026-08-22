// Command api runs the RankPulse HTTP server: REST endpoints for
// projects/keywords and the Paddle billing webhook. Deploy this as a
// Render Web Service.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"rankpulse/internal/config"
	"rankpulse/internal/db"
	"rankpulse/internal/handlers"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[api] db connection failed: %v", err)
	}
	defer pool.Close()

	router := handlers.NewRouter(pool, cfg.SupabaseJWTSecret, cfg.PaddleWebhookSecret)

	// Wrap the chi router with global middleware (logging, recovery, CORS).
	handler := withMiddleware(router, cfg.FrontendOrigin)

	log.Printf("[api] rankpulse API listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		log.Fatalf("[api] server error: %v", err)
	}
}

func withMiddleware(next http.Handler, frontendOrigin string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", next)

	corsMiddleware := cors.Handler(cors.Options{
		AllowedOrigins:   []string{frontendOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	logged := middleware.Logger(mux)
	recovered := middleware.Recoverer(logged)
	timedOut := http.TimeoutHandler(recovered, 15*time.Second, `{"error":"request timed out"}`)

	return corsMiddleware(timedOut)
}
