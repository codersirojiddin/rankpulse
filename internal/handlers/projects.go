package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rankpulse/internal/auth"
	"rankpulse/internal/models"
)

type ProjectHandler struct {
	DB *pgxpool.Pool
}

type createProjectReq struct {
	Domain        string `json:"domain"`
	TargetCountry string `json:"target_country"`
}

// Create adds a new project owned by the authenticated user.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)

	var req createProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Domain) == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}
	if req.TargetCountry == "" {
		req.TargetCountry = "us"
	}

	var p models.Project
	err := h.DB.QueryRow(ctx, `
		insert into projects (user_id, domain, target_country)
		values ($1, $2, $3)
		returning id, user_id, domain, target_country, created_at
	`, userID, normalizeDomain(req.Domain), req.TargetCountry).
		Scan(&p.ID, &p.UserID, &p.Domain, &p.TargetCountry, &p.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create project")
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

// List returns every project owned by the authenticated user, along
// with a live keyword count for the dashboard summary cards.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)

	rows, err := h.DB.Query(ctx, `
		select p.id, p.user_id, p.domain, p.target_country, p.created_at,
		       coalesce(k.keyword_count, 0) as keyword_count
		from projects p
		left join (
			select project_id, count(*) as keyword_count
			from keywords
			group by project_id
		) k on k.project_id = p.id
		where p.user_id = $1
		order by p.created_at desc
	`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	projects := []models.Project{}
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.UserID, &p.Domain, &p.TargetCountry, &p.CreatedAt, &p.KeywordCount); err != nil {
			continue
		}
		projects = append(projects, p)
	}

	writeJSON(w, http.StatusOK, projects)
}

// Get returns a single project, scoped to the authenticated owner.
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")

	var p models.Project
	err := h.DB.QueryRow(ctx, `
		select id, user_id, domain, target_country, created_at
		from projects where id = $1 and user_id = $2
	`, projectID, userID).Scan(&p.ID, &p.UserID, &p.Domain, &p.TargetCountry, &p.CreatedAt)

	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// Delete removes a project (and cascades to its keywords/rank history)
// only if it belongs to the authenticated user.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")

	tag, err := h.DB.Exec(ctx, `delete from projects where id = $1 and user_id = $2`, projectID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// normalizeDomain strips protocol/www/paths so stored domains match
// consistently against SERP result links later.
func normalizeDomain(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "www.")
	if idx := strings.Index(d, "/"); idx != -1 {
		d = d[:idx]
	}
	return d
}
