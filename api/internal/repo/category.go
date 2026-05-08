package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Category struct {
	ID         uuid.UUID
	Slug       string
	Label      string
	Emoji      string
	Sort       int
	GroupLabel string
}

type CategoryRepo struct {
	pool *pgxpool.Pool
}

func NewCategoryRepo(p *pgxpool.Pool) *CategoryRepo { return &CategoryRepo{pool: p} }

// List returns every category in presentation order.
func (r *CategoryRepo) List(ctx context.Context) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, slug, label, emoji, sort, group_label FROM categories ORDER BY sort, label
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Slug, &c.Label, &c.Emoji, &c.Sort, &c.GroupLabel); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *CategoryRepo) FindByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	var c Category
	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, label, emoji, sort, group_label FROM categories WHERE id = $1
	`, id).Scan(&c.ID, &c.Slug, &c.Label, &c.Emoji, &c.Sort, &c.GroupLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CategoryRepo) FindBySlug(ctx context.Context, slug string) (*Category, error) {
	var c Category
	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, label, emoji, sort, group_label FROM categories WHERE slug = $1
	`, slug).Scan(&c.ID, &c.Slug, &c.Label, &c.Emoji, &c.Sort, &c.GroupLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
