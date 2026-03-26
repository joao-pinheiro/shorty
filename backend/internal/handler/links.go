package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/config"
	"shorty/internal/model"
	"shorty/internal/qr"
	"shorty/internal/shortcode"
	"shorty/internal/store"
	"shorty/internal/urlcheck"
)

type LinkHandler struct {
	store  store.Store
	config *config.Config
}

func NewLinkHandler(s store.Store, cfg *config.Config) *LinkHandler {
	return &LinkHandler{store: s, config: cfg}
}

type CreateLinkRequest struct {
	URL        string   `json:"url"`
	CustomCode string   `json:"custom_code,omitempty"`
	ExpiresIn  *int     `json:"expires_in,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type UpdateLinkRequest struct {
	IsActive  *bool     `json:"is_active,omitempty"`
	ExpiresAt *string   `json:"expires_at,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
}

func (h *LinkHandler) Create(c echo.Context) error {
	var req CreateLinkRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	req.URL = strings.TrimSpace(req.URL)
	req.CustomCode = strings.TrimSpace(req.CustomCode)

	if err := urlcheck.Validate(req.URL); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Validate and trim tag names before creating the link
	var trimmedTags []string
	if len(req.Tags) > 0 {
		trimmedTags = make([]string, 0, len(req.Tags))
		for _, name := range req.Tags {
			trimmed := strings.TrimSpace(name)
			if !validateTagName(trimmed) {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": "invalid tag name: must be 1-50 alphanumeric, dash, or underscore",
				})
			}
			trimmedTags = append(trimmedTags, trimmed)
		}
	}

	var expiresAt *string
	if req.ExpiresIn != nil {
		if *req.ExpiresIn <= 0 || *req.ExpiresIn > 31536000 {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "expires_in must be a positive integer, max 31536000 (365 days)",
			})
		}
		t := time.Now().UTC().Add(time.Duration(*req.ExpiresIn) * time.Second).Format(time.RFC3339)
		expiresAt = &t
	}

	var code string
	if req.CustomCode != "" {
		if errMsg := shortcode.ValidateCustomCode(req.CustomCode); errMsg != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": errMsg})
		}

		exists, err := h.store.CodeExists(c.Request().Context(), req.CustomCode)
		if err != nil {
			slog.Error("code exists check failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		if exists {
			return c.JSON(http.StatusConflict, map[string]string{"error": "code already in use"})
		}
		code = req.CustomCode
	} else {
		ctx := c.Request().Context()
		var err error
		code, err = shortcode.GenerateUnique(h.config.DefaultCodeLength, func(candidate string) (bool, error) {
			return h.store.CodeExists(ctx, candidate)
		})
		if err != nil {
			slog.Error("generate unique code failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}

	link, err := h.store.CreateLink(c.Request().Context(), code, req.URL, expiresAt)
	if err != nil {
		if errors.Is(err, store.ErrCodeExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "code already in use"})
		}
		slog.Error("create link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	if len(trimmedTags) > 0 {
		if err := h.store.SetLinkTags(c.Request().Context(), link.ID, trimmedTags); err != nil {
			slog.Error("set link tags failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		link.Tags = trimmedTags
	}

	link.ShortURL = h.config.BaseURL + "/" + link.Code
	if link.Tags == nil {
		link.Tags = []string{}
	}

	return c.JSON(http.StatusCreated, link)
}

func (h *LinkHandler) List(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	var active *bool
	if activeStr := c.QueryParam("active"); activeStr != "" {
		b := activeStr == "true"
		active = &b
	}

	params := store.ListParams{
		Page:    page,
		PerPage: perPage,
		Search:  strings.TrimSpace(c.QueryParam("search")),
		Sort:    c.QueryParam("sort"),
		Order:   c.QueryParam("order"),
		Active:  active,
		Tag:     strings.TrimSpace(c.QueryParam("tag")),
	}

	result, err := h.store.ListLinks(c.Request().Context(), params)
	if err != nil {
		slog.Error("list links failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	// Batch load tags for all links
	ids := make([]int64, len(result.Links))
	for i, l := range result.Links {
		ids[i] = l.ID
	}
	tagMap, err := h.store.GetLinksTagsBatch(c.Request().Context(), ids)
	if err != nil {
		slog.Error("batch load tags failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	for i := range result.Links {
		result.Links[i].ShortURL = h.config.BaseURL + "/" + result.Links[i].Code
		result.Links[i].Tags = tagMap[result.Links[i].ID]
		if result.Links[i].Tags == nil {
			result.Links[i].Tags = []string{}
		}
	}

	if result.Links == nil {
		result.Links = []model.Link{}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"links":    result.Links,
		"total":    result.Total,
		"page":     page,
		"per_page": perPage,
	})
}

func (h *LinkHandler) Get(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	link, err := h.store.GetLinkByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("get link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	tags, err := h.store.GetLinkTags(c.Request().Context(), link.ID)
	if err != nil {
		slog.Error("get link tags failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	link.Tags = tags

	link.ShortURL = h.config.BaseURL + "/" + link.Code
	if link.Tags == nil {
		link.Tags = []string{}
	}

	return c.JSON(http.StatusOK, link)
}

func (h *LinkHandler) Update(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	var req UpdateLinkRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.ExpiresAt != nil {
		if _, err := time.Parse(time.RFC3339, *req.ExpiresAt); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "expires_at must be a valid RFC3339 timestamp"})
		}
	}

	// Validate tag names if provided
	if req.Tags != nil {
		for i, name := range *req.Tags {
			trimmed := strings.TrimSpace(name)
			(*req.Tags)[i] = trimmed
			if !validateTagName(trimmed) {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": "invalid tag name: must be 1-50 alphanumeric, dash, or underscore",
				})
			}
		}
	}

	link, err := h.store.UpdateLink(c.Request().Context(), id, req.IsActive, req.ExpiresAt)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("update link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	if req.Tags != nil {
		if err := h.store.SetLinkTags(c.Request().Context(), id, *req.Tags); err != nil {
			slog.Error("set link tags failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}

	// Fetch current tags
	tags, err := h.store.GetLinkTags(c.Request().Context(), link.ID)
	if err != nil {
		slog.Error("get link tags failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	link.Tags = tags

	link.ShortURL = h.config.BaseURL + "/" + link.Code
	if link.Tags == nil {
		link.Tags = []string{}
	}

	return c.JSON(http.StatusOK, link)
}

func (h *LinkHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	err = h.store.DeleteLink(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("delete link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *LinkHandler) QRCode(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid link ID"})
	}

	ctx := c.Request().Context()
	link, err := h.store.GetLinkByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("qr get link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	size := 256
	if s := c.QueryParam("size"); s != "" {
		parsed, err := strconv.Atoi(s)
		if err == nil {
			size = parsed
		}
	}
	if size < 128 {
		size = 128
	}
	if size > 1024 {
		size = 1024
	}

	shortURL := h.config.BaseURL + "/" + link.Code
	png, err := qr.Generate(shortURL, size)
	if err != nil {
		slog.Error("qr generation failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.Blob(http.StatusOK, "image/png", png)
}
