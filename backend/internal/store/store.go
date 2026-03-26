package store

import (
	"context"
	"errors"
	"time"

	"shorty/internal/model"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrCodeExists  = errors.New("code already exists")
	ErrTagExists   = errors.New("tag already exists")
	ErrTagNotFound = errors.New("tag not found")
)

type ListParams struct {
	Page    int
	PerPage int
	Search  string
	Sort    string
	Order   string
	Active  *bool
	Tag     string
}

type ListResult struct {
	Links []model.Link
	Total int
}

type ClickEvent struct {
	LinkID    int64
	ClickedAt time.Time
}

type Store interface {
	// Links
	CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error)
	GetLinkByCode(ctx context.Context, code string) (*model.Link, error)
	GetLinkByID(ctx context.Context, id int64) (*model.Link, error)
	ListLinks(ctx context.Context, params ListParams) (*ListResult, error)
	UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error)
	DeactivateExpiredLink(ctx context.Context, id int64) error
	DeleteLink(ctx context.Context, id int64) error
	CodeExists(ctx context.Context, code string) (bool, error)

	// Clicks
	BatchInsertClicks(ctx context.Context, events []ClickEvent) error

	// Tags
	CreateTag(ctx context.Context, name string) (*model.Tag, error)
	ListTags(ctx context.Context) ([]model.TagWithCount, error)
	TagCount(ctx context.Context) (int, error)
	GetTagByID(ctx context.Context, id int64) (*model.Tag, error)
	DeleteTag(ctx context.Context, id int64) error
	SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error
	GetLinkTags(ctx context.Context, linkID int64) ([]string, error)
	GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error)

	// Analytics
	GetClicksByDay(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error)
	GetClicksByHour(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error)
	GetPeriodClickCount(ctx context.Context, linkID int64, since time.Time) (int, error)
	GetTotalClickCount(ctx context.Context, linkID int64) (int, error)

	// Retention
	DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error)

	// Lifecycle
	Close() error
}
