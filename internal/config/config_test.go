package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if cfg.Cache.Redis.Prefix != "wiki_x" {
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

func TestLoadMailConfig(t *testing.T) {
	path := writeConfig(t, "site:\n  base_url: https://wiki.example\nmail:\n  host: smtp.example.com\n  from: wiki@example.com\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Mail.Enabled() || cfg.Mail.Port != 587 {
		t.Fatalf("mail config = %+v", cfg.Mail)
	}

	path = writeConfig(t, "mail:\n  host: smtp.example.com\n  from: wiki@example.com\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected base_url requirement when mail is configured")
	}
	path = writeConfig(t, "mail:\n  host: smtp.example.com\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected host and from to be required together")
	}
	path = writeConfig(t, "site:\n  base_url: https://wiki.example\nmail:\n  host: smtp.example.com\n  from: not-an-email\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid mail.from error")
	}
}

func TestLoadRegistrationConfig(t *testing.T) {
	path := writeConfig(t, "registration:\n  mode: invite\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Registration.Mode != "invite" || cfg.Registration.DefaultGroup != "user" {
		t.Fatalf("registration = %+v", cfg.Registration)
	}
	path = writeConfig(t, "registration:\n  mode: open\n")
	if _, err := Load(path); err == nil {
		t.Fatal("open registration should require mail")
	}
	path = writeConfig(t, "site:\n  base_url: https://wiki.example\nmail:\n  host: smtp.example.com\n  from: wiki@example.com\nregistration:\n  mode: open\n")
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	path = writeConfig(t, "registration:\n  mode: everyone\n")
	if _, err := Load(path); err == nil {
		t.Fatal("unknown registration mode should fail")
	}
}

func TestLoadAuditConfig(t *testing.T) {
	path := writeConfig(t, "audit:\n  retention: 4320h\n  archive_dir: archives\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit.RetentionDuration() != 180*24*time.Hour {
		t.Fatalf("retention = %v", cfg.Audit.RetentionDuration())
	}
	if !filepath.IsAbs(cfg.Audit.ArchiveDir) || filepath.Base(cfg.Audit.ArchiveDir) != "archives" {
		t.Fatalf("archive dir = %q", cfg.Audit.ArchiveDir)
	}
	path = writeConfig(t, "audit:\n  retention: 720h\n")
	if _, err := Load(path); err == nil {
		t.Fatal("retention below 180 days should fail")
	}
	path = writeConfig(t, "audit:\n  retention: 4320h\n  interval: 0s\n")
	if _, err := Load(path); err == nil {
		t.Fatal("zero interval should fail")
	}
}

func TestLoadRateLimitConfig(t *testing.T) {
	path := writeConfig(t, "ratelimit:\n  enabled: true\n  requests: 100\n  route_requests: 10\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RateLimit.Enabled || cfg.RateLimit.Requests != 100 || cfg.RateLimit.Window != "1m" {
		t.Fatalf("ratelimit = %+v", cfg.RateLimit)
	}
	if cfg.RateLimit.WindowDuration() != time.Minute || cfg.RateLimit.RouteWindowDuration() != time.Minute {
		t.Fatalf("durations = %v, %v", cfg.RateLimit.WindowDuration(), cfg.RateLimit.RouteWindowDuration())
	}
	path = writeConfig(t, "ratelimit:\n  requests: 0\n  window: 0s\n")
	if _, err := Load(path); err == nil {
		t.Fatal("zero window should fail")
	}
	path = writeConfig(t, "ratelimit:\n  route_requests: -1\n")
	if _, err := Load(path); err == nil {
		t.Fatal("negative route limit should fail")
	}
}

func TestSearchAndMediaRateLimitDefaults(t *testing.T) {
	path := writeConfig(t, "site:\n  title: Test\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search.RateLimitValue() != 30 || cfg.Media.RateLimitValue() != 600 {
		t.Fatalf("defaults = %d, %d", cfg.Search.RateLimitValue(), cfg.Media.RateLimitValue())
	}
	path = writeConfig(t, "site:\n  title: Test\nsearch:\n  rate_limit: 0\nmedia:\n  rate_limit: 0\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search.RateLimitValue() != 0 || cfg.Media.RateLimitValue() != 0 {
		t.Fatalf("disabled = %d, %d", cfg.Search.RateLimitValue(), cfg.Media.RateLimitValue())
	}
}

