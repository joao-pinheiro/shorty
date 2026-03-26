package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/clickrecorder"
	"shorty/internal/store"
)

type RedirectHandler struct {
	store    store.Store
	recorder *clickrecorder.Recorder
}

func NewRedirectHandler(s store.Store, recorder *clickrecorder.Recorder) *RedirectHandler {
	return &RedirectHandler{store: s, recorder: recorder}
}

func (h *RedirectHandler) Redirect(c echo.Context) error {
	code := c.Param("code")
	code = strings.TrimSuffix(code, "/")

	if code == "" {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	link, err := h.store.GetLinkByCode(c.Request().Context(), code)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("redirect lookup failed", "error", err, "code", code)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	if !link.IsActive {
		return c.JSON(http.StatusGone, map[string]string{"error": "link is deactivated"})
	}

	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now().UTC()) {
		if err := h.store.DeactivateExpiredLink(c.Request().Context(), link.ID); err != nil {
			slog.Error("lazy deactivation failed", "error", err, "link_id", link.ID)
		}
		return c.JSON(http.StatusGone, map[string]string{"error": "link has expired"})
	}

	h.recorder.Record(link.ID)

	c.Response().Header().Set("X-Frame-Options", "DENY")
	c.Response().Header().Set("Cache-Control", "private, max-age=0")

	return c.Redirect(http.StatusFound, link.OriginalURL)
}
