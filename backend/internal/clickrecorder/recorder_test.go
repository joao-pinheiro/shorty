package clickrecorder

import (
	"context"
	"testing"
	"time"

	"shorty/internal/store"
)

// stubStore satisfies store.Store for the Recorder, only BatchInsertClicks matters.
type stubStore struct {
	store.Store
}

func (s *stubStore) BatchInsertClicks(ctx context.Context, events []store.ClickEvent) error {
	return nil
}

func TestRecord_SendsToChannel(t *testing.T) {
	r := New(&stubStore{}, 100, 1)

	r.Record(42)

	select {
	case event := <-r.clickCh:
		if event.LinkID != 42 {
			t.Errorf("expected link_id 42, got %d", event.LinkID)
		}
		if time.Since(event.ClickedAt) > 2*time.Second {
			t.Error("ClickedAt too old")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event on channel")
	}
}

func TestRecord_DropsWhenFull(t *testing.T) {
	r := New(&stubStore{}, 1, 1)

	// Fill the buffer
	r.Record(1)

	// This should be dropped (non-blocking)
	r.Record(2)

	if r.DroppedCount() != 1 {
		t.Errorf("expected 1 dropped, got %d", r.DroppedCount())
	}

	// Another drop
	r.Record(3)
	if r.DroppedCount() != 2 {
		t.Errorf("expected 2 dropped, got %d", r.DroppedCount())
	}
}

func TestRecord_NonBlocking(t *testing.T) {
	r := New(&stubStore{}, 1, 1)

	// Fill the channel
	r.Record(1)

	// This must return immediately, not block
	done := make(chan struct{})
	go func() {
		r.Record(2)
		close(done)
	}()

	select {
	case <-done:
		// good, didn't block
	case <-time.After(time.Second):
		t.Fatal("Record blocked when channel was full")
	}
}
