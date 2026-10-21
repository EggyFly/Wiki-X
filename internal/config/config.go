package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Site     SiteConfig     `yaml:"site"`
	Content  ContentConfig  `yaml:"content"`
	Theme    ThemeConfig    `yaml:"theme"`
	Server   ServerConfig   `yaml:"server"`
	Cache    CacheConfig    `yaml:"cache"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	GitHub   GitHubConfig   `yaml:"github"`
	Media    MediaConfig    `yaml:"media"`
	Comment  CommentConfig  `yaml:"comment"`
}

type SiteConfig struct {
	Title       string         `yaml:"title"`
	Description string         `yaml:"description"`
	Language    string         `yaml:"language"`
	BaseURL     string         `yaml:"base_url"`
	CommunityUI bool           `yaml:"community_ui"`
	Static      bool           `yaml:"-"`
	Locales     []LocaleConfig `yaml:"locales"`
	Nav         []NavItem      `yaml:"nav"`
}

type LocaleConfig struct {
	Code   string `yaml:"code"`
	Label  string `yaml:"label"`
	Prefix string `yaml:"prefix"`
}

type NavItem struct {
	Title string `yaml:"title"`
	Href  string `yaml:"href"`
}

type ContentConfig struct {
	Source         string `yaml:"source"`
	Dir            string `yaml:"dir"`
	MaxFileSize    int64  `yaml:"max_file_size"`
	ReloadInterval string `yaml:"reload_interval"`
	GitPull        bool   `yaml:"git_pull"`
}

type ThemeConfig struct {
	Dir           string `yaml:"dir"`
	Root          string `yaml:"root"`
	Default       string `yaml:"default"`
	DisableSwitch bool   `yaml:"disable_switch"`
}

type ServerConfig struct {
	Addr      string `yaml:"addr"`
	AccessLog bool   `yaml:"access_log"`
}

type CacheConfig struct {
	Driver     string      `yaml:"driver"`
	TTL        string      `yaml:"ttl"`
	MaxEntries int         `yaml:"max_entries"`
	MaxBytes   int64       `yaml:"max_bytes"`
	Redis      RedisConfig `yaml:"redis"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Prefix   string `yaml:"prefix"`
}

type DatabaseConfig struct {
	URL         string `yaml:"url"`
	MaxConns    int32  `yaml:"max_conns"`
	AutoMigrate bool   `yaml:"auto_migrate"`
}

type AuthConfig struct {
	SecureCookies  bool              `yaml:"secure_cookies"`
	SessionTTL     string            `yaml:"session_ttl"`
	TrustedProxies []string          `yaml:"trusted_proxies"`
	GitHub         GitHubOAuthConfig `yaml:"github"`
}

type GitHubOAuthConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
}

type GitHubConfig struct {
	WebhookSecret  string `yaml:"webhook_secret"`
	Repo           string `yaml:"repo"`
	Token          string `yaml:"token"`
	AppID          int64  `yaml:"app_id"`
	InstallationID int64  `yaml:"installation_id"`
	PrivateKeyFile string `yaml:"private_key_file"`
	PrivateKeyPEM  string `yaml:"-"`
	BaseBranch     string `yaml:"base_branch"`
	ContentPath    string `yaml:"content_path"`
	PRImport       string `yaml:"pr_import"`
	APIBaseURL     string `yaml:"api_base_url"`
}

type MediaConfig struct {
	Dir     string `yaml:"dir"`
	MaxSize int64  `yaml:"max_size"`
}

type CommentConfig struct {
	RateLimit    int      `yaml:"rate_limit"`
	RateWindow   string   `yaml:"rate_window"`
	BlockedWords []string `yaml:"blocked_words"`
}

