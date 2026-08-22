package serp

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PendingKeyword bundles a keyword with the project context needed
// to run and interpret a SERP query for it.
type PendingKeyword struct {
	KeywordID     string
	KeywordText   string
	ProjectDomain string
	CountryCode   string
}

// FetchPendingKeywords returns keywords that haven't been checked in
// the last 24 hours, oldest-first, up to `limit` rows. Called
// repeatedly in pages by the scheduler until the queue is drained.
func FetchPendingKeywords(ctx context.Context, db *pgxpool.Pool, limit int) ([]PendingKeyword, error) {
	rows, err := db.Query(ctx, `
		select k.id, k.keyword_text, p.domain, p.target_country
		from keywords k
		join projects p on p.id = k.project_id
		where k.last_checked_at is null
		   or k.last_checked_at < now() - interval '24 hours'
		order by k.last_checked_at asc nulls first
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingKeyword
	for rows.Next() {
		var pk PendingKeyword
		if err := rows.Scan(&pk.KeywordID, &pk.KeywordText, &pk.ProjectDomain, &pk.CountryCode); err != nil {
			continue
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

// UpdateKeywordRank shifts current_position -> previous_position,
// stores the new position, and upserts today's rank_history row —
// all inside a single transaction so the two tables never drift.
func UpdateKeywordRank(ctx context.Context, db *pgxpool.Pool, keywordID string, newPosition *int) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op if committed

	if _, err := tx.Exec(ctx, `
		update keywords
		set previous_position = current_position,
		    current_position  = $2,
		    last_checked_at   = now()
		where id = $1
	`, keywordID, newPosition); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		insert into rank_history (keyword_id, position, checked_date)
		values ($1, $2, current_date)
		on conflict (keyword_id, checked_date)
		do update set position = excluded.position
	`, keywordID, newPosition); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// LogRankCheck writes a single-line structured log entry per keyword
// check — enough to debug quota/rate issues from Render's log tail.
func LogRankCheck(kw PendingKeyword, pos *int, elapsed time.Duration) {
	if pos != nil {
		log.Printf("[serp] %-30s (%s) -> rank #%-3d [%s]", kw.KeywordText, kw.ProjectDomain, *pos, elapsed.Round(time.Millisecond))
	} else {
		log.Printf("[serp] %-30s (%s) -> not found in top 100 [%s]", kw.KeywordText, kw.ProjectDomain, elapsed.Round(time.Millisecond))
	}
}