func TestLoadSearchConfig(t *testing.T) {
	path := writeConfig(t, "search:\n  backend: memory\n")
	cfg, err := Load(path)
	if err != nil || cfg.Search.Backend != "memory" {
		t.Fatalf("search = %+v, %v", cfg, err)
	}
	path = writeConfig(t, "search:\n  backend: postgres\n")
	if _, err := Load(path); err == nil {
		t.Fatal("postgres search should require postgres content")
	}
	path = writeConfig(t, "content:\n  source: postgres\ndatabase:\n  url: postgres://x\nsearch:\n  backend: postgres\n")
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	path = writeConfig(t, "content:\n  source: postgres\ndatabase:\n  url: postgres://x\nsearch:\n  backend: meilisearch\n")
	if _, err := Load(path); err == nil {
		t.Fatal("meilisearch should require url")
	}
	path = writeConfig(t, "content:\n  source: postgres\ndatabase:\n  url: postgres://x\nsearch:\n  backend: meilisearch\n  url: http://127.0.0.1:7700\n  index: wiki_x\n")
	cfg, err = Load(path)
	if err != nil || cfg.Search.Index != "wiki_x" {
		t.Fatalf("meilisearch config = %+v, %v", cfg.Search, err)
	}
	path = writeConfig(t, "content:\n  source: postgres\ndatabase:\n  url: postgres://x\nsearch:\n  backend: meilisearch\n  url: http://127.0.0.1:7700\n  index: bad/index\n")
	if _, err := Load(path); err == nil {
		t.Fatal("invalid search index should fail")
	}
	path = writeConfig(t, "search:\n  backend: external\n")
	if _, err := Load(path); err == nil {
		t.Fatal("unknown search backend should fail")
	}
}

func TestLoadMetricsConfig(t *testing.T) {
	path := writeConfig(t, "metrics:\n  enabled: true\n")
	cfg, err := Load(path)
	if err != nil || !cfg.Metrics.Enabled || cfg.Metrics.Path != "/-/metrics" {
		t.Fatalf("metrics = %+v, %v", cfg.Metrics, err)
	}
	path = writeConfig(t, "metrics:\n  enabled: true\n  path: metrics\n")
	if _, err := Load(path); err == nil {
		t.Fatal("relative metrics path should fail")
	}
	path = writeConfig(t, "metrics:\n  enabled: true\n  path: /assets/\n")
	if _, err := Load(path); err == nil {
		t.Fatal("reserved metrics path should fail")
	}
}

func TestLoadGitContentConfig(t *testing.T) {
	path := writeConfig(t, "content:\n  source: git\n  git_url: https://example.com/wiki.git\n  git_branch: dev\n  git_depth: 1\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Content.GitURL != "https://example.com/wiki.git" || cfg.Content.GitBranch != "dev" || cfg.Content.GitDepth != 1 {
		t.Fatalf("content = %+v", cfg.Content)
	}
	path = writeConfig(t, "content:\n  source: git\n  git_url: git@example.com:owner/repo.git\n")
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	path = writeConfig(t, "content:\n  source: git\n  git_url: https://example.com/wiki.git\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Content.GitBranch != "" {
		t.Fatalf("empty branch should keep remote default, got %q", cfg.Content.GitBranch)
	}
	for _, body := range []string{
		"content:\n  source: git\n  git_url: ftp://example.com/repo.git\n",
		"content:\n  source: git\n  git_url: ssh://-oProxyCommand=calc/repo.git\n",
		"content:\n  source: git\n  git_url: git@-oProxyCommand=calc:repo.git\n",
		"content:\n  source: git\n  git_url: https://example.com/wiki.git\n  git_branch: bad branch\n",
		"content:\n  source: git\n  git_url: https://example.com/wiki.git\n  git_depth: -1\n",
	} {
		path = writeConfig(t, body)
		if _, err := Load(path); err == nil {
			t.Fatalf("expected git content error for %q", body)
		}
	}
	path = writeConfig(t, "content:\n  source: postgres\n  git_url: https://example.com/wiki.git\ndatabase:\n  url: postgres://x\n")
	if _, err := Load(path); err != nil {
		t.Fatalf("postgres import source should accept git_url: %v", err)
	}
}

func TestLoadOSSMediaConfig(t *testing.T) {
	path := writeConfig(t, "media:\n  storage: oss\n  oss:\n    endpoint: https://oss-cn-hangzhou.aliyuncs.com\n    bucket: my-bucket\n    access_key_id: test-id\n    access_key_secret: test-secret\n    public_base_url: https://cdn.example.com/\n    path_prefix: /media/\n    acl: public-read\n    signed_url_ttl: 5m\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Media.Storage != "oss" || cfg.Media.OSS.ACL != "public-read" || cfg.Media.OSS.PathPrefix != "media" {
		t.Fatalf("media = %+v", cfg.Media)
	}
	if cfg.Media.OSS.PublicBaseURL != "https://cdn.example.com" || cfg.Media.OSS.SignedURLTTL != "5m" {
		t.Fatalf("oss = %+v", cfg.Media.OSS)
	}
	for _, body := range []string{
		"media:\n  storage: oss\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: https://oss.example.com\n    bucket: my-bucket\n    access_key_id: id\n    access_key_secret: secret\n    acl: public\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: ftp://oss.example.com\n    bucket: my-bucket\n    access_key_id: id\n    access_key_secret: secret\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: http://oss.example.com\n    bucket: my-bucket\n    access_key_id: id\n    access_key_secret: secret\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: https://oss.example.com\n    bucket: My_Bucket\n    access_key_id: id\n    access_key_secret: secret\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: https://oss.example.com\n    bucket: my-bucket\n    access_key_id: id\n    access_key_secret: secret\n    path_prefix: ../secret\n",
		"media:\n  storage: oss\n  oss:\n    endpoint: https://oss.example.com\n    bucket: my-bucket\n    access_key_id: id\n    access_key_secret: secret\n    signed_url_ttl: 200h\n",
	} {
		path = writeConfig(t, body)
		if _, err := Load(path); err == nil {
			t.Fatalf("expected oss config error for %q", body)
		}
	}
}

