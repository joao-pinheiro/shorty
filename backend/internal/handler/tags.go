package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"shorty/internal/model"
	"shorty/internal/store"
)

var tagNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,50}$`)

func validateTagName(name string) bool {
	return tagNameRegex.MatchString(name)
}

type TagHandler struct {
	store store.Store
}

func NewTagHandler(s store.Store) *TagHandler {
	return &TagHandler{store: s}
}

type createTagRequest struct {
	Name string `json:"name"`
}

func (h *TagHandler) List(c echo.Context) error {
	tags, err := h.store.ListTags(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	if tags == nil {
		tags = []model.TagWithCount{}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"tags": tags,
	})
}

func (h *TagHandler) Create(c echo.Context) error {
	var req createTagRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	req.Name = strings.TrimSpace(req.Name)
	if !validateTagName(req.Name) {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid tag name: must be 1-50 alphanumeric, dash, or underscore",
		})
	}

	count, err := h.store.TagCount(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	if count >= 100 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "tag limit reached (max 100)"})
	}

	tag, err := h.store.CreateTag(c.Request().Context(), req.Name)
	if err != nil {
		if errors.Is(err, store.ErrTagExists) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "tag already exists"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.JSON(http.StatusCreated, tag)
}

func (h *TagHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
	}

	err = h.store.DeleteTag(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTagNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.NoContent(http.StatusNoContent)
}
