package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvLine(t *testing.T) {
	cases := []struct {
		line  string
		key   string
		value string
		ok    bool
	}{
		{"", "", "", false},
		{"# comment", "", "", false},
		{"SERVER_ADDR=:8080", "SERVER_ADDR", ":8080", true},
		{"export SITE_TITLE=Test Wiki", "SITE_TITLE", "Test Wiki", true},
		{`MAIL_PASSWORD="a b\nc"`, "MAIL_PASSWORD", "a b\nc", true},
		{`X='keep $literal'`, "X", "keep $literal", true},
		{"URL=postgres://a:b=c@h/db", "URL", "postgres://a:b=c@h/db", true},
		{"SITE_TITLE=Wiki-X # 站点名", "SITE_TITLE", "Wiki-X", true},
		{"SITE_DESCRIPTION= # 空值", "SITE_DESCRIPTION", "", true},
		{"URL=https://wiki.example/a#b", "URL", "https://wiki.example/a#b", true},
		{`X="keep # hash"`, "X", "keep # hash", true},
		{`X="a b" # 行内注释`, "X", "a b", true},
		{`X='a b' # 行内注释`, "X", "a b", true},
		{`X="a # b" # 行内注释`, "X", "a # b", true},
	}
	for _, item := range cases {
		key, value, ok, err := parseEnvLine(item.line)
		if err != nil {
			t.Fatalf("%q: %v", item.line, err)
		}
		if ok != item.ok || key != item.key || value != item.value {
			t.Fatalf("%q = %q, %q, %v", item.line, key, value, ok)
		}
	}
	for _, line := range []string{"NOEQUALS", `X="unterminated`, `X="a" trailing`, "BAD-KEY=bad"} {
		if _, _, _, err := parseEnvLine(line); err == nil {
			t.Fatalf("%q should fail", line)
		}
	}
}

func TestLoadReadsDotEnv(t *testing.T) {
	unsetEnv(t, "SITE_DESCRIPTION")
	unsetEnv(t, "SERVER_ADDR")
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki_x.yaml")
	if err := os.WriteFile(path, []byte("site:\n  description: from yaml\nserver:\n  addr: \":1111\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	envBody := "# local settings\nSITE_TITLE=Inline # 行内注释\nSITE_DESCRIPTION=\"from env\"\nSERVER_ADDR=:2222\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Title != "Inline" || cfg.Site.Description != "from env" || cfg.Server.Addr != ":2222" {
		t.Fatalf("config = %q, %q, %q", cfg.Site.Title, cfg.Site.Description, cfg.Server.Addr)
	}

	t.Setenv("SERVER_ADDR", ":3333")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":3333" {
		t.Fatalf("process env should win, addr = %q", cfg.Server.Addr)
	}
}

func TestLoadDotEnvFileOverride(t *testing.T) {
	unsetEnv(t, "SITE_DESCRIPTION")
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki_x.yaml")
	if err := os.WriteFile(path, []byte("site:\n  title: Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(envPath, []byte("SITE_DESCRIPTION=custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV_FILE", envPath)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Description != "custom" {
		t.Fatalf("description = %q", cfg.Site.Description)
	}
}

func TestEnvValuesGetDefaults(t *testing.T) {
	unsetEnv(t, "MAIL_HOST")
	t.Setenv("MAIL_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM", "wiki@example.com")
	t.Setenv("SITE_BASE_URL", "https://wiki.example/")
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki_x.yaml")
	if err := os.WriteFile(path, []byte("site:\n  title: Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.Port != 587 {
		t.Fatalf("mail port = %d", cfg.Mail.Port)
	}
	if cfg.Site.BaseURL != "https://wiki.example" {
		t.Fatalf("base url = %q", cfg.Site.BaseURL)
	}
}

func TestLoadRejectsBadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki_x.yaml")
	if err := os.WriteFile(path, []byte("site:\n  title: Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("INVALID LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("bad env file should fail")
	}
}

func TestLoadDotEnvLineLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiki_x.yaml")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(strings.Repeat("X=a\n", 20000)), 0o644); err != nil {
		t.Fatal(err)
	}
	applied, err := loadDotEnv(path)
	restoreEnv(applied)
	if err != nil {
		t.Fatalf("20000 lines should load: %v", err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Repeat("X=a\n", 20001)), 0o644); err != nil {
		t.Fatal(err)
	}
	applied, err = loadDotEnv(path)
	restoreEnv(applied)
	if err == nil {
		t.Fatal("20001 lines should fail")
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
