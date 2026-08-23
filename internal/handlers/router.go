package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rankpulse/internal/auth"
)

// NewRouter wires every HTTP route for the RankPulse API. Kept in its
// own function so cmd/api/main.go stays a thin bootstrapping layer.
func NewRouter(pool *pgxpool.Pool, jwtSecret, paddleSecret string) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Paddle calls this directly — auth is via HMAC signature, not JWT.
	paddleHandler := &PaddleWebhookHandler{DB: pool, Secret: paddleSecret}
	r.Post("/webhooks/paddle", paddleHandler.Handle)

	projectHandler := &ProjectHandler{DB: pool}
	keywordHandler := &KeywordHandler{DB: pool}
	userHandler := &UserHandler{DB: pool}

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(jwtSecret))

		r.Get("/me", userHandler.Me)

		r.Route("/projects", func(r chi.Router) {
			r.Get("/", projectHandler.List)
			r.Post("/", projectHandler.Create)

			r.Route("/{projectID}", func(r chi.Router) {
				r.Get("/", projectHandler.Get)
				r.Delete("/", projectHandler.Delete)

				r.Route("/keywords", func(r chi.Router) {
					r.Get("/", keywordHandler.List)
					r.Post("/", keywordHandler.Create)

					r.Route("/{keywordID}", func(r chi.Router) {
						r.Delete("/", keywordHandler.Delete)
						r.Get("/history", keywordHandler.History)
					})
				})
			})
		})
	})

	return r
}
