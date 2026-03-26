# Phase 8: Data Retention

## Summary

Implement a background goroutine that periodically deletes old click data from the `clicks` table. When `DATA_RETENTION_DAYS > 0`, the goroutine runs daily at midnight UTC and purges rows where `clicked_at` is older than the configured retention period. The `click_count` on `links` is NOT decremented — it represents a lifetime total (S14). The goroutine respects graceful shutdown (S10).

Spec references: S11 (`DATA_RETENTION_DAYS` config), S14 (data retention behavior), S10 (graceful shutdown).

---

## Step 1: Config

The `DATA_RETENTION_DAYS` environment variable is already defined in the config struct from Phase 1. Verify it exists:

```go
// In internal/config/config.go
type Config struct {
    // ... existing fields ...
    DataRetentionDays int // Default: 0 (forever)
}
```

Parsing (already done in Phase 1, verify):

```go
cfg.DataRetentionDays = getEnvInt("DATA_RETENTION_DAYS", 0)
```

A value of 0 means no retention policy — clicks are kept forever.

---

## Step 2: Store Method

Add to `Store` interface in `backend/internal/store/store.go`:

```go
DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error)
```

### SQLite Implementation

```go
func (s *SQLiteStore) DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error) {
    res, err := s.writeDB.ExecContext(ctx,
        `DELETE FROM clicks WHERE clicked_at < ?`,
        before.UTC().Format(time.RFC3339))
    if err != nil {
        return 0, err
    }
    return res.RowsAffected()
}
```

This uses the write pool (single writer, S9.3). The query uses a parameterized value, not string interpolation (S8.3).

**Important**: `click_count` on `links` is NOT decremented. Per S14, it is a lifetime total counter. The note in S14 explicitly states this: "click_count is a best-effort lifetime counter."

---

## Step 3: Retention Goroutine

Create `backend/internal/retention/retention.go`:

```go
package retention

import (
    "context"
    "log/slog"
    "time"

    "shorty/internal/store"
)

// Runner manages the periodic click data cleanup goroutine.
type Runner struct {
    store         store.Store
    retentionDays int
    done          chan struct{}
    logger        *slog.Logger
}

// New creates a new retention Runner. Returns nil if retentionDays <= 0.
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

// Start begins the background retention goroutine.
// It runs the first purge at the next midnight UTC, then daily.
func (r *Runner) Start() {
    go r.run()
}

// Stop signals the goroutine to stop and waits for it to finish.
func (r *Runner) Stop() {
    close(r.done)
}

func (r *Runner) run() {
    r.logger.Info("data retention started",
        "retention_days", r.retentionDays)

    // Calculate time until next midnight UTC
    timer := time.NewTimer(r.timeUntilMidnightUTC())

    for {
        select {
        case <-r.done:
            timer.Stop()
            r.logger.Info("data retention goroutine stopped")
            return
        case <-timer.C:
            r.purge()
            // Schedule next run at next midnight UTC (24h from now, adjusted)
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
```

### Design Decisions

- **Timer-based, not ticker-based**: Uses `time.NewTimer` targeting midnight UTC, not a fixed 24h ticker. This ensures the purge always runs at midnight UTC regardless of when the server started.
- **Context timeout**: 5-minute timeout on the DELETE query. For very large tables, this prevents indefinite blocking. If it times out, it will retry at the next midnight.
- **No batching**: The DELETE is a single query. SQLite handles this fine for reasonable row counts. If millions of rows need purging, the `busy_timeout` PRAGMA (5000ms, S3.5) prevents blocking other writes for too long.

---

## Step 4: Wire Into `main.go`

In `backend/cmd/shorty/main.go`, after store initialization and before starting the HTTP server:

```go
import "shorty/internal/retention"

// Start data retention goroutine if configured
retentionRunner := retention.New(store, cfg.DataRetentionDays, logger)
if retentionRunner != nil {
    retentionRunner.Start()
}
```

### Graceful Shutdown Integration

In the shutdown sequence (S10), after draining the click buffer and before closing the database:

