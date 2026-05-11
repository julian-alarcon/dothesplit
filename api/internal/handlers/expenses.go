package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/julian-alarcon/dothesplit/api/internal/apigen"
	"github.com/julian-alarcon/dothesplit/api/internal/middleware"
	"github.com/julian-alarcon/dothesplit/api/internal/repo"
	"github.com/julian-alarcon/dothesplit/api/internal/service"
)

func (s *Server) ListExpenses(c *gin.Context) {
	u := middleware.User(c)
	groupID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	exps, err := s.Expenses.List(c.Request.Context(), u.ID, groupID)
	if err != nil {
		if errors.Is(err, service.ErrNotMember) {
			writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
			return
		}
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]apigen.Expense, 0, len(exps))
	for i := range exps {
		out = append(out, toAPIExpense(&exps[i]))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) CreateExpense(c *gin.Context) {
	u := middleware.User(c)
	groupID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req apigen.CreateExpenseRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	incurredAt := time.Now().UTC()
	if req.IncurredAt != nil {
		incurredAt = *req.IncurredAt
	}
	currency := ""
	if req.Currency != nil {
		currency = *req.Currency
	}

	splits := make([]service.SplitInput, len(req.Splits))
	for i, sp := range req.Splits {
		v := int64(0)
		if sp.Value != nil {
			v = *sp.Value
		}
		splits[i] = service.SplitInput{UserID: sp.UserId, Value: v}
	}

	out, err := s.Expenses.Create(c.Request.Context(), u.ID, service.CreateExpenseInput{
		GroupID:     groupID,
		PayerID:     req.PayerId,
		CategoryID:  req.CategoryId,
		AmountCents: req.AmountCents,
		Currency:    currency,
		Description: req.Description,
		IncurredAt:  incurredAt,
		Mode:        service.SplitMode(req.Mode),
		Splits:      splits,
	})
	switch {
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case errors.Is(err, service.ErrUnknownCategory):
		writeErr(c, http.StatusBadRequest, "bad_request", "unknown category_id")
		return
	case errors.Is(err, service.ErrPayerNotMember), errors.Is(err, service.ErrSplitNotMember),
		errors.Is(err, service.ErrBadSplit):
		writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toAPIExpense(out))
}

// GetExpense returns one expense (member-only).
func (s *Server) GetExpense(c *gin.Context) {
	u := middleware.User(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	e, err := s.Expenses.Get(c.Request.Context(), u.ID, id)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeErr(c, http.StatusNotFound, "not_found", "expense not found")
		return
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, toAPIExpense(e))
}

// UpdateExpense edits description / amount / category / payer / splits.
// Any group member may edit; the change is recorded in the revision history.
func (s *Server) UpdateExpense(c *gin.Context) {
	u := middleware.User(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req apigen.UpdateExpenseRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	in := service.UpdateExpenseInput{
		Description: req.Description,
		AmountCents: req.AmountCents,
		CategoryID:  req.CategoryId,
		PayerID:     req.PayerId,
		IncurredAt:  req.IncurredAt,
	}
	if req.Mode != nil {
		m := service.SplitMode(*req.Mode)
		in.Mode = &m
	}
	if req.Splits != nil {
		splits := make([]service.SplitInput, len(*req.Splits))
		for i, sp := range *req.Splits {
			v := int64(0)
			if sp.Value != nil {
				v = *sp.Value
			}
			splits[i] = service.SplitInput{UserID: sp.UserId, Value: v}
		}
		in.Splits = splits
	}

	out, err := s.Expenses.Update(c.Request.Context(), u.ID, id, in)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeErr(c, http.StatusNotFound, "not_found", "expense not found")
		return
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case errors.Is(err, service.ErrUnknownCategory):
		writeErr(c, http.StatusBadRequest, "bad_request", "unknown category_id")
		return
	case errors.Is(err, service.ErrPayerNotMember):
		writeErr(c, http.StatusBadRequest, "bad_request", "payer is not a group member")
		return
	case errors.Is(err, service.ErrSplitNotMember):
		writeErr(c, http.StatusBadRequest, "bad_request", "split user is not a group member")
		return
	case errors.Is(err, service.ErrBadSplit):
		writeErr(c, http.StatusBadRequest, "bad_request", err.Error())
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, toAPIExpense(out))
}

// ListExpenseRevisions returns the edit history for an expense.
func (s *Server) ListExpenseRevisions(c *gin.Context) {
	u := middleware.User(c)
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	revs, err := s.Expenses.ListRevisions(c.Request.Context(), u.ID, id)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeErr(c, http.StatusNotFound, "not_found", "expense not found")
		return
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]apigen.ExpenseRevision, 0, len(revs))
	for i := range revs {
		r := revs[i]
		out = append(out, apigen.ExpenseRevision{
			Id:        r.ID,
			ExpenseId: r.ExpenseID,
			EditedBy:  r.EditedBy,
			EditedAt:  r.EditedAt,
			Field:     apigen.ExpenseRevisionField(r.Field),
			OldValue:  r.OldValue,
			NewValue:  r.NewValue,
		})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) DeleteExpense(c *gin.Context) {
	u := middleware.User(c)
	expenseID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	err := s.Expenses.Delete(c.Request.Context(), u.ID, expenseID)
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeErr(c, http.StatusNotFound, "not_found", "expense not found")
		return
	case errors.Is(err, service.ErrNotMember):
		writeErr(c, http.StatusForbidden, "forbidden", "not a group member")
		return
	case err != nil:
		writeErr(c, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func toAPIExpense(e *repo.Expense) apigen.Expense {
	splits := make([]apigen.Split, 0, len(e.Splits))
	for _, sp := range e.Splits {
		splits = append(splits, apigen.Split{UserId: sp.UserID, ShareCents: sp.ShareCents})
	}
	return apigen.Expense{
		Id:          e.ID,
		GroupId:     e.GroupID,
		PayerId:     e.PayerID,
		CreatedBy:   e.CreatedBy,
		CategoryId:  e.CategoryID,
		AmountCents: e.AmountCents,
		Currency:    e.Currency,
		Description: e.Description,
		IncurredAt:  e.IncurredAt,
		CreatedAt:   e.CreatedAt,
		Splits:      splits,
	}
}
