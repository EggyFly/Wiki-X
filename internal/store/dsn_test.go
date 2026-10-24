package store

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestNodeDSN(t *testing.T) {
	dsn, err := nodeDSN(config.DatabaseNode{
		Address:  "127.0.0.1",
		Username: "postgres",
		Password: "p@ss:/word",
		Database: "wiki_x",
		Params:   "?sslmode=disable&connect_timeout=5",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "127.0.0.1:5432" || parsed.Path != "/wiki_x" {
		t.Fatalf("dsn = %s", dsn)
	}
	if parsed.User.Username() != "postgres" {
		t.Fatalf("username = %q", parsed.User.Username())
	}
	if password, _ := parsed.User.Password(); password != "p@ss:/word" {
		t.Fatalf("password = %q", password)
	}
	if parsed.RawQuery != "sslmode=disable&connect_timeout=5" {
		t.Fatalf("query = %q", parsed.RawQuery)
	}
}

func TestPrimaryDSNLegacyURL(t *testing.T) {
	dsn, err := primaryDSN(config.DatabaseConfig{URL: "postgres://postgres:secret@127.0.0.1:5432/wiki_x?sslmode=disable"})
	if err != nil || !strings.Contains(dsn, "wiki_x") {
		t.Fatalf("dsn = %q, %v", dsn, err)
	}
}

func TestNodeDSNRejectsBadAddress(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1:bad"} {
		if _, err := nodeDSN(config.DatabaseNode{Address: address, Database: "wiki_x"}); err == nil {
			t.Fatalf("%q should fail", address)
		}
	}
	dsn, err := nodeDSN(config.DatabaseNode{Address: "::1", Database: "wiki_x"})
	if err != nil || !strings.Contains(dsn, "[::1]:5432") {
		t.Fatalf("ipv6 dsn = %q, %v", dsn, err)
	}
}

func TestDatabaseNameAndMaintenanceDSN(t *testing.T) {
	cfg := config.DatabaseConfig{
		MaintenanceDatabase: "template1",
		Primary:             config.DatabaseNode{Address: "127.0.0.1:5432", Database: "wiki_x"},
	}
	name, err := databaseName(cfg)
	if err != nil || name != "wiki_x" {
		t.Fatalf("name = %q, %v", name, err)
	}
	dsn, err := maintenanceDSN(cfg)
	if err != nil || !strings.Contains(dsn, "/template1") {
		t.Fatalf("maintenance dsn = %q, %v", dsn, err)
	}
	legacy := config.DatabaseConfig{URL: "postgres://postgres@127.0.0.1:5432/wiki_x?sslmode=disable"}
	name, err = databaseName(legacy)
	if err != nil || name != "wiki_x" {
		t.Fatalf("legacy name = %q, %v", name, err)
	}
	dsn, err = maintenanceDSN(legacy)
	if err != nil || !strings.Contains(dsn, "/postgres") {
		t.Fatalf("legacy maintenance dsn = %q, %v", dsn, err)
	}
}

func TestOpenPrimaryReplica(t *testing.T) {
	raw := os.Getenv("TEST_POSTGRES_URL")
	if raw == "" {
		t.Skip("set TEST_POSTGRES_URL to run postgres integration tests")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	node := config.DatabaseNode{
		Address:  parsed.Host,
		Username: parsed.User.Username(),
		Password: password,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		Params:   parsed.RawQuery,
	}
	same := false
	ctx := context.Background()
	s, err := Open(ctx, config.DatabaseConfig{
		Same:     &same,
		Primary:  node,
		Replica:  node,
		MaxConns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var value int
	if err := s.read().QueryRow(ctx, `SELECT 1`).Scan(&value); err != nil || value != 1 {
		t.Fatalf("read replica = %d, %v", value, err)
	}
	s.replica.Close()
	if err := s.read().QueryRow(ctx, `SELECT 2`).Scan(&value); err != nil || value != 2 {
		t.Fatalf("read fallback = %d, %v", value, err)
	}
}
