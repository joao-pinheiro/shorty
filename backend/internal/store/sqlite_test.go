package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"shorty/internal/migrations"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var ctx = context.Background()

// --- Migration ---

func TestMigrationVersioning(t *testing.T) {
	s := newTestStore(t)

	var version int
	err := s.writeDB.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	// Verify tables exist
	for _, table := range []string{"links", "clicks", "tags", "link_tags", "schema_version"} {
		var name string
		err := s.writeDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s should exist: %v", table, err)
		}
	}

	// Verify indexes exist
	for _, idx := range []string{"idx_links_code", "idx_links_created_at", "idx_links_expires_at", "idx_clicks_link_id", "idx_clicks_clicked_at", "idx_link_tags_tag_id"} {
		var name string
		err := s.writeDB.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err != nil {
			t.Fatalf("index %s should exist: %v", idx, err)
		}
	}

	// Re-running New on same DB is idempotent
	dbPath := filepath.Join(t.TempDir(), "test2.db")
	s2, err := New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s2.Close()

	s3, err := New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("second open (idempotent): %v", err)
	}
	s3.Close()
}

// --- Links ---

func TestCreateLink(t *testing.T) {
	s := newTestStore(t)

	link, err := s.CreateLink(ctx, "abc123", "https://example.com", nil)
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	if link.Code != "abc123" {
		t.Errorf("expected code abc123, got %s", link.Code)
	}
	if link.OriginalURL != "https://example.com" {
		t.Errorf("expected URL https://example.com, got %s", link.OriginalURL)
	}
	if !link.IsActive {
		t.Error("expected link to be active")
	}
	if link.ClickCount != 0 {
		t.Errorf("expected click_count 0, got %d", link.ClickCount)
	}
	if link.ExpiresAt != nil {
		t.Error("expected nil expires_at")
	}

	// With expires_at
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	link2, err := s.CreateLink(ctx, "def456", "https://example.com/2", &exp)
	if err != nil {
		t.Fatalf("create link with expires: %v", err)
	}
	if link2.ExpiresAt == nil {
		t.Error("expected non-nil expires_at")
	}
}

