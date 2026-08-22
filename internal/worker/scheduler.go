// Package worker runs the daily SERP-refresh job: a cron trigger that
// fans out pending keywords across a bounded goroutine pool, gated by
// a token-bucket rate limiter to respect the Serper.dev API quota.
package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"

	"rankpulse/internal/serp"
)

const (
	// pageSize controls how many pending keywords are pulled from the
	// DB per batch, to avoid loading an unbounded result set into memory.
	pageSize = 200

	// workerCount is the number of concurrent goroutines processing
	// SERP lookups. Tune based on Serper plan concurrency limits.
	workerCount = 10

	// requestsPerSecond caps outbound Serper.dev calls. Adjust to your
	// plan's rate limit (check Serper's dashboard for your quota).
	requestsPerSecond = 5
)

type Scheduler struct {
	DB           *pgxpool.Pool
	SerperClient *serp.Client
	Cron         *cron.Cron

	limiter *rate.Limiter
}

func NewScheduler(db *pgxpool.Pool, client *serp.Client) *Scheduler {
	return &Scheduler{
		DB:           db,
		SerperClient: client,
		Cron:         cron.New(),
		limiter:      rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond),
	}
}

// Start registers the daily refresh job (03:00 UTC) and starts the
// cron loop in the background. Call sched.Cron.Stop() on shutdown.
func (s *Scheduler) Start() {
	if _, err := s.Cron.AddFunc("0 3 * * *", func() {
		s.RunBatch(context.Background())
	}); err != nil {
		log.Fatalf("[scheduler] failed to register cron job: %v", err)
	}
	s.Cron.Start()
	log.Println("[scheduler] cron started — daily SERP refresh scheduled for 03:00 UTC")
}

// RunBatch drains the entire pending-keyword queue, processing it in
// pages so a large backlog doesn't spike memory usage.
func (s *Scheduler) RunBatch(ctx context.Context) {
	start := time.Now()
	total := 0

	for {
		pending, err := serp.FetchPendingKeywords(ctx, s.DB, pageSize)
		if err != nil {
			log.Printf("[scheduler] failed to fetch pending keywords: %v", err)
			return
		}
		if len(pending) == 0 {
			break
		}

		s.processConcurrently(ctx, pending)
		total += len(pending)

		if len(pending) < pageSize {
			break
		}
	}

	log.Printf("[scheduler] batch complete — %d keyword(s) processed in %s", total, time.Since(start).Round(time.Second))
}

// processConcurrently fans a batch of keywords out across a fixed
// worker pool, each gated by the shared rate limiter.
func (s *Scheduler) processConcurrently(ctx context.Context, keywords []serp.PendingKeyword) {
	jobs := make(chan serp.PendingKeyword)
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for kw := range jobs {
				s.processOne(ctx, kw)
			}
		}()
	}

	for _, kw := range keywords {
		jobs <- kw
	}
	close(jobs)
	wg.Wait()
}

func (s *Scheduler) processOne(ctx context.Context, kw serp.PendingKeyword) {
	if err := s.limiter.Wait(ctx); err != nil {
		log.Printf("[scheduler] rate limiter error for %q: %v", kw.KeywordText, err)
		return
	}

	start := time.Now()
	results, err := s.SerperClient.Search(ctx, kw.KeywordText, kw.CountryCode)
	if err != nil {
		log.Printf("[scheduler] search failed for %q: %v", kw.KeywordText, err)
		return
	}

	position := serp.FindRank(results, kw.ProjectDomain)

	if err := serp.UpdateKeywordRank(ctx, s.DB, kw.KeywordID, position); err != nil {
		log.Printf("[scheduler] db update failed for keyword %s: %v", kw.KeywordID, err)
		return
	}

	serp.LogRankCheck(kw, position, time.Since(start))
}
