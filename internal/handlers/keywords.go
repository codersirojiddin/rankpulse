package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rankpulse/internal/auth"
	"rankpulse/internal/models"
)

type KeywordHandler struct {
	DB *pgxpool.Pool
}

type createKeywordReq struct {
	KeywordText string `json:"keyword_text"`
}

// ownsProject confirms the authenticated user owns the given project
// before allowing any read/write on its keywords.
func (h *KeywordHandler) ownsProject(r *http.Request, projectID, userID string) (bool, error) {
	var owns bool
	err := h.DB.QueryRow(r.Context(),
		`select exists(select 1 from projects where id = $1 and user_id = $2)`,
		projectID, userID,
	).Scan(&owns)
	return owns, err
}

// Create adds a keyword to a project, enforcing the 20-keyword cap.
func (h *KeywordHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")

	owns, err := h.ownsProject(r, projectID, userID)
	if err != nil || !owns {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var count int
	if err := h.DB.QueryRow(ctx,
		`select count(*) from keywords where project_id = $1`, projectID,
	).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if count >= models.MaxKeywordsPerProject {
		writeError(w, http.StatusPaymentRequired, "keyword limit reached (20 per project) — upgrade your plan")
		return
	}

	var req createKeywordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.KeywordText) == "" {
		writeError(w, http.StatusBadRequest, "keyword_text is required")
		return
	}

	var kw models.Keyword
	err = h.DB.QueryRow(ctx, `
		insert into keywords (project_id, keyword_text)
		values ($1, $2)
		returning id, project_id, keyword_text, current_position, previous_position, last_checked_at, created_at
	`, projectID, strings.TrimSpace(req.KeywordText)).Scan(
		&kw.ID, &kw.ProjectID, &kw.KeywordText,
		&kw.CurrentPosition, &kw.PreviousPosition, &kw.LastCheckedAt, &kw.CreatedAt,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "could not add keyword (it may already exist in this project)")
		return
	}

	writeJSON(w, http.StatusCreated, kw)
}

// List returns every keyword tracked under a project.
func (h *KeywordHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")

	owns, err := h.ownsProject(r, projectID, userID)
	if err != nil || !owns {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.DB.Query(ctx, `
		select id, project_id, keyword_text, current_position, previous_position, last_checked_at, created_at
		from keywords
		where project_id = $1
		order by created_at desc
	`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	keywords := []models.Keyword{}
	for rows.Next() {
		var k models.Keyword
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.KeywordText, &k.CurrentPosition, &k.PreviousPosition, &k.LastCheckedAt, &k.CreatedAt); err != nil {
			continue
		}
		keywords = append(keywords, k)
	}

	writeJSON(w, http.StatusOK, keywords)
}

// Delete removes a keyword (and cascades to its rank_history rows),
// scoped through project ownership.
func (h *KeywordHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")
	keywordID := chi.URLParam(r, "keywordID")

	owns, err := h.ownsProject(r, projectID, userID)
	if err != nil || !owns {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	tag, err := h.DB.Exec(ctx,
		`delete from keywords where id = $1 and project_id = $2`, keywordID, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "keyword not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// History returns the last 90 days of rank_history for a single
// keyword — used to render the dashboard's line chart.
func (h *KeywordHandler) History(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := auth.UserIDFromContext(ctx)
	projectID := chi.URLParam(r, "projectID")
	keywordID := chi.URLParam(r, "keywordID")

	owns, err := h.ownsProject(r, projectID, userID)
	if err != nil || !owns {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	rows, err := h.DB.Query(ctx, `
		select rh.id, rh.keyword_id, rh.position, rh.checked_date
		from rank_history rh
		join keywords k on k.id = rh.keyword_id
		where rh.keyword_id = $1 and k.project_id = $2
		  and rh.checked_date >= current_date - interval '90 days'
		order by rh.checked_date asc
	`, keywordID, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	history := []models.RankHistoryEntry{}
	for rows.Next() {
		var entry models.RankHistoryEntry
		if err := rows.Scan(&entry.ID, &entry.KeywordID, &entry.Position, &entry.CheckedDate); err != nil {
			continue
		}
		history = append(history, entry)
	}

	writeJSON(w, http.StatusOK, history)
}