func Load(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config %s is empty", path)
		}
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.setDefault()
	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	cfg.resolvePaths(filepath.Dir(path))
	if cfg.Auth.GitHub.RedirectURL == "" && cfg.Auth.GitHubEnabled() && cfg.Site.BaseURL != "" {
		cfg.Auth.GitHub.RedirectURL = cfg.Site.BaseURL + "/auth/github/callback"
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnv() error {
	if value := strings.TrimSpace(os.Getenv("WIKIX_SERVER_ADDR")); value != "" {
		c.Server.Addr = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_REDIS_ADDR")); value != "" {
		c.Cache.Redis.Addr = value
	}
	if value, ok := os.LookupEnv("WIKIX_REDIS_PASSWORD"); ok {
		c.Cache.Redis.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_REDIS_DB")); value != "" {
		db, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("WIKIX_REDIS_DB: %w", err)
		}
		c.Cache.Redis.DB = db
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_DATABASE_URL")); value != "" {
		c.Database.URL = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_CLIENT_ID")); value != "" {
		c.Auth.GitHub.ClientID = value
	}
	if value, ok := os.LookupEnv("WIKIX_GITHUB_CLIENT_SECRET"); ok {
		c.Auth.GitHub.ClientSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_REDIRECT_URL")); value != "" {
		c.Auth.GitHub.RedirectURL = value
	}
	if value, ok := os.LookupEnv("WIKIX_GITHUB_WEBHOOK_SECRET"); ok {
		c.GitHub.WebhookSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_REPO")); value != "" {
		c.GitHub.Repo = value
	}
	if value, ok := os.LookupEnv("WIKIX_GITHUB_TOKEN"); ok {
		c.GitHub.Token = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_APP_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("WIKIX_GITHUB_APP_ID: %w", err)
		}
		c.GitHub.AppID = id
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_INSTALLATION_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("WIKIX_GITHUB_INSTALLATION_ID: %w", err)
		}
		c.GitHub.InstallationID = id
	}
	if value, ok := os.LookupEnv("WIKIX_GITHUB_PRIVATE_KEY"); ok {
		c.GitHub.PrivateKeyPEM = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_PRIVATE_KEY_FILE")); value != "" {
		c.GitHub.PrivateKeyFile = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_BASE_BRANCH")); value != "" {
		c.GitHub.BaseBranch = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_CONTENT_PATH")); value != "" {
		c.GitHub.ContentPath = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_PR_IMPORT")); value != "" {
		c.GitHub.PRImport = value
	}
	if value := strings.TrimSpace(os.Getenv("WIKIX_GITHUB_API_BASE_URL")); value != "" {
		c.GitHub.APIBaseURL = value
	}
	return nil
}

func (c *Config) setDefault() {
	if strings.TrimSpace(c.Site.Title) == "" {
		c.Site.Title = "Wiki-X"
	}
	if strings.TrimSpace(c.Site.Language) == "" {
		c.Site.Language = "zh-CN"
	}
	if c.Site.BaseURL != "" {
		c.Site.BaseURL = strings.TrimRight(c.Site.BaseURL, "/")
	}
	for i := range c.Site.Locales {
		c.Site.Locales[i].Prefix = strings.Trim(c.Site.Locales[i].Prefix, "/")
		if strings.TrimSpace(c.Site.Locales[i].Label) == "" {
			c.Site.Locales[i].Label = c.Site.Locales[i].Code
		}
	}
	if c.Content.Source == "" {
		c.Content.Source = "git"
	}
	if c.Content.Dir == "" {
		c.Content.Dir = "content"
	}
	if c.Content.MaxFileSize == 0 {
		c.Content.MaxFileSize = 1 << 20
	}
	if c.Content.ReloadInterval == "" {
		c.Content.ReloadInterval = "0s"
	}
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Cache.Driver == "" {
		c.Cache.Driver = "auto"
	}
	c.Cache.Driver = strings.ToLower(strings.TrimSpace(c.Cache.Driver))
	if c.Cache.TTL == "" {
		c.Cache.TTL = "10m"
	}
	if c.Cache.MaxEntries == 0 {
		c.Cache.MaxEntries = 2000
	}
	if c.Cache.MaxBytes == 0 {
		c.Cache.MaxBytes = 64 << 20
	}
	if strings.TrimSpace(c.Cache.Redis.Prefix) == "" {
		c.Cache.Redis.Prefix = "wikix"
	}
	if c.Database.MaxConns == 0 {
		c.Database.MaxConns = 10
	}
	if c.Auth.SessionTTL == "" {
		c.Auth.SessionTTL = "336h"
	}
	if c.Media.Dir == "" {
		c.Media.Dir = "data/media"
	}
	if c.Media.MaxSize == 0 {
		c.Media.MaxSize = 8 << 20
	}
	if c.Comment.RateLimit == 0 {
		c.Comment.RateLimit = 5
	}
	if c.Comment.RateWindow == "" {
		c.Comment.RateWindow = "1m"
	}
	for i := range c.Comment.BlockedWords {
		c.Comment.BlockedWords[i] = strings.ToLower(strings.TrimSpace(c.Comment.BlockedWords[i]))
	}
	if c.GitHub.BaseBranch == "" {
		c.GitHub.BaseBranch = "main"
	}
	c.GitHub.ContentPath = strings.Trim(c.GitHub.ContentPath, "/")
	if c.GitHub.PRImport == "" {
		c.GitHub.PRImport = "pending"
	}
	if c.GitHub.APIBaseURL == "" {
		c.GitHub.APIBaseURL = "https://api.github.com"
	}
}

