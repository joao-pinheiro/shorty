# Phase 5: Async Click Recording — Implementation Plan

## Summary

Add non-blocking click recording to the redirect handler. Clicks are sent to a buffered channel and batch-inserted by a background goroutine. The redirect response is never delayed by click recording. Graceful shutdown drains the channel and flushes remaining clicks to the database.

---

## File: `backend/internal/clickrecorder/recorder.go`

### Struct and Constructor

```go
package clickrecorder

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"shorty/internal/store"
)

// Recorder handles async click recording via a buffered channel (S9.2).
type Recorder struct {
	clickCh       chan store.ClickEvent
	store         store.Store
	flushInterval time.Duration
	batchSize     int
	droppedCount  atomic.Int64
	wg            sync.WaitGroup
}

// New creates a Recorder with the given buffer size and flush interval.
// bufferSize corresponds to CLICK_BUFFER_SIZE (default 10000, S11).
// flushIntervalSec corresponds to CLICK_FLUSH_INTERVAL (default 1, S11).
func New(s store.Store, bufferSize int, flushIntervalSec int) *Recorder {
	return &Recorder{
		clickCh:       make(chan store.ClickEvent, bufferSize),
		store:         s,
		flushInterval: time.Duration(flushIntervalSec) * time.Second,
		batchSize:     500, // S9.2: flush when buffer reaches 500 items
	}
}
```

### Record Method (Non-Blocking Send)

Called by the redirect handler. Uses `select` with `default` to never block (S9.2).

```go
// Record enqueues a click event. If the channel is full, the click is dropped,
// a warning is logged, and the dropped counter is incremented (S9.2).
func (r *Recorder) Record(linkID int64) {
	select {
	case r.clickCh <- store.ClickEvent{LinkID: linkID, ClickedAt: time.Now()}:
		// sent
	default:
		dropped := r.droppedCount.Add(1)
		slog.Warn("click dropped, buffer full", "link_id", linkID, "total_dropped", dropped)
	}
}
```

### Start Method (Background Goroutine)

```go
// Start launches the background goroutine that batch-inserts clicks.
// Call Stop() to shut it down gracefully.
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
			// Flush if batch reaches threshold (S9.2: 500 items)
			if len(batch) >= r.batchSize {
				r.flush(batch)
				batch = nil
			}

		case <-ticker.C:
			// Flush on timer (S9.2: every flushInterval seconds)
			if len(batch) > 0 {
				r.flush(batch)
				batch = nil
			}

		case <-ctx.Done():
			// Context cancelled — drain channel and flush remaining (S10)
			r.drain(&batch)
			if len(batch) > 0 {
				r.flush(batch)
			}
			return
		}
	}
}
```

### Drain Method (Graceful Shutdown)

```go
// drain reads all remaining events from the channel into the batch (S10 step 3).
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
```

### Flush Method

```go
// flush writes a batch of click events to the database.
// Increments click_count on the links table in the same transaction (S9.2).
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
```

### Stop Method

```go
// Stop signals the background goroutine to drain and flush, then waits for completion (S10).
func (r *Recorder) Stop() {
	// The caller should cancel the context passed to Start().
	// Then wait for the goroutine to finish.
	r.wg.Wait()

	dropped := r.droppedCount.Load()
	if dropped > 0 {
		slog.Warn("total clicks dropped during lifetime", "count", dropped)
	}
}
```

### DroppedCount Accessor

```go
// DroppedCount returns the total number of dropped clicks (useful for monitoring/health).
func (r *Recorder) DroppedCount() int64 {
	return r.droppedCount.Load()
}
```

---

## Store Methods — `backend/internal/store/sqlite.go`

### `BatchInsertClicks`

Inserts click rows and increments `click_count` on the `links` table in a single transaction (S9.2).