func TestLoadPrimaryReplicaDatabase(t *testing.T) {
	body := "database:\n  same: false\n  maintenance_database: postgres\n  primary:\n    address: 127.0.0.1:5432\n    username: postgres\n    password: primary-pass\n    database: wiki_x\n    params: sslmode=disable\n  replica:\n    address: 127.0.0.1:5433\n    username: postgres\n    password: replica-pass\n    database: wiki_x\n    params: sslmode=disable&connect_timeout=5\n"
	path := writeConfig(t, body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.SameDatabase() || !cfg.Database.Configured() {
		t.Fatalf("database = %+v", cfg.Database)
	}
	if cfg.Database.Primary.Database != "wiki_x" || cfg.Database.Replica.Address != "127.0.0.1:5433" {
		t.Fatalf("database = %+v", cfg.Database)
	}
	if cfg.Database.MaintenanceDatabase != "postgres" {
		t.Fatalf("maintenance database = %q", cfg.Database.MaintenanceDatabase)
	}
	for _, body := range []string{
		"content:\n  source: postgres\ndatabase:\n  same: false\n  primary:\n    address: 127.0.0.1:5432\n    database: wiki_x\n",
		"content:\n  source: postgres\ndatabase:\n  primary:\n    database: wiki_x\n",
		"database:\n  primary:\n    address: 127.0.0.1:5432\n    database: wiki_x\n    params: '%zz'\n",
		"database:\n  maintenance_database: bad/name\n  primary:\n    address: 127.0.0.1:5432\n    database: wiki_x\n",
		"content:\n  source: postgres\ndatabase:\n  same: false\n  primary:\n    address: 127.0.0.1:5432\n    database: wiki_x\n  replica:\n    address: 127.0.0.1:5433\n",
	} {
		path = writeConfig(t, body)
		if _, err := Load(path); err == nil {
			t.Fatalf("expected database error for %q", body)
		}
	}
}

func TestLoadOIDCConfig(t *testing.T) {
	const key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	body := "site:\n  title: Test\n  base_url: https://wiki.example.com/\ndatabase:\n  url: postgres://postgres@127.0.0.1:5432/wiki_x\noidc:\n  enabled: true\n  key_encryption_key: " + key + "\n"
	path := writeConfig(t, body)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OIDC.Enabled || cfg.OIDC.Issuer != "https://wiki.example.com" || cfg.OIDC.AccessTTL != "5m" {
		t.Fatalf("oidc = %+v", cfg.OIDC)
	}
	for _, body := range []string{
		"site:\n  base_url: https://wiki.example.com\noidc:\n  enabled: true\n",
		"site:\n  base_url: https://wiki.example.com\ndatabase:\n  url: postgres://postgres@127.0.0.1/wiki_x\noidc:\n  enabled: true\n  key_encryption_key: " + key + "\n  issuer: ftp://wiki.example.com\n",
		"site:\n  base_url: https://wiki.example.com\ndatabase:\n  url: postgres://postgres@127.0.0.1/wiki_x\noidc:\n  enabled: true\n  key_encryption_key: " + key + "\n  issuer: https://wiki.example.com/sub\n",
		"site:\n  base_url: https://wiki.example.com\ndatabase:\n  url: postgres://postgres@127.0.0.1/wiki_x\noidc:\n  enabled: true\n  key_encryption_key: bad\n",
		"site:\n  base_url: https://wiki.example.com\ndatabase:\n  url: postgres://postgres@127.0.0.1/wiki_x\noidc:\n  enabled: true\n  key_encryption_key: " + key + "\n  access_ttl: nope\n",
	} {
		path = writeConfig(t, body)
		if _, err := Load(path); err == nil {
			t.Fatalf("expected oidc error for %q", body)
		}
	}
}

func TestLoadRevokePolicy(t *testing.T) {
	path := writeConfig(t, "site:\n  title: Test\n")
	policyPath := filepath.Join(filepath.Dir(path), "revoke.json")
	if err := os.WriteFile(policyPath, []byte(`{"sections":{"group":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Revoke.Enabled("group") {
		t.Fatal("group revoke should be disabled")
	}
	if !cfg.Revoke.Enabled("entity") {
		t.Fatal("missing section should default to enabled")
	}
	if err := os.WriteFile(policyPath, []byte(`{"sections":{"unknown":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown section should fail")
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
	t.Setenv("SERVER_ADDR", "127.0.0.1:9090")
	t.Setenv("REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")

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
	t.Setenv("REDIS_DB", "abc")
	path := writeConfig(t, "site:\n  title: Test\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected redis db env error")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wiki_x.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
