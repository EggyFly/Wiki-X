package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/EggyFly/Wiki-X/internal/config"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	ErrNotFound           = errors.New("not found")
	ErrEditConflict       = errors.New("edit conflict")
	ErrIdentityTaken      = errors.New("identity is already linked to another user")
	ErrProtected          = errors.New("page is protected")
	ErrBadProtection      = errors.New("unknown protection level")
	ErrLastAdmin          = errors.New("at least one enabled admin is required")
	ErrBadGroup           = errors.New("unknown group")
	ErrUserExists         = errors.New("user already exists")
	ErrEmailExists        = errors.New("email already exists")
	ErrBadEntity          = errors.New("entity data is invalid")
	ErrEntityExists       = errors.New("entity already exists")
	ErrEntityTypeExists   = errors.New("entity type already exists")
	ErrEntityFieldExists  = errors.New("entity field already exists")
	ErrEntityTypeNotEmpty = errors.New("entity type still has entities")
	ErrGroupExists        = errors.New("group already exists")
	ErrBadRight           = errors.New("unknown right")
	ErrBuiltinGroup       = errors.New("builtin group cannot be deleted")
)

type Store struct {
	pool    *pgxpool.Pool
	replica *pgxpool.Pool
}

func Open(ctx context.Context, cfg config.DatabaseConfig) (*Store, error) {
	primary, err := primaryDSN(cfg)
	if err != nil {
		return nil, err
	}
	pool, err := openPool(ctx, primary, cfg.MaxConns)
	if err != nil {
		return nil, err
	}
	replica := pool
	if !cfg.SameDatabase() {
		replicaURL, err := replicaDSN(cfg)
		if err != nil {
			pool.Close()
			return nil, err
		}
		replica, err = openPool(ctx, replicaURL, cfg.MaxConns)
		if err != nil {
			pool.Close()
			return nil, err
		}
	}
	return &Store{pool: pool, replica: replica}, nil
}

func openPool(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		poolCfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func (s *Store) read() readQuerier {
	if s.replica == nil || s.replica == s.pool {
		return s.pool
	}
	return readRouter{primary: s.pool, replica: s.replica}
}

func (s *Store) Close() {
	if s.replica != nil && s.replica != s.pool {
		s.replica.Close()
	}
	s.pool.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return err
	}
	if s.replica != nil && s.replica != s.pool {
		return s.replica.Ping(ctx)
	}
	return nil
}

const migrationLockKey int64 = 0x57495858

func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		data, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if err := runMigration(ctx, tx, string(data)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runMigration(ctx context.Context, tx pgx.Tx, script string) error {
	results, err := tx.Conn().PgConn().Exec(ctx, script).ReadAll()
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}
