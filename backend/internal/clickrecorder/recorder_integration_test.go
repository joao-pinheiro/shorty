package clickrecorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"shorty/internal/store"
)

type mockStore struct {
	store.Store
	clicks []store.ClickEvent
	mu     sync.Mutex
}

func (m *mockStore) BatchInsertClicks(_ context.Context, events []store.ClickEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clicks = append(m.clicks, events...)
	return nil
}

func (m *mockStore) getClicks() []store.ClickEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.ClickEvent, len(m.clicks))
	copy(cp, m.clicks)
	return cp
}

func TestRecorder_FlushOnStop(t *testing.T) {
	ms := &mockStore{}
	r := New(ms, 100, 60) // large flush interval so only Stop triggers flush

	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)

	for i := 0; i < 10; i++ {
		r.Record(int64(i + 1))
	}

	// Give the goroutine a moment to receive events
	time.Sleep(50 * time.Millisecond)

	cancel()
	r.Stop()

	clicks := ms.getClicks()
	if len(clicks) != 10 {
		t.Errorf("expected 10 clicks flushed, got %d", len(clicks))
	}
}

func TestRecorder_FlushOnInterval(t *testing.T) {
	ms := &mockStore{}
	// flushInterval = 1 second (minimum supported by the constructor which uses seconds)
	// We'll use a custom approach: set flushIntervalSec=1 but that's 1s, too slow.
	// Instead, directly construct with a short interval.
	r := &Recorder{
		clickCh:       make(chan store.ClickEvent, 100),
		store:         ms,
		flushInterval: 100 * time.Millisecond,
		batchSize:     500,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	r.Record(1)
	r.Record(2)
	r.Record(3)

	// Wait for the flush interval to fire
	time.Sleep(300 * time.Millisecond)

	clicks := ms.getClicks()
	if len(clicks) != 3 {
		t.Errorf("expected 3 clicks flushed on interval, got %d", len(clicks))
	}

	cancel()
	r.Stop()
}

func TestRecorder_DropWhenFull(t *testing.T) {
	ms := &mockStore{}
	r := New(ms, 2, 60)

	// Don't start the recorder so nothing drains the channel
	r.Record(1)
	r.Record(2)
	// Buffer full now
	r.Record(3)
	r.Record(4)
	r.Record(5)

	if r.DroppedCount() < 3 {
		t.Errorf("expected at least 3 dropped, got %d", r.DroppedCount())
	}
}
