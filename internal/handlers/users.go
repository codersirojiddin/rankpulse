

package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rankpulse/internal/auth"
	"rankpulse/internal/models"
)

type UserHandler struct {
	DB *pgxpool.Pool
}

// Me returns the authenticated user's own account row — used by the
// dashboard sidebar to show subscription status and plan type.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)

	var u models.User
	err := h.DB.QueryRow(ctx, `
		select id, email, paddle_customer_id, subscription_status, plan_type, created_at
		from users where id = $1
	`, userID).Scan(&u.ID, &u.Email, &u.PaddleCustomerID, &u.SubscriptionStatus, &u.PlanType, &u.CreatedAt)

	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "user record not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusOK, u)
}
