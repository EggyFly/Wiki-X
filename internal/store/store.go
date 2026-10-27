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
	"github.com/EggyFly/Wiki-X/internal/revoke"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	ErrNotFound           = errors.New("not found")
	ErrEditConflict       = errors.New("edit conflict")
	ErrRouteExists        = errors.New("route already exists")
	ErrRevokeDisabled     = errors.New("deleting this section is disabled")
	ErrIdentityTaken      = errors.New("identity is already linked to another user")
	ErrProtected          = errors.New("page is protected")
	ErrBadProtection      = errors.New("unknown protection level")
	ErrLastAdmin          = errors.New("at least one enabled admin is required")
	ErrBadGroup           = errors.New("unknown group")
	ErrUserExists         = errors.New("user already exists")
	ErrEmailExists        = errors.New("email already exists")
	ErrBadEntity          = errors.New("entity data is invalid")
	ErrBadInteractive     = errors.New("interactive node is invalid")
	ErrInteractiveExists  = errors.New("interactive node already exists")
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
	policy  revoke.Policy
}

func (s *Store) SetRevokePolicy(policy revoke.Policy) {
	s.policy = policy
}

func (s *Store) revokeEnabled(section string) bool {
	return s.policy.Enabled(section)
}

const (
	SectionPage        = revoke.SectionPage
	SectionUser        = revoke.SectionUser
	SectionGroup       = revoke.SectionGroup
	SectionEntity      = revoke.SectionEntity
	SectionInvite      = revoke.SectionInvite
	SectionOIDCClient  = revoke.SectionOIDCClient
	SectionWatchlist   = revoke.SectionWatchlist
	SectionInteractive = revoke.SectionInteractive
)

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

// strong 专供启动加载和刚写完立即读的场景, 永远读主库, 避免复制延迟读到旧内容.
func (s *Store) strong() readQuerier {
	return s.pool
}

// BeginTx 开启主库事务; 事务对象复用 Store 的方法签名, 业务层在事务内外调用同一套代码.
func (s *Store) BeginTx(ctx context.Context) (*TxHandle, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &TxHandle{store: s, tx: tx}, nil
}

type TxHandle struct {
	store *Store
	tx    pgx.Tx
}

func (h *TxHandle) Rollback(ctx context.Context) error {
	return h.tx.Rollback(ctx)
}

func (h *TxHandle) Commit(ctx context.Context) error {
	return h.tx.Commit(ctx)
}

func (h *TxHandle) ListEntityTypes(ctx context.Context) ([]*EntityType, error) {
	return h.store.listEntityTypes(ctx, h.tx)
}

func (h *TxHandle) CreateEntityType(ctx context.Context, name, label string, actorID int64) (*EntityType, error) {
	return h.store.createEntityTypeTx(ctx, h.tx, name, label, actorID)
}

func (h *TxHandle) AddEntityField(ctx context.Context, typeID int64, name, label, kind string, required bool, options []string, sortOrder int, actorID int64) error {
	return h.store.addEntityFieldTx(ctx, h.tx, typeID, name, label, kind, required, options, sortOrder, actorID)
}

func (h *TxHandle) GetEntityByName(ctx context.Context, name string) (*EntityRecord, error) {
	row := h.tx.QueryRow(ctx, entitySelectSQL+` WHERE e.name = $1 AND e.revoked_at IS NULL AND t.revoked_at IS NULL`, name)
	item, err := scanEntityRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (h *TxHandle) SaveEntity(ctx context.Context, in EntityInput) (*EntityRecord, error) {
	return h.store.saveEntityTx(ctx, h.tx, in)
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