```go
func (s *SQLiteStore) BatchInsertClicks(ctx context.Context, events []store.ClickEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Batch-insert click rows with the original click timestamp
	insertStmt, err := tx.PrepareContext(ctx, "INSERT INTO clicks (link_id, clicked_at) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer insertStmt.Close()

	// 2. Aggregate counts per link_id
	counts := make(map[int64]int)
	for _, event := range events {
		if _, err := insertStmt.ExecContext(ctx, event.LinkID, event.ClickedAt); err != nil {
			return fmt.Errorf("insert click: %w", err)
		}
		counts[event.LinkID]++
	}

	// 3. Increment click_count on links table (S9.2)
	// Note: updated_at is NOT bumped here — per S6.6, updated_at is only set
	// for PATCH operations and lazy deactivation (S6.1), not click recording.
	updateStmt, err := tx.PrepareContext(ctx,
		"UPDATE links SET click_count = click_count + ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer updateStmt.Close()

	for linkID, count := range counts {
		if _, err := updateStmt.ExecContext(ctx, count, linkID); err != nil {
			return fmt.Errorf("update click_count for link %d: %w", linkID, err)
		}
	}

	// 4. Commit
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
```

**Performance note**: For very large batches, the individual INSERT approach is fine since flushes are capped at 500 items. If performance becomes a concern, a multi-row INSERT could be used, but 500 individual parameterized inserts in a single transaction is fast in SQLite with WAL mode.

---

## Updated Redirect Handler — `backend/internal/handler/redirect.go`

Add the click recorder to the redirect handler.

### Updated Struct

```go
type RedirectHandler struct {
	store    store.Store
	recorder *clickrecorder.Recorder // added in Phase 5
}

func NewRedirectHandler(s store.Store, recorder *clickrecorder.Recorder) *RedirectHandler {
	return &RedirectHandler{store: s, recorder: recorder}
}
```

### Updated Redirect Method

Add one line after the expiration check, before the redirect response:

```go
	// ... (after expiration check, before redirect)

	// Record click asynchronously (S9.2) — never blocks the redirect response
	h.recorder.Record(link.ID)

	// Security headers for redirect (S8.5)
	c.Response().Header().Set("X-Frame-Options", "DENY")
	// ...
```

---

## Updated `backend/cmd/shorty/main.go`

### Wiring

```go
import "shorty/internal/clickrecorder"

// After store initialization:

// Create click recorder (S9.2)
recorder := clickrecorder.New(db, cfg.ClickBufferSize, cfg.ClickFlushInterval)

// Start background flush goroutine
recorderCtx, recorderCancel := context.WithCancel(context.Background())
recorder.Start(recorderCtx)

// Create redirect handler with recorder
redirectHandler := handler.NewRedirectHandler(db, recorder)
```

### Updated Graceful Shutdown (S10)

The shutdown sequence must drain clicks before closing the database:

```go
// Wait for signal
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

slog.Info("shutting down server")

// 1. Stop accepting new HTTP connections (S10 step 1)
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
defer shutdownCancel()

if err := e.Shutdown(shutdownCtx); err != nil {
	slog.Error("server forced to shutdown", "error", err)
}

// 2. Stop rate limiter cleanup goroutines
cancel() // cancels the context for limiter cleanup goroutines

// 3. Drain click channel and flush remaining batch (S10 step 3)
recorderCancel() // signals the recorder to drain and flush
drainDone := make(chan struct{})
go func() {
	recorder.Stop() // drains channel and flushes
	close(drainDone)
}()
select {
case <-drainDone:
	slog.Info("click buffer drained successfully")
case <-time.After(5 * time.Second):
	slog.Warn("click buffer drain timed out, some clicks may be lost")
	os.Exit(1)
}

// 4. Close database connections (S10 step 4)
if err := db.Close(); err != nil {
	slog.Error("failed to close database", "error", err)
}

slog.Info("server stopped")
// Exit with code 0 (S10 step 5)
```

### Full Shutdown Order (S10)

1. `e.Shutdown(ctx)` — stop accepting new connections, wait for in-flight requests (up to 10s)
2. `recorderCancel()` — signal recorder to stop
3. `recorder.Stop()` — wait for drain + final flush
4. `db.Close()` — close database connections
5. Exit 0 on success, exit 1 if drain timeout expired

---

## ClickEvent Type — `backend/internal/store/store.go`

Already defined in Phase 1's store interface. Confirm it exists:

```go
type ClickEvent struct {
	LinkID    int64
	ClickedAt time.Time
}
```

Set `ClickedAt: time.Now()` when creating the event in the redirect handler so the timestamp reflects the actual click time, not the batch flush time.

---

## Configuration Used

| Variable | Default | Used By |
|----------|---------|---------|
| `CLICK_BUFFER_SIZE` | `10000` | `clickrecorder.New()` — channel capacity |
| `CLICK_FLUSH_INTERVAL` | `1` | `clickrecorder.New()` — seconds between timer flushes |

---

## Concurrency Design

```
    Redirect Handler           Click Recorder Goroutine           SQLite
    ================          ============================       ========

    1. Look up link (read pool)
    2. recorder.Record(linkID)
       |
       v
    [buffered channel, cap=10000]
       |                        3. Read from channel
       |                        4. Accumulate batch
       |                        5. On timer tick OR batch=500:
       |                           flush(batch)
       |                           |
       |                           v
       |                        6. BEGIN TX (write pool)
       |                           INSERT INTO clicks...
       |                           UPDATE links SET click_count...
       |                           COMMIT

    3. Return 302 (immediate)
```

The redirect handler never waits for step 3-6. If the channel is full at step 2, the click is silently dropped (logged + counted).

---

## Error Handling

| Scenario | Behavior | Spec |
|----------|----------|------|
| Channel full | Click dropped, warning logged, counter incremented. Redirect succeeds. | S9.2 |
| Batch insert fails (DB error) | Error logged. Clicks in that batch are lost. Redirect is unaffected (already returned). | S19 "Click recording fails" |
| Shutdown with pending clicks | Channel drained, final flush attempted. If flush fails, error logged. | S10 |
| `crypto/rand` failure during code gen | Returns 500 (unrelated to click recording, but same request path) | S4 |

---

## Verification Checklist

1. **Click count increments**:
   ```bash
   # Create a link
   curl -X POST http://localhost:8080/api/v1/links \
     -H "Authorization: Bearer <key>" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://example.com"}'
   # Note the code, e.g. "aB3xYz"

   # Visit the short URL 3 times
   curl -s -o /dev/null http://localhost:8080/aB3xYz
   curl -s -o /dev/null http://localhost:8080/aB3xYz
   curl -s -o /dev/null http://localhost:8080/aB3xYz

   # Wait for flush interval (1 second)
   sleep 2

   # Check click count
   curl -H "Authorization: Bearer <key>" http://localhost:8080/api/v1/links/1
   # Expect click_count: 3
   ```

2. **Clicks table populated**:
   ```bash
   sqlite3 shorty.db "SELECT COUNT(*) FROM clicks WHERE link_id = 1"
   # Expect: 3
   ```

3. **Redirect latency unaffected**:
   - Time a redirect: `curl -w "%{time_total}" -s -o /dev/null http://localhost:8080/aB3xYz`
   - Should be < 10ms (S9.1).

4. **Channel full behavior**:
   - Set `CLICK_BUFFER_SIZE=5` and `CLICK_FLUSH_INTERVAL=60` (slow flush).
   - Send 10 rapid redirects.
   - Check logs for "click dropped, buffer full" warnings.
   - All 10 redirects should return 302 (none blocked).

5. **Graceful shutdown flush**:
   ```bash
   # Set CLICK_FLUSH_INTERVAL=3600 (1 hour, so auto-flush won't trigger)
   # Create a link and visit it
   curl -s -o /dev/null http://localhost:8080/aB3xYz

   # Send SIGINT
   kill -SIGINT <pid>

   # After server stops, check DB
   sqlite3 shorty.db "SELECT click_count FROM links WHERE id = 1"
   # The click should be flushed during shutdown
   ```

6. **DroppedCount accessible**: The `DroppedCount()` method can be exposed via a health or metrics endpoint in the future. For now, verify it's logged on shutdown if > 0.

7. **Batch transaction atomicity**: If INSERT succeeds but UPDATE fails, the transaction rolls back — neither clicks nor count are written. Verify by checking that `click_count` always matches `SELECT COUNT(*) FROM clicks WHERE link_id = ?` (within flush boundaries).