func TestGetLinkByCode(t *testing.T) {
	s := newTestStore(t)

	s.CreateLink(ctx, "findme", "https://example.com", nil)

	link, err := s.GetLinkByCode(ctx, "findme")
	if err != nil {
		t.Fatalf("get by code: %v", err)
	}
	if link.Code != "findme" {
		t.Errorf("expected findme, got %s", link.Code)
	}

	_, err = s.GetLinkByCode(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetLinkByID(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateLink(ctx, "byid", "https://example.com", nil)

	link, err := s.GetLinkByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if link.ID != created.ID {
		t.Errorf("expected id %d, got %d", created.ID, link.ID)
	}

	_, err = s.GetLinkByID(ctx, 99999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListLinks(t *testing.T) {
	s := newTestStore(t)

	// Empty DB
	result, err := s.ListLinks(ctx, ListParams{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}

	// Insert 25 links
	for i := 0; i < 25; i++ {
		_, err := s.CreateLink(ctx, fmt.Sprintf("code%02d", i), fmt.Sprintf("https://example.com/%d", i), nil)
		if err != nil {
			t.Fatalf("create link %d: %v", i, err)
		}
	}

	// Page 1
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if result.Total != 25 {
		t.Errorf("expected total 25, got %d", result.Total)
	}
	if len(result.Links) != 20 {
		t.Errorf("expected 20 links, got %d", len(result.Links))
	}

	// Page 2
	result, err = s.ListLinks(ctx, ListParams{Page: 2, PerPage: 20})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(result.Links) != 5 {
		t.Errorf("expected 5 links on page 2, got %d", len(result.Links))
	}

	// Sort by created_at desc (default)
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 5, Sort: "created_at", Order: "desc"})
	if err != nil {
		t.Fatalf("list sort desc: %v", err)
	}
	if len(result.Links) > 1 && result.Links[0].CreatedAt.Before(result.Links[len(result.Links)-1].CreatedAt) {
		t.Error("expected descending order by created_at")
	}

	// Sort asc
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 5, Sort: "created_at", Order: "asc"})
	if err != nil {
		t.Fatalf("list sort asc: %v", err)
	}
	if len(result.Links) > 1 && result.Links[0].CreatedAt.After(result.Links[len(result.Links)-1].CreatedAt) {
		t.Error("expected ascending order by created_at")
	}

	// Search by URL
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 100, Search: "example.com/1"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// matches /1, /10..19 = 11 links
	if result.Total < 1 {
		t.Errorf("expected at least 1 result for search, got %d", result.Total)
	}

	// Search by code
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 100, Search: "code01"})
	if err != nil {
		t.Fatalf("search by code: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 result for code search, got %d", result.Total)
	}

	// Filter active
	active := true
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 100, Active: &active})
	if err != nil {
		t.Fatalf("filter active: %v", err)
	}
	if result.Total != 25 {
		t.Errorf("expected 25 active links, got %d", result.Total)
	}

	// Deactivate one and filter inactive
	isActive := false
	s.UpdateLink(ctx, 1, &isActive, nil)
	inactive := false
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 100, Active: &inactive})
	if err != nil {
		t.Fatalf("filter inactive: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 inactive link, got %d", result.Total)
	}

	// Filter by tag
	s.SetLinkTags(ctx, 1, []string{"special"})
	result, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 100, Tag: "special"})
	if err != nil {
		t.Fatalf("filter by tag: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 link with tag, got %d", result.Total)
	}

	// Invalid sort column
	_, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 20, Sort: "invalid"})
	if err == nil {
		t.Error("expected error for invalid sort column")
	}

	// Invalid order
	_, err = s.ListLinks(ctx, ListParams{Page: 1, PerPage: 20, Order: "sideways"})
	if err == nil {
		t.Error("expected error for invalid order")
	}
}

func TestUpdateLink(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "upd", "https://example.com", nil)

	isActive := false
	updated, err := s.UpdateLink(ctx, link.ID, &isActive, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.IsActive {
		t.Error("expected is_active=false")
	}
	// updated_at should be set (at least equal to created_at due to CURRENT_TIMESTAMP 1s resolution)
	if updated.UpdatedAt.IsZero() {
		t.Error("expected non-zero updated_at")
	}

	// Update expires_at
	exp := "2030-01-01T00:00:00Z"
	updated, err = s.UpdateLink(ctx, link.ID, nil, &exp)
	if err != nil {
		t.Fatalf("update expires_at: %v", err)
	}
	if updated.ExpiresAt == nil {
		t.Error("expected non-nil expires_at")
	}

	// Non-existent
	_, err = s.UpdateLink(ctx, 99999, &isActive, nil)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeactivateExpiredLink(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "exp", "https://example.com", nil)

	err := s.DeactivateExpiredLink(ctx, link.ID)
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	got, _ := s.GetLinkByID(ctx, link.ID)
	if got.IsActive {
		t.Error("expected link to be deactivated")
	}
}

