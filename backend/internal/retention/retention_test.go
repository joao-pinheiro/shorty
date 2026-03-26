package retention

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"shorty/internal/store"
)

type mockStore struct {
	store.Store
	mu     sync.Mutex
	calls  []time.Time
}

func (m *mockStore) DeleteClicksOlderThan(_ context.Context, before time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, before)
	return 5, nil
}

func (m *mockStore) getCalls() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]time.Time, len(m.calls))
	copy(cp, m.calls)
	return cp
}

func TestNew_ZeroRetentionDays(t *testing.T) {
	r := New(&mockStore{}, 0, slog.Default())
	if r != nil {
		t.Error("expected nil runner for retentionDays <= 0")
	}
}

func TestNew_NegativeRetentionDays(t *testing.T) {
	r := New(&mockStore{}, -1, slog.Default())
	if r != nil {
		t.Error("expected nil runner for negative retentionDays")
	}
}

func TestRunner_PurgeCalled(t *testing.T) {
	ms := &mockStore{}
	r := New(ms, 30, slog.Default())
	if r == nil {
		t.Fatal("expected non-nil runner")
	}

	// Call purge directly to verify it works
	r.purge()

	calls := ms.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 purge call, got %d", len(calls))
	}

	// The cutoff should be approximately 30 days ago
	expected := time.Now().UTC().AddDate(0, 0, -30)
	diff := calls[0].Sub(expected)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("cutoff time off by %v", diff)
	}
}

func TestRunner_StopBlocksUntilExit(t *testing.T) {
	ms := &mockStore{}
	r := New(ms, 30, slog.Default())

	r.Start()

	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop returned, goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2 seconds")
	}
}

func TestRunner_TimeUntilMidnight(t *testing.T) {
	ms := &mockStore{}
	r := New(ms, 30, slog.Default())

	d := r.timeUntilMidnightUTC()
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("expected 0 < d <= 24h, got %v", d)
	}
}
