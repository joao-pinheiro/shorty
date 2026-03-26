package retention

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"shorty/internal/store"
)

type Runner struct {
	store         store.Store
	retentionDays int
	done          chan struct{}
	wg            sync.WaitGroup
	logger        *slog.Logger
}

func New(s store.Store, retentionDays int, logger *slog.Logger) *Runner {
	if retentionDays <= 0 {
		return nil
	}
	return &Runner{
		store:         s,
		retentionDays: retentionDays,
		done:          make(chan struct{}),
		logger:        logger,
	}
}

func (r *Runner) Start() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run()
	}()
}

func (r *Runner) Stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *Runner) run() {
	r.logger.Info("data retention started",
		"retention_days", r.retentionDays)

	timer := time.NewTimer(r.timeUntilMidnightUTC())

	for {
		select {
		case <-r.done:
			timer.Stop()
			r.logger.Info("data retention goroutine stopped")
			return
		case <-timer.C:
			r.purge()
			timer.Reset(r.timeUntilMidnightUTC())
		}
	}
}

func (r *Runner) timeUntilMidnightUTC() time.Duration {
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return nextMidnight.Sub(now)
}

func (r *Runner) purge() {
	cutoff := time.Now().UTC().AddDate(0, 0, -r.retentionDays)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	deleted, err := r.store.DeleteClicksOlderThan(ctx, cutoff)
	if err != nil {
		r.logger.Error("data retention purge failed",
			"error", err,
			"cutoff", cutoff.Format(time.RFC3339))
		return
	}

	r.logger.Info("data retention purge completed",
		"deleted_clicks", deleted,
		"cutoff", cutoff.Format(time.RFC3339))
}
