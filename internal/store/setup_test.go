package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestEnsureDatabaseConcurrent(t *testing.T) {
	raw := os.Getenv("TEST_POSTGRES_URL")
	if raw == "" {
		t.Skip("set TEST_POSTGRES_URL to run postgres integration tests")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	name := fmt.Sprintf("wiki_x_setup_%d", time.Now().UnixNano())
	cfg := config.DatabaseConfig{
		Same:                boolPointer(true),
		MaintenanceDatabase: "postgres",
		Primary: config.DatabaseNode{
			Address:  parsed.Host,
			Username: parsed.User.Username(),
			Password: password,
			Database: name,
			Params:   parsed.RawQuery,
		},
	}
	t.Cleanup(func() {
		maintenance, err := maintenanceDSN(cfg)
		if err != nil {
			return
		}
		conn, err := pgx.Connect(context.Background(), maintenance)
		if err != nil {
			return
		}
		defer conn.Close(context.Background())
		_, _ = conn.Exec(context.Background(), `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize())
	})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- EnsureDatabase(context.Background(), cfg)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckReplica(t *testing.T) {
	if err := CheckReplica(context.Background(), config.DatabaseConfig{}); err != nil {
		t.Fatal(err)
	}
	same := false
	err := CheckReplica(context.Background(), config.DatabaseConfig{Same: &same})
	if err == nil || !strings.Contains(err.Error(), "replica") {
		t.Fatalf("err = %v", err)
	}
}

func boolPointer(value bool) *bool {
	return &value
}
