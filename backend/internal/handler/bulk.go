package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/config"
	"shorty/internal/model"
	"shorty/internal/shortcode"
	"shorty/internal/store"
	"shorty/internal/urlcheck"
)

type BulkHandler struct {
	store store.Store
	cfg   *config.Config
}

func NewBulkHandler(s store.Store, cfg *config.Config) *BulkHandler {
	return &BulkHandler{store: s, cfg: cfg}
}

type bulkCreateRequest struct {
	URLs []bulkCreateItem `json:"urls"`
}

type bulkCreateItem struct {
	URL        string   `json:"url"`
	CustomCode string   `json:"custom_code"`
	ExpiresIn  *int     `json:"expires_in"`
	Tags       []string `json:"tags"`
}

type bulkResultItem struct {
	OK    bool        `json:"ok"`
	Link  interface{} `json:"link,omitempty"`
	Error string      `json:"error,omitempty"`
	Index *int        `json:"index,omitempty"`
}

type bulkCreateResponse struct {
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Results   []bulkResultItem `json:"results"`
}

func (h *BulkHandler) Create(c echo.Context) error {
	var req bulkCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if len(req.URLs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "urls array is required and must not be empty"})
	}
	if len(req.URLs) > h.cfg.MaxBulkURLs {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("maximum %d URLs per request", h.cfg.MaxBulkURLs),
		})
	}

	ctx := c.Request().Context()
	results := make([]bulkResultItem, len(req.URLs))
	succeeded := 0
	failed := 0

	for i, item := range req.URLs {
		link, tags, err := h.processOneLink(ctx, item)
		if err != nil {
			idx := i
			results[i] = bulkResultItem{OK: false, Error: err.Error(), Index: &idx}
			failed++
		} else {
			link.ShortURL = h.cfg.BaseURL + "/" + link.Code
			link.Tags = tags
			if link.Tags == nil {
				link.Tags = []string{}
			}
			results[i] = bulkResultItem{OK: true, Link: link}
			succeeded++
		}
	}

	return c.JSON(http.StatusOK, bulkCreateResponse{
		Total:     len(req.URLs),
		Succeeded: succeeded,
		Failed:    failed,
		Results:   results,
	})
}

func (h *BulkHandler) processOneLink(ctx context.Context, item bulkCreateItem) (*model.Link, []string, error) {
	item.URL = strings.TrimSpace(item.URL)
	item.CustomCode = strings.TrimSpace(item.CustomCode)

	if err := urlcheck.Validate(item.URL); err != nil {
		return nil, nil, err
	}

	// Validate tags
	var trimmedTags []string
	if len(item.Tags) > 0 {
		trimmedTags = make([]string, 0, len(item.Tags))
		for _, name := range item.Tags {
			trimmed := strings.TrimSpace(name)
			if !validateTagName(trimmed) {
				return nil, nil, fmt.Errorf("invalid tag name: must be 1-50 alphanumeric, dash, or underscore")
			}
			trimmedTags = append(trimmedTags, trimmed)
		}
	}

	var expiresAt *string
	if item.ExpiresIn != nil {
		if *item.ExpiresIn <= 0 || *item.ExpiresIn > 31536000 {
			return nil, nil, fmt.Errorf("expires_in must be a positive integer, max 31536000 (365 days)")
		}
		t := time.Now().UTC().Add(time.Duration(*item.ExpiresIn) * time.Second).Format(time.RFC3339)
		expiresAt = &t
	}

	var code string
	if item.CustomCode != "" {
		if errMsg := shortcode.ValidateCustomCode(item.CustomCode); errMsg != "" {
			return nil, nil, fmt.Errorf("%s", errMsg)
		}
		exists, err := h.store.CodeExists(ctx, item.CustomCode)
		if err != nil {
			return nil, nil, fmt.Errorf("internal server error")
		}
		if exists {
			return nil, nil, fmt.Errorf("code already in use")
		}
		code = item.CustomCode
	} else {
		var err error
		code, err = shortcode.GenerateUnique(h.cfg.DefaultCodeLength, func(candidate string) (bool, error) {
			return h.store.CodeExists(ctx, candidate)
		})
		if err != nil {
			return nil, nil, fmt.Errorf("internal server error")
		}
	}

	link, err := h.store.CreateLink(ctx, code, item.URL, expiresAt)
	if err != nil {
		slog.Error("bulk create link failed", "error", err)
		return nil, nil, fmt.Errorf("internal server error")
	}

	if len(trimmedTags) > 0 {
		if err := h.store.SetLinkTags(ctx, link.ID, trimmedTags); err != nil {
			// Clean up orphaned link
			_ = h.store.DeleteLink(ctx, link.ID)
			slog.Error("bulk set link tags failed", "error", err)
			return nil, nil, fmt.Errorf("internal server error")
		}
	}

	return link, trimmedTags, nil
}
