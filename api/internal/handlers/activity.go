package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

func (s *Server) ListActivity(c *gin.Context) {
	u := middleware.User(c)
	groupID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(c, http.StatusBadRequest, "bad_request", "limit must be an integer")
			return
		}
		limit = n
	}
	cursor := c.Query("cursor")
	page, err := s.Activity.List(c.Request.Context(), u.ID, groupID, limit, cursor)
	switch {
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case errors.Is(err, service.ErrBadCursor):
		writeErr(c, http.StatusBadRequest, "bad_request", "invalid cursor")
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, toAPIActivityPage(page))
}

func toAPIActivityPage(p *service.ActivityPage) apigen.ActivityPage {
	out := apigen.ActivityPage{
		Items: make([]apigen.ActivityItem, 0, len(p.Items)),
	}
	for _, item := range p.Items {
		ai := apigen.ActivityItem{
			Kind:       apigen.ActivityItemKind(item.Kind),
			OccurredAt: item.OccurredAt,
		}
		if item.Cadence != "" {
			c := apigen.Cadence(item.Cadence)
			ai.Cadence = &c
		}
		switch item.Kind {
		case repo.ActivityExpense:
			if item.Expense != nil {
				e := toAPIExpense(item.Expense)
				ai.Expense = &e
			}
		case repo.ActivitySettlement:
			if item.Settlement != nil {
				st := toAPISettlement(item.Settlement)
				ai.Settlement = &st
			}
		}
		out.Items = append(out.Items, ai)
	}
	if p.NextCursor != "" {
		nc := p.NextCursor
		out.NextCursor = &nc
	}
	return out
}