func (c *Config) resolvePaths(baseDir string) {
	if !filepath.IsAbs(c.Content.Dir) {
		c.Content.Dir = filepath.Join(baseDir, c.Content.Dir)
	}
	if c.Theme.Dir != "" && !filepath.IsAbs(c.Theme.Dir) {
		c.Theme.Dir = filepath.Join(baseDir, c.Theme.Dir)
	}
	if c.Theme.Root != "" && !filepath.IsAbs(c.Theme.Root) {
		c.Theme.Root = filepath.Join(baseDir, c.Theme.Root)
	}
	if c.GitHub.PrivateKeyFile != "" && !filepath.IsAbs(c.GitHub.PrivateKeyFile) {
		c.GitHub.PrivateKeyFile = filepath.Join(baseDir, c.GitHub.PrivateKeyFile)
	}
	if !filepath.IsAbs(c.Media.Dir) {
		c.Media.Dir = filepath.Join(baseDir, c.Media.Dir)
	}
}

func (c *Config) check() error {
	switch c.Content.Source {
	case "git", "postgres":
	default:
		return fmt.Errorf("unknown content.source %q", c.Content.Source)
	}
	if c.Site.BaseURL != "" {
		u, err := url.Parse(c.Site.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("site.base_url must be a http(s) url")
		}
	}
	for i, item := range c.Site.Nav {
		if strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("site.nav[%d].title is empty", i)
		}
		if !strings.HasPrefix(item.Href, "/") || strings.HasPrefix(item.Href, "//") {
			return fmt.Errorf("site.nav[%d].href must be a site path", i)
		}
	}
	if len(c.Site.Locales) > 0 {
		codes := make(map[string]bool)
		prefixes := make(map[string]bool)
		defaultCode := ""
		for i, locale := range c.Site.Locales {
			if strings.TrimSpace(locale.Code) == "" {
				return fmt.Errorf("site.locales[%d].code is empty", i)
			}
			if codes[locale.Code] {
				return fmt.Errorf("duplicate locale code %q", locale.Code)
			}
			codes[locale.Code] = true
			if prefixes[locale.Prefix] {
				return fmt.Errorf("duplicate locale prefix %q", locale.Prefix)
			}
			prefixes[locale.Prefix] = true
			if locale.Prefix == "" {
				defaultCode = locale.Code
			}
			if strings.ContainsAny(locale.Prefix, "/\\?#%&") || strings.Contains(locale.Prefix, "..") || strings.HasPrefix(locale.Prefix, ".") {
				return fmt.Errorf("site.locales[%d].prefix is invalid", i)
			}
		}
		if defaultCode == "" {
			return fmt.Errorf("site.locales must include one locale with an empty prefix")
		}
		if defaultCode != c.Site.Language {
			return fmt.Errorf("site.language must match the locale with an empty prefix")
		}
	}
	if c.Content.MaxFileSize < 1 {
		return fmt.Errorf("content.max_file_size must be positive")
	}
	reload, err := time.ParseDuration(c.Content.ReloadInterval)
	if err != nil || reload < 0 {
		return fmt.Errorf("content.reload_interval must be a non-negative duration")
	}
	ttl, err := time.ParseDuration(c.Cache.TTL)
	if err != nil || ttl <= 0 {
		return fmt.Errorf("cache.ttl must be a positive duration")
	}
	switch c.Cache.Driver {
	case "auto", "memory", "redis":
	default:
		return fmt.Errorf("unknown cache.driver %q", c.Cache.Driver)
	}
	if c.Cache.Driver == "redis" && strings.TrimSpace(c.Cache.Redis.Addr) == "" {
		return fmt.Errorf("cache.redis.addr is required when cache.driver is redis")
	}
	if c.Cache.MaxEntries < 1 {
		return fmt.Errorf("cache.max_entries must be positive")
	}
	if c.Cache.MaxBytes < 1 {
		return fmt.Errorf("cache.max_bytes must be positive")
	}
	if c.Cache.Redis.DB < 0 {
		return fmt.Errorf("cache.redis.db must be zero or positive")
	}
	if c.Comment.RateLimit < 1 || c.Comment.RateLimit > 10000 {
		return fmt.Errorf("comment.rate_limit must be between 1 and 10000")
	}
	if window, err := time.ParseDuration(c.Comment.RateWindow); err != nil || window <= 0 {
		return fmt.Errorf("comment.rate_window must be a positive duration")
	}
	if len(c.Comment.BlockedWords) > 500 {
		return fmt.Errorf("comment.blocked_words cannot exceed 500 words")
	}
	for i, word := range c.Comment.BlockedWords {
		if word == "" {
			return fmt.Errorf("comment.blocked_words[%d] is empty", i)
		}
		if len([]rune(word)) > 100 {
			return fmt.Errorf("comment.blocked_words[%d] is too long", i)
		}
	}
	if c.Content.Source == "postgres" && strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("database.url is required when content.source is postgres")
	}
	if c.Database.MaxConns < 1 {
		return fmt.Errorf("database.max_conns must be positive")
	}
	sessionTTL, err := time.ParseDuration(c.Auth.SessionTTL)
	if err != nil || sessionTTL <= 0 {
		return fmt.Errorf("auth.session_ttl must be a positive duration")
	}
	if len(c.Auth.TrustedProxies) > 32 {
		return fmt.Errorf("auth.trusted_proxies cannot exceed 32 entries")
	}
	for i, item := range c.Auth.TrustedProxies {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(item)); err != nil {
			return fmt.Errorf("auth.trusted_proxies[%d] must be a cidr", i)
		}
	}
	if c.Auth.GitHub.ClientID != "" || c.Auth.GitHub.ClientSecret != "" {
		if c.Auth.GitHub.ClientID == "" || c.Auth.GitHub.ClientSecret == "" {
			return fmt.Errorf("auth.github client_id and client_secret are both required")
		}
		if c.Auth.GitHub.RedirectURL == "" && c.Site.BaseURL == "" {
			return fmt.Errorf("auth.github.redirect_url is required when base_url is empty")
		}
	}
	if c.GitHub.WebhookSecret != "" && len(c.GitHub.WebhookSecret) < 16 {
		return fmt.Errorf("github.webhook_secret must be at least 16 characters")
	}
	if c.GitHub.Token != "" && c.GitHub.Repo == "" {
		return fmt.Errorf("github.repo is required when github.token is set")
	}
	appConfigured := c.GitHub.AppID != 0 || c.GitHub.InstallationID != 0 || c.GitHub.PrivateKeyFile != "" || c.GitHub.PrivateKeyPEM != ""
	if appConfigured {
		if c.GitHub.Repo == "" {
			return fmt.Errorf("github.repo is required for github app authentication")
		}
		if c.GitHub.AppID <= 0 || c.GitHub.InstallationID <= 0 {
			return fmt.Errorf("github.app_id and github.installation_id are required")
		}
		if c.GitHub.PrivateKeyFile == "" && strings.TrimSpace(c.GitHub.PrivateKeyPEM) == "" {
			return fmt.Errorf("github.private_key_file or WIKIX_GITHUB_PRIVATE_KEY is required")
		}
	}
	if c.GitHub.Repo != "" && strings.Count(c.GitHub.Repo, "/") != 1 {
		return fmt.Errorf("github.repo must be owner/repo")
	}
	switch c.GitHub.PRImport {
	case "off", "pending", "publish":
	default:
		return fmt.Errorf("github.pr_import must be off, pending or publish")
	}
	if strings.Contains(c.GitHub.ContentPath, "..") || strings.Contains(c.GitHub.ContentPath, "\\") {
		return fmt.Errorf("github.content_path is invalid")
	}
	if c.GitHub.APIBaseURL != "" {
		u, err := url.Parse(c.GitHub.APIBaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("github.api_base_url must be a http(s) url")
		}
	}
	if c.Media.MaxSize < 1 {
		return fmt.Errorf("media.max_size must be positive")
	}
	return nil
}

