package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/model"
	"shorty/internal/store"
)

var allowedPeriods = map[string]bool{
	"24h": true,
	"7d":  true,
	"30d": true,
	"all": true,
}

type AnalyticsHandler struct {
	store store.Store
}

func NewAnalyticsHandler(s store.Store) *AnalyticsHandler {
	return &AnalyticsHandler{store: s}
}

func (h *AnalyticsHandler) Get(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid link ID"})
	}

	period := c.QueryParam("period")
	if period == "" {
		period = "7d"
	}
	if !allowedPeriods[period] {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "period must be one of: 24h, 7d, 30d, all",
		})
	}

	ctx := c.Request().Context()

	// Verify link exists
	_, err = h.store.GetLinkByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		slog.Error("analytics get link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	totalClicks, err := h.store.GetTotalClickCount(ctx, id)
	if err != nil {
		slog.Error("analytics get total clicks failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	now := time.Now().UTC()
	var since time.Time
	switch period {
	case "24h":
		since = now.Add(-24 * time.Hour)
	case "7d":
		since = now.AddDate(0, 0, -7)
	case "30d":
		since = now.AddDate(0, 0, -30)
	case "all":
		since = time.Time{}
	}

	periodClicks, err := h.store.GetPeriodClickCount(ctx, id, since)
	if err != nil {
		slog.Error("analytics get period clicks failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	response := map[string]interface{}{
		"link_id":       id,
		"total_clicks":  totalClicks,
		"period_clicks": periodClicks,
	}

	if period == "24h" {
		hours, err := h.store.GetClicksByHour(ctx, id, since)
		if err != nil {
			slog.Error("analytics get clicks by hour failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		if hours == nil {
			hours = []model.HourCount{}
		}
		response["clicks_by_hour"] = hours
	} else {
		days, err := h.store.GetClicksByDay(ctx, id, since)
		if err != nil {
			slog.Error("analytics get clicks by day failed", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		if days == nil {
			days = []model.DayCount{}
		}
		response["clicks_by_day"] = days
	}

	return c.JSON(http.StatusOK, response)
}
