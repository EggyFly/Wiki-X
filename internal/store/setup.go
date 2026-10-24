package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func EnsureDatabase(ctx context.Context, cfg config.DatabaseConfig) error {
	dbName, err := databaseName(cfg)
	if err != nil {
		return err
	}
	target, err := primaryDSN(cfg)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, target)
	if err == nil {
		return conn.Close(ctx)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "3D000" {
		return fmt.Errorf("connect database: %w", err)
	}

	maintenance, err := maintenanceDSN(cfg)
	if err != nil {
		return err
	}
	admin, err := pgx.Connect(ctx, maintenance)
	if err != nil {
		return fmt.Errorf("connect maintenance database %q: %w", maintenanceName(cfg), err)
	}
	defer admin.Close(ctx)

	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbName).Scan(&exists); err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()); err != nil {
		var createErr *pgconn.PgError
		if errors.As(err, &createErr) {
			switch createErr.Code {
			case "42P04", "23505":
				return nil
			case "42501":
				return fmt.Errorf("create database %s: permission denied; grant CREATEDB or create it manually", dbName)
			}
		}
		return fmt.Errorf("create database %s: %w", dbName, err)
	}
	return nil
}

func CheckReplica(ctx context.Context, cfg config.DatabaseConfig) error {
	if cfg.SameDatabase() {
		return nil
	}
	dsn, err := replicaDSN(cfg)
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect replica database: %w", err)
	}
	return conn.Close(ctx)
}

func databaseName(cfg config.DatabaseConfig) (string, error) {
	if cfg.Primary.Database != "" {
		return cfg.Primary.Database, nil
	}
	if cfg.URL == "" {
		return "", errors.New("database is not configured")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		return "", errors.New("database name is empty")
	}
	return name, nil
}

func maintenanceDSN(cfg config.DatabaseConfig) (string, error) {
	name := maintenanceName(cfg)
	if cfg.Primary.Address != "" || cfg.Primary.Database != "" {
		node := cfg.Primary
		node.Database = name
		return nodeDSN(node)
	}
	if cfg.URL == "" {
		return "", errors.New("database is not configured")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	parsed.Path = "/" + name
	parsed.RawPath = ""
	return parsed.String(), nil
}

func maintenanceName(cfg config.DatabaseConfig) string {
	if cfg.MaintenanceDatabase != "" {
		return cfg.MaintenanceDatabase
	}
	return "postgres"
}
