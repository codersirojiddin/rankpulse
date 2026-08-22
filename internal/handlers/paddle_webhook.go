package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PaddleWebhookHandler struct {
	DB     *pgxpool.Pool
	Secret string // Paddle Billing webhook signing secret
}

// paddleEvent models the subset of the Paddle Billing webhook payload
// RankPulse cares about. Paddle's full schema has more fields — add
// them here as needed.
type paddleEvent struct {
	EventType string `json:"event_type"`
	Data      struct {
		ID         string `json:"id"`
		CustomerID string `json:"customer_id"`
		Status     string `json:"status"` // active, canceled, past_due, trialing, paused
		Items      []struct {
			Price struct {
				BillingCycle struct {
					Interval string `json:"interval"` // month, year
				} `json:"billing_cycle"`
			} `json:"price"`
		} `json:"items"`
		CustomData struct {
			UserID string `json:"user_id"`
		} `json:"custom_data"`
	} `json:"data"`
}

// Handle processes subscription.created / subscription.updated /
// subscription.canceled webhooks from Paddle Billing and keeps
// public.users.subscription_status in sync.
func (h *PaddleWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to read request body")
		return
	}

	if !h.verifySignature(r.Header.Get("Paddle-Signature"), body) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	var evt paddleEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	ctx := r.Context()

	switch evt.EventType {
	case "subscription.created", "subscription.updated":
		planType := "monthly"
		if len(evt.Data.Items) > 0 && evt.Data.Items[0].Price.BillingCycle.Interval == "year" {
			planType = "annual"
		}
		status := mapPaddleStatus(evt.Data.Status)

		_, err = h.DB.Exec(ctx, `
			update users
			set subscription_status = $1,
			    plan_type = $2,
			    paddle_customer_id = $3
			where id = $4 or paddle_customer_id = $3
		`, status, planType, evt.Data.CustomerID, evt.Data.CustomData.UserID)

	case "subscription.canceled":
		_, err = h.DB.Exec(ctx, `
			update users
			set subscription_status = 'canceled'
			where paddle_customer_id = $1
		`, evt.Data.CustomerID)

	default:
		// Acknowledge unhandled event types with 200 so Paddle doesn't retry.
		log.Printf("[paddle] unhandled event type: %s", evt.EventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err != nil {
		log.Printf("[paddle] failed to update subscription for event %s: %v", evt.EventType, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func mapPaddleStatus(paddleStatus string) string {
	switch paddleStatus {
	case "active", "trialing":
		return "active"
	case "past_due":
		return "past_due"
	default:
		return "canceled"
	}
}

// verifySignature validates the `Paddle-Signature: ts=...;h1=...` header.
// h1 = hex(HMAC-SHA256(secret, "{ts}:{rawBody}")).
// See: https://developer.paddle.com/webhooks/signature-verification
func (h *PaddleWebhookHandler) verifySignature(header string, body []byte) bool {
	if header == "" {
		return false
	}

	var ts, h1 string
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "ts":
			ts = kv[1]
		case "h1":
			h1 = kv[1]
		}
	}
	if ts == "" || h1 == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(h.Secret))
	mac.Write([]byte(ts + ":" + string(body)))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(h1))
}
