// Command worker runs the RankPulse background scheduler: a daily
// cron job that refreshes Google rank positions for every tracked
// keyword via the Serper.dev API. Deploy this as a separate Render
// Background Worker service (do not run it inside the API process).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"rankpulse/internal/config"
	"rankpulse/internal/db"
	"rankpulse/internal/serp"
	"rankpulse/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[worker] db connection failed: %v", err)
	}
	defer pool.Close()

	client := serp.NewClient(cfg.SerperAPIKey)
	sched := worker.NewScheduler(pool, client)
	sched.Start()

	log.Println("[worker] rankpulse worker running — waiting for scheduled jobs (Ctrl+C to stop)")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("[worker] shutdown signal received, stopping cron")
	stopCtx := sched.Cron.Stop()
	<-stopCtx.Done()
	log.Println("[worker] shutdown complete")
}
