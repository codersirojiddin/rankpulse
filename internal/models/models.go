// Package models defines the core domain entities shared across the
// API handlers, the SERP worker, and the database layer.
package models

import "time"

// MaxKeywordsPerProject enforces the free-tier / entry-plan cap.
// Bump this per-plan later if you introduce tiered limits.
const MaxKeywordsPerProject = 20

type User struct {
	ID                 string    `json:"id" db:"id"`
	Email              string    `json:"email" db:"email"`
	PaddleCustomerID   *string   `json:"paddle_customer_id" db:"paddle_customer_id"`
	SubscriptionStatus string    `json:"subscription_status" db:"subscription_status"`
	PlanType           *string   `json:"plan_type" db:"plan_type"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
}

type Project struct {
	ID            string    `json:"id" db:"id"`
	UserID        string    `json:"user_id" db:"user_id"`
	Domain        string    `json:"domain" db:"domain"`
	TargetCountry string    `json:"target_country" db:"target_country"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`

	// KeywordCount is populated only on list endpoints (not a DB column).
	KeywordCount int `json:"keyword_count,omitempty" db:"-"`
}

type Keyword struct {
	ID               string     `json:"id" db:"id"`
	ProjectID        string     `json:"project_id" db:"project_id"`
	KeywordText      string     `json:"keyword_text" db:"keyword_text"`
	CurrentPosition  *int       `json:"current_position" db:"current_position"`
	PreviousPosition *int       `json:"previous_position" db:"previous_position"`
	LastCheckedAt    *time.Time `json:"last_checked_at" db:"last_checked_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}

type RankHistoryEntry struct {
	ID          int64     `json:"id" db:"id"`
	KeywordID   string    `json:"keyword_id" db:"keyword_id"`
	Position    *int      `json:"position" db:"position"`
	CheckedDate time.Time `json:"checked_date" db:"checked_date"`
}