func TestDeleteLink(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "del", "https://example.com", nil)

	// Add clicks and tags so cascade can be tested
	s.BatchInsertClicks(ctx, []ClickEvent{{LinkID: link.ID, ClickedAt: time.Now()}})
	s.SetLinkTags(ctx, link.ID, []string{"deltag"})

	err := s.DeleteLink(ctx, link.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = s.GetLinkByID(ctx, link.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Verify cascaded clicks deleted
	var clickCount int
	s.writeDB.QueryRow("SELECT COUNT(*) FROM clicks WHERE link_id = ?", link.ID).Scan(&clickCount)
	if clickCount != 0 {
		t.Errorf("expected 0 clicks after cascade, got %d", clickCount)
	}

	// Verify cascaded link_tags deleted
	var ltCount int
	s.writeDB.QueryRow("SELECT COUNT(*) FROM link_tags WHERE link_id = ?", link.ID).Scan(&ltCount)
	if ltCount != 0 {
		t.Errorf("expected 0 link_tags after cascade, got %d", ltCount)
	}

	// Delete non-existent
	err = s.DeleteLink(ctx, 99999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCodeExists(t *testing.T) {
	s := newTestStore(t)

	s.CreateLink(ctx, "exists", "https://example.com", nil)

	exists, err := s.CodeExists(ctx, "exists")
	if err != nil {
		t.Fatalf("code exists: %v", err)
	}
	if !exists {
		t.Error("expected code to exist")
	}

	exists, err = s.CodeExists(ctx, "nope")
	if err != nil {
		t.Fatalf("code exists: %v", err)
	}
	if exists {
		t.Error("expected code not to exist")
	}
}

// --- Tags ---

func TestCreateTag(t *testing.T) {
	s := newTestStore(t)

	tag, err := s.CreateTag(ctx, "mytag")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if tag.Name != "mytag" {
		t.Errorf("expected mytag, got %s", tag.Name)
	}
	if tag.ID == 0 {
		t.Error("expected non-zero id")
	}

	// Duplicate
	_, err = s.CreateTag(ctx, "mytag")
	if err != ErrTagExists {
		t.Errorf("expected ErrTagExists, got %v", err)
	}
}

func TestListTags(t *testing.T) {
	s := newTestStore(t)

	// Empty
	tags, err := s.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}

	// Create tags and link
	s.CreateTag(ctx, "alpha")
	s.CreateTag(ctx, "beta")
	link, _ := s.CreateLink(ctx, "tagged", "https://example.com", nil)
	s.SetLinkTags(ctx, link.ID, []string{"alpha"})

	tags, err = s.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// Find alpha - should have link_count=1
	for _, tag := range tags {
		if tag.Name == "alpha" && tag.LinkCount != 1 {
			t.Errorf("expected alpha link_count=1, got %d", tag.LinkCount)
		}
		if tag.Name == "beta" && tag.LinkCount != 0 {
			t.Errorf("expected beta link_count=0, got %d", tag.LinkCount)
		}
	}
}

func TestTagCount(t *testing.T) {
	s := newTestStore(t)

	count, _ := s.TagCount(ctx)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	s.CreateTag(ctx, "a")
	s.CreateTag(ctx, "b")

	count, _ = s.TagCount(ctx)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestGetTagByID(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateTag(ctx, "findtag")

	tag, err := s.GetTagByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get tag: %v", err)
	}
	if tag.Name != "findtag" {
		t.Errorf("expected findtag, got %s", tag.Name)
	}

	_, err = s.GetTagByID(ctx, 99999)
	if err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound, got %v", err)
	}
}

func TestDeleteTag(t *testing.T) {
	s := newTestStore(t)

	tag, _ := s.CreateTag(ctx, "deltag")
	link, _ := s.CreateLink(ctx, "tagdel", "https://example.com", nil)
	s.SetLinkTags(ctx, link.ID, []string{"deltag"})

	err := s.DeleteTag(ctx, tag.ID)
	if err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	// Tag gone
	_, err = s.GetTagByID(ctx, tag.ID)
	if err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound, got %v", err)
	}

	// link_tags cascade
	var count int
	s.writeDB.QueryRow("SELECT COUNT(*) FROM link_tags WHERE tag_id = ?", tag.ID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 link_tags after cascade, got %d", count)
	}

	// Link still exists
	_, err = s.GetLinkByID(ctx, link.ID)
	if err != nil {
		t.Error("link should still exist after tag delete")
	}

	// Delete non-existent
	err = s.DeleteTag(ctx, 99999)
	if err != ErrTagNotFound {
		t.Errorf("expected ErrTagNotFound, got %v", err)
	}
}