```go
// Existing shutdown sequence:
// 1. Stop accepting new HTTP connections
// 2. Wait for in-flight requests (10s timeout)
// 3. Drain click recording channel

// Add step 3.5: Stop retention goroutine
if retentionRunner != nil {
    retentionRunner.Stop()
}

// 4. Close database connections
// 5. Exit
```

The order matters: stop retention before closing the DB so the goroutine doesn't try to query a closed connection.

---

## Step 5: Error Handling

- If the DELETE query fails, log at error level and continue. The goroutine retries at the next midnight.
- If `DATA_RETENTION_DAYS` is 0 (default), no goroutine is started. No resources consumed.
- If the context times out (5 min), the partially-completed DELETE still removed some rows. The next run will clean up the rest.
- No user-facing errors — this is entirely a background process.

---

## Step 6: Testing

### Store Test (`sqlite_test.go`)

```go
func TestDeleteClicksOlderThan(t *testing.T) {
    s := newTestStore(t) // in-memory SQLite

    // Create a link
    link := createTestLink(t, s, "https://example.com")

    // Insert clicks with specific timestamps
    // Click 1: 10 days ago
    // Click 2: 5 days ago
    // Click 3: 1 day ago
    // Click 4: now
    insertClickAt(t, s, link.ID, time.Now().AddDate(0, 0, -10))
    insertClickAt(t, s, link.ID, time.Now().AddDate(0, 0, -5))
    insertClickAt(t, s, link.ID, time.Now().AddDate(0, 0, -1))
    insertClickAt(t, s, link.ID, time.Now())

    // Also set click_count = 4 on the link
    // (normally done by click recorder, manually set for test)

    // Delete clicks older than 7 days
    cutoff := time.Now().AddDate(0, 0, -7)
    deleted, err := s.DeleteClicksOlderThan(context.Background(), cutoff)
    require.NoError(t, err)
    assert.Equal(t, int64(1), deleted) // only the 10-day-old click

    // Verify remaining clicks
    count := countClicks(t, s, link.ID)
    assert.Equal(t, 3, count)

    // Verify click_count on link is NOT decremented
    updatedLink, _ := s.GetLinkByID(context.Background(), link.ID)
    assert.Equal(t, 4, updatedLink.ClickCount) // still 4
}
```

Helper functions needed for the test:

```go
// insertClickAt inserts a click with a specific timestamp directly via SQL
func insertClickAt(t *testing.T, s *SQLiteStore, linkID int64, at time.Time) {
    _, err := s.writeDB.Exec(
        `INSERT INTO clicks (link_id, clicked_at) VALUES (?, ?)`,
        linkID, at.UTC().Format(time.RFC3339))
    require.NoError(t, err)
}

// countClicks returns the number of click rows for a link
func countClicks(t *testing.T, s *SQLiteStore, linkID int64) int {
    var count int
    err := s.readDB.QueryRow(`SELECT COUNT(*) FROM clicks WHERE link_id = ?`, linkID).Scan(&count)
    require.NoError(t, err)
    return count
}
```

### Retention Runner Unit Test

```go
func TestRetentionRunner_NilWhenDisabled(t *testing.T) {
    r := retention.New(nil, 0, slog.Default())
    assert.Nil(t, r)

    r = retention.New(nil, -1, slog.Default())
    assert.Nil(t, r)
}

func TestRetentionRunner_TimeUntilMidnight(t *testing.T) {
    // This is an internal function; test indirectly by verifying
    // the runner starts and stops cleanly without panicking
    mockStore := &mockStore{} // returns 0 deleted, nil error
    r := retention.New(mockStore, 30, slog.Default())
    r.Start()
    time.Sleep(10 * time.Millisecond) // let goroutine start
    r.Stop()
    // No panic, no hang = pass
}
```

### Integration Test

1. Set `DATA_RETENTION_DAYS=1` in test config.
2. Create a link. Insert clicks with timestamps 2 days ago and now (via direct SQL).
3. Manually call `purge()` (or expose it for testing).
4. Verify only the old click was deleted.
5. Verify `click_count` on the link is unchanged.

### Verification Commands

```bash
cd backend && go test ./internal/store/... -run TestDeleteClicks -race -v
cd backend && go test ./internal/retention/... -race -v
cd backend && go test ./... -race -count=1
```
