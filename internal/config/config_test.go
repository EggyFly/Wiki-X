package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndPaths(t *testing.T) {
	path := writeConfig(t, "site:\n  title: Test\ncontent:\n  dir: docs\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Content.Source != "git" {
		t.Fatalf("source = %q", cfg.Content.Source)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Server.AccessLog {
		t.Fatal("access log should be off by default")
	}
	if cfg.Site.Language != "zh-CN" {
		t.Fatalf("language = %q", cfg.Site.Language)
	}
	if cfg.Content.MaxFileSize != 1<<20 {
		t.Fatalf("max file size = %d", cfg.Content.MaxFileSize)
	}
	if cfg.Content.ReloadInterval != "0s" {
		t.Fatalf("reload interval = %q", cfg.Content.ReloadInterval)
	}
	if cfg.Cache.Driver != "auto" || cfg.Cache.TTL != "10m" {
		t.Fatalf("cache defaults = %+v", cfg.Cache)
	}
	if cfg.Cache.MaxEntries != 2000 || cfg.Cache.MaxBytes != 64<<20 {
		t.Fatalf("cache limits = %+v", cfg.Cache)
	}
	if cfg.Cache.Redis.Prefix != "wikix" {
		t.Fatalf("redis prefix = %q", cfg.Cache.Redis.Prefix)
	}
	if cfg.Comment.RateLimit != 5 || cfg.Comment.RateWindow != "1m" {
		t.Fatalf("comment defaults = %+v", cfg.Comment)
	}
	wantDir := filepath.Join(filepath.Dir(path), "docs")
	if cfg.Content.Dir != wantDir {
		t.Fatalf("content dir = %q, want %q", cfg.Content.Dir, wantDir)
	}
}

func TestLoadCommentBlockedWords(t *testing.T) {
	path := writeConfig(t, "comment:\n  blocked_words: [\" Spam \", \"广告\"]\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Comment.BlockedWords) != 2 || cfg.Comment.BlockedWords[0] != "spam" || cfg.Comment.BlockedWords[1] != "广告" {
		t.Fatalf("blocked words = %+v", cfg.Comment.BlockedWords)
	}
}

func TestLoadRejectsBadCommentWindow(t *testing.T) {
	path := writeConfig(t, "comment:\n  rate_window: zero\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected bad comment window error")
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	path := writeConfig(t, "auth:\n  trusted_proxies:\n    - 10.0.0.0/8\n    - 2001:db8::/32\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Auth.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies = %+v", cfg.Auth.TrustedProxies)
	}

	path = writeConfig(t, "auth:\n  trusted_proxies:\n    - 10.0.0.1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected bad trusted proxy error")
	}
}

func TestLoadAcceptsAccessLog(t *testing.T) {
	path := writeConfig(t, "server:\n  access_log: true\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Server.AccessLog {
		t.Fatal("access log should be enabled")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, "site:\n  titel: typo\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadRejectsUnknownSource(t *testing.T) {
	path := writeConfig(t, "content:\n  source: mysql\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown source error")
	}
}

func TestLoadRejectsBadNav(t *testing.T) {
	path := writeConfig(t, "site:\n  nav:\n    - title: Outside\n      href: https://example.com\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected nav error")
	}
}

func TestLoadRejectsBadBaseURL(t *testing.T) {
	path := writeConfig(t, "site:\n  base_url: localhost:8080\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected base url error")
	}
}

func TestLoadRejectsEmptyFile(t *testing.T) {
	path := writeConfig(t, "")
	if _, err := Load(path); err == nil {
		t.Fatal("expected empty config error")
	}
}

func TestLoadRejectsBadCacheTTL(t *testing.T) {
	for _, ttl := range []string{"abc", "0s", "-1s"} {
		path := writeConfig(t, "cache:\n  ttl: "+ttl+"\n")
		if _, err := Load(path); err == nil {
			t.Fatalf("ttl %q should fail", ttl)
		}
	}
}

func TestLoadRejectsBadCacheDriver(t *testing.T) {
	path := writeConfig(t, "cache:\n  driver: disk\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected cache driver error")
	}
}

func TestLoadRejectsRedisWithoutAddr(t *testing.T) {
	path := writeConfig(t, "cache:\n  driver: redis\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected redis address error")
	}
}

func TestLoadRejectsBadReloadInterval(t *testing.T) {
	path := writeConfig(t, "content:\n  reload_interval: -1s\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected reload interval error")
	}
}

func TestLoadRejectsNegativeMaxFileSize(t *testing.T) {
	path := writeConfig(t, "content:\n  max_file_size: -1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected max file size error")
	}
}

func TestLoadLocales(t *testing.T) {
	path := writeConfig(t, `site:
  language: zh-CN
  locales:
    - code: zh-CN
      label: 中文
      prefix: ""
    - code: en
      label: English
      prefix: en
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	locales := cfg.Site.LocaleConfigs()
	if len(locales) != 2 || locales[1].Prefix != "en" {
		t.Fatalf("locales = %+v", locales)
	}
}

func TestLoadRejectsBadLocales(t *testing.T) {
	cases := []string{
		"site:\n  language: zh-CN\n  locales:\n    - code: en\n      prefix: en\n",
		"site:\n  language: zh-CN\n  locales:\n    - code: zh-CN\n      prefix: \"\"\n    - code: zh-CN\n      prefix: en\n",
		"site:\n  language: zh-CN\n  locales:\n    - code: zh-CN\n      prefix: \"\"\n    - code: en\n      prefix: a/b\n",
	}
	for _, body := range cases {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Fatalf("expected locale error for %q", body)
		}
	}
}

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("WIKIX_SERVER_ADDR", "127.0.0.1:9090")
	t.Setenv("WIKIX_REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("WIKIX_REDIS_PASSWORD", "secret")
	t.Setenv("WIKIX_REDIS_DB", "2")

	path := writeConfig(t, "cache:\n  driver: auto\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "127.0.0.1:9090" {
		t.Fatalf("server addr = %q", cfg.Server.Addr)
	}
	if cfg.Cache.Redis.Addr != "127.0.0.1:6380" || cfg.Cache.Redis.Password != "secret" || cfg.Cache.Redis.DB != 2 {
		t.Fatalf("redis config = %+v", cfg.Cache.Redis)
	}
}

func TestLoadRejectsBadRedisDBEnv(t *testing.T) {
	t.Setenv("WIKIX_REDIS_DB", "abc")
	path := writeConfig(t, "site:\n  title: Test\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected redis db env error")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wikix.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