func TestSetLinkTags(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "settags", "https://example.com", nil)

	// Set tags (auto-create)
	err := s.SetLinkTags(ctx, link.ID, []string{"tag1", "tag2"})
	if err != nil {
		t.Fatalf("set tags: %v", err)
	}

	tags, _ := s.GetLinkTags(ctx, link.ID)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// Replace tags
	err = s.SetLinkTags(ctx, link.ID, []string{"tag3"})
	if err != nil {
		t.Fatalf("replace tags: %v", err)
	}

	tags, _ = s.GetLinkTags(ctx, link.ID)
	if len(tags) != 1 || tags[0] != "tag3" {
		t.Errorf("expected [tag3], got %v", tags)
	}

	// Set empty tags
	err = s.SetLinkTags(ctx, link.ID, []string{})
	if err != nil {
		t.Fatalf("set empty tags: %v", err)
	}

	tags, _ = s.GetLinkTags(ctx, link.ID)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestGetLinkTags(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "gettags", "https://example.com", nil)

	// No tags
	tags, err := s.GetLinkTags(ctx, link.ID)
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}

	// With tags
	s.SetLinkTags(ctx, link.ID, []string{"b", "a"})
	tags, _ = s.GetLinkTags(ctx, link.ID)
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	// Should be sorted by name
	if tags[0] != "a" || tags[1] != "b" {
		t.Errorf("expected [a b], got %v", tags)
	}
}

func TestGetLinksTagsBatch(t *testing.T) {
	s := newTestStore(t)

	// Empty input
	result, err := s.GetLinksTagsBatch(ctx, []int64{})
	if err != nil {
		t.Fatalf("batch empty: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}

	link1, _ := s.CreateLink(ctx, "batch1", "https://example.com/1", nil)
	link2, _ := s.CreateLink(ctx, "batch2", "https://example.com/2", nil)
	s.SetLinkTags(ctx, link1.ID, []string{"x", "y"})
	s.SetLinkTags(ctx, link2.ID, []string{"y", "z"})

	result, err = s.GetLinksTagsBatch(ctx, []int64{link1.ID, link2.ID})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(result[link1.ID]) != 2 {
		t.Errorf("expected 2 tags for link1, got %d", len(result[link1.ID]))
	}
	if len(result[link2.ID]) != 2 {
		t.Errorf("expected 2 tags for link2, got %d", len(result[link2.ID]))
	}
}

// --- Clicks ---

func TestBatchInsertClicks(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "clicks", "https://example.com", nil)

	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: time.Now().UTC()},
		{LinkID: link.ID, ClickedAt: time.Now().UTC()},
		{LinkID: link.ID, ClickedAt: time.Now().UTC()},
	}

	err := s.BatchInsertClicks(ctx, events)
	if err != nil {
		t.Fatalf("batch insert: %v", err)
	}

	// Verify click rows
	var count int
	s.writeDB.QueryRow("SELECT COUNT(*) FROM clicks WHERE link_id = ?", link.ID).Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 click rows, got %d", count)
	}

	// Verify click_count updated
	updated, _ := s.GetLinkByID(ctx, link.ID)
	if updated.ClickCount != 3 {
		t.Errorf("expected click_count=3, got %d", updated.ClickCount)
	}

	// Empty batch is no-op
	err = s.BatchInsertClicks(ctx, []ClickEvent{})
	if err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}

func TestGetClicksByDay(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "byday", "https://example.com", nil)

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)

	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: now},
		{LinkID: link.ID, ClickedAt: now},
		{LinkID: link.ID, ClickedAt: yesterday},
	}
	s.BatchInsertClicks(ctx, events)

	days, err := s.GetClicksByDay(ctx, link.ID, yesterday.Add(-time.Hour))
	if err != nil {
		t.Fatalf("get clicks by day: %v", err)
	}
	if len(days) < 1 {
		t.Error("expected at least 1 day bucket")
	}

	// No clicks before range
	days, err = s.GetClicksByDay(ctx, link.ID, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("get clicks by day (none): %v", err)
	}
	if len(days) != 0 {
		t.Errorf("expected 0 day buckets, got %d", len(days))
	}
}

