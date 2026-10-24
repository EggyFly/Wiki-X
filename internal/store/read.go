package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type readRouter struct {
	primary *pgxpool.Pool
	replica *pgxpool.Pool
}

func (r readRouter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := r.replica.Query(ctx, sql, args...)
	if err != nil && shouldReadFallback(err) {
		return r.primary.Query(ctx, sql, args...)
	}
	return rows, err
}

func (r readRouter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return readRow{primary: r.primary, replica: r.replica, ctx: ctx, sql: sql, args: args}
}

type readRow struct {
	primary *pgxpool.Pool
	replica *pgxpool.Pool
	ctx     context.Context
	sql     string
	args    []any
}

func (r readRow) Scan(dest ...any) error {
	err := r.replica.QueryRow(r.ctx, r.sql, r.args...).Scan(dest...)
	if err != nil && shouldReadFallback(err) {
		return r.primary.QueryRow(r.ctx, r.sql, r.args...).Scan(dest...)
	}
	return err
}

func shouldReadFallback(err error) bool {
	if err == nil || errors.Is(err, pgx.ErrNoRows) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var pgErr *pgconn.PgError
	return !errors.As(err, &pgErr)
}
