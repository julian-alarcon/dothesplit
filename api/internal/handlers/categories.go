package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
)

// ListCategories returns the seeded category set (authenticated users only).
func (s *Server) ListCategories(c *gin.Context) {
	list, err := s.Categories.List(c.Request.Context())
	if err != nil {
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]apigen.Category, 0, len(list))
	for _, cat := range list {
		out = append(out, apigen.Category{
			Id:         cat.ID,
			Slug:       cat.Slug,
			Label:      cat.Label,
			Emoji:      cat.Emoji,
			GroupLabel: cat.GroupLabel,
		})
	}
	c.JSON(http.StatusOK, out)
}
