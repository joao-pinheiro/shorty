package clickrecorder

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"shorty/internal/store"
)

type Recorder struct {
	clickCh       chan store.ClickEvent
	store         store.Store
	flushInterval time.Duration
	batchSize     int
	droppedCount  atomic.Int64
	wg            sync.WaitGroup
}

func New(s store.Store, bufferSize int, flushIntervalSec int) *Recorder {
	return &Recorder{
		clickCh:       make(chan store.ClickEvent, bufferSize),
		store:         s,
		flushInterval: time.Duration(flushIntervalSec) * time.Second,
		batchSize:     500,
	}
}

func (r *Recorder) Record(linkID int64) {
	select {
	case r.clickCh <- store.ClickEvent{LinkID: linkID, ClickedAt: time.Now()}:
	default:
		dropped := r.droppedCount.Add(1)
		slog.Warn("click dropped, buffer full", "link_id", linkID, "total_dropped", dropped)
	}
}

func (r *Recorder) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.run(ctx)
}

func (r *Recorder) run(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()

	var batch []store.ClickEvent

	for {
		select {
		case event := <-r.clickCh:
			batch = append(batch, event)
			if len(batch) >= r.batchSize {
				r.flush(batch)
				batch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				r.flush(batch)
				batch = nil
			}

		case <-ctx.Done():
			r.drain(&batch)
			if len(batch) > 0 {
				r.flush(batch)
			}
			return
		}
	}
}

func (r *Recorder) drain(batch *[]store.ClickEvent) {
	for {
		select {
		case event := <-r.clickCh:
			*batch = append(*batch, event)
		default:
			return
		}
	}
}

func (r *Recorder) flush(batch []store.ClickEvent) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := r.store.BatchInsertClicks(ctx, batch); err != nil {
		slog.Error("failed to flush clicks", "error", err, "batch_size", len(batch))
		return
	}

	slog.Debug("flushed clicks", "count", len(batch))
}

func (r *Recorder) Stop() {
	r.wg.Wait()

	dropped := r.droppedCount.Load()
	if dropped > 0 {
		slog.Warn("total clicks dropped during lifetime", "count", dropped)
	}
}

func (r *Recorder) DroppedCount() int64 {
	return r.droppedCount.Load()
}