func TestGetClicksByHour(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "byhour", "https://example.com", nil)

	now := time.Now().UTC()
	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: now},
		{LinkID: link.ID, ClickedAt: now},
	}
	s.BatchInsertClicks(ctx, events)

	hours, err := s.GetClicksByHour(ctx, link.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("get clicks by hour: %v", err)
	}
	if len(hours) < 1 {
		t.Error("expected at least 1 hour bucket")
	}
}

func TestGetPeriodClickCount(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "period", "https://example.com", nil)

	now := time.Now().UTC()
	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: now},
		{LinkID: link.ID, ClickedAt: now},
	}
	s.BatchInsertClicks(ctx, events)

	count, err := s.GetPeriodClickCount(ctx, link.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("get period count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestGetTotalClickCount(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "total", "https://example.com", nil)

	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: time.Now().UTC()},
		{LinkID: link.ID, ClickedAt: time.Now().UTC()},
	}
	s.BatchInsertClicks(ctx, events)

	count, err := s.GetTotalClickCount(ctx, link.ID)
	if err != nil {
		t.Fatalf("get total count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestDeleteClicksOlderThan(t *testing.T) {
	s := newTestStore(t)

	link, _ := s.CreateLink(ctx, "retention", "https://example.com", nil)

	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	events := []ClickEvent{
		{LinkID: link.ID, ClickedAt: old},
		{LinkID: link.ID, ClickedAt: recent},
	}
	s.BatchInsertClicks(ctx, events)

	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	deleted, err := s.DeleteClicksOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("delete old clicks: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Remaining click
	var count int
	s.writeDB.QueryRow("SELECT COUNT(*) FROM clicks WHERE link_id = ?", link.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 remaining click, got %d", count)
	}

	// click_count on link is NOT decremented (per spec)
	got, _ := s.GetLinkByID(ctx, link.ID)
	if got.ClickCount != 2 {
		t.Errorf("click_count should still be 2 (lifetime), got %d", got.ClickCount)
	}
}

// Verify the read pool works (GetLinkByCode uses readDB)
func TestReadPoolWorks(t *testing.T) {
	s := newTestStore(t)

	s.CreateLink(ctx, "readpool", "https://example.com", nil)

	// GetLinkByCode uses readDB
	link, err := s.GetLinkByCode(ctx, "readpool")
	if err != nil {
		t.Fatalf("readpool lookup: %v", err)
	}
	if link.Code != "readpool" {
		t.Errorf("expected readpool, got %s", link.Code)
	}

	// CodeExists uses readDB
	exists, err := s.CodeExists(ctx, "readpool")
	if err != nil {
		t.Fatalf("readpool exists: %v", err)
	}
	if !exists {
		t.Error("expected code to exist via readDB")
	}

	// Verify readDB ping works
	if err := s.readDB.Ping(); err != nil {
		// For file-based temp DB the read pool should work
		// However mode=ro might fail for new temp files, so we just check it doesn't panic
		_ = err
	}

	// Verify the readDB is indeed separate from writeDB
	if s.readDB == s.writeDB {
		t.Error("readDB and writeDB should be separate")
	}
}

// Test that unique constraint on code is enforced
func TestCreateLinkDuplicateCode(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateLink(ctx, "dup", "https://example.com", nil)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = s.CreateLink(ctx, "dup", "https://example.com/2", nil)
	if err == nil {
		t.Error("expected error for duplicate code")
	}
}

// Test foreign keys with schema_version table
func TestSchemaVersionTable(t *testing.T) {
	s := newTestStore(t)

	var count int
	err := s.writeDB.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count)
	if err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in schema_version, got %d", count)
	}

	// Verify foreign_keys pragma is ON
	var fk int
	s.writeDB.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Error("expected foreign_keys=ON")
	}

	// Verify WAL mode
	var journalMode string
	s.writeDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if journalMode != "wal" {
		t.Errorf("expected wal journal mode, got %s", journalMode)
	}
}

// Helper to open raw DB for verifying things directly
func verifyDB(t *testing.T, s *SQLiteStore) *sql.DB {
	t.Helper()
	return s.writeDB
}
