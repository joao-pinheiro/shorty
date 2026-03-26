package model

import "time"

type Link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url"`
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	IsActive    bool       `json:"is_active"`
	ClickCount  int64      `json:"click_count"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Tags        []string   `json:"tags"`
}

type Click struct {
	ID        int64     `json:"id"`
	LinkID    int64     `json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
}

type Tag struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TagWithCount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LinkCount int64     `json:"link_count"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type HourCount struct {
	Hour  string `json:"hour"`
	Count int64  `json:"count"`
}