func (c ContentConfig) ReloadEvery() time.Duration {
	interval, _ := time.ParseDuration(c.ReloadInterval)
	return interval
}

func (c CacheConfig) CacheTTL() time.Duration {
	ttl, _ := time.ParseDuration(c.TTL)
	return ttl
}

func (c SiteConfig) LocaleConfigs() []LocaleConfig {
	if len(c.Locales) > 0 {
		return append([]LocaleConfig(nil), c.Locales...)
	}
	code := c.Language
	if code == "" {
		code = "zh-CN"
	}
	return []LocaleConfig{{Code: code, Label: code, Prefix: ""}}
}

func (c AuthConfig) SessionDuration() time.Duration {
	ttl, _ := time.ParseDuration(c.SessionTTL)
	return ttl
}

func (c AuthConfig) GitHubEnabled() bool {
	return c.GitHub.ClientID != "" && c.GitHub.ClientSecret != ""
}

func (c CommentConfig) RateLimitOrDefault() int {
	if c.RateLimit <= 0 {
		return 5
	}
	return c.RateLimit
}

func (c CommentConfig) RateWindowOrDefault() time.Duration {
	window, err := time.ParseDuration(c.RateWindow)
	if err != nil || window <= 0 {
		return time.Minute
	}
	return window
}
