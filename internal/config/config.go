package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Site         SiteConfig         `yaml:"site"`
	Content      ContentConfig      `yaml:"content"`
	Theme        ThemeConfig        `yaml:"theme"`
	Server       ServerConfig       `yaml:"server"`
	Cache        CacheConfig        `yaml:"cache"`
	Database     DatabaseConfig     `yaml:"database"`
	Auth         AuthConfig         `yaml:"auth"`
	GitHub       GitHubConfig       `yaml:"github"`
	Media        MediaConfig        `yaml:"media"`
	Comment      CommentConfig      `yaml:"comment"`
	Mail         MailConfig         `yaml:"mail"`
	Registration RegistrationConfig `yaml:"registration"`
	Audit        AuditConfig        `yaml:"audit"`
	RateLimit    RateLimitConfig    `yaml:"ratelimit"`
	Search       SearchConfig       `yaml:"search"`
	Metrics      MetricsConfig      `yaml:"metrics"`
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
	GitURL         string `yaml:"git_url"`
	GitBranch      string `yaml:"git_branch"`
	GitDepth       int    `yaml:"git_depth"`
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
	URL                 string       `yaml:"url"`
	Same                *bool        `yaml:"same"`
	MaintenanceDatabase string       `yaml:"maintenance_database"`
	Primary             DatabaseNode `yaml:"primary"`
	Replica             DatabaseNode `yaml:"replica"`
	MaxConns            int32        `yaml:"max_conns"`
	AutoMigrate         bool         `yaml:"auto_migrate"`
}

type DatabaseNode struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	Params   string `yaml:"params"`
}

func (c DatabaseConfig) SameDatabase() bool {
	return c.Same == nil || *c.Same
}

func (c DatabaseConfig) Configured() bool {
	return c.URL != "" || c.Primary.Address != "" || c.Primary.Database != ""
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
	Dir     string    `yaml:"dir"`
	MaxSize int64     `yaml:"max_size"`
	Storage string    `yaml:"storage"`
	OSS     OSSConfig `yaml:"oss"`
}

type OSSConfig struct {
	Endpoint        string `yaml:"endpoint"`
	Bucket          string `yaml:"bucket"`
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	PublicBaseURL   string `yaml:"public_base_url"`
	PathPrefix      string `yaml:"path_prefix"`
	ACL             string `yaml:"acl"`
	SignedURLTTL    string `yaml:"signed_url_ttl"`
}

type CommentConfig struct {
	RateLimit    int      `yaml:"rate_limit"`
	RateWindow   string   `yaml:"rate_window"`
	BlockedWords []string `yaml:"blocked_words"`
}

type MailConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	From        string `yaml:"from"`
	ImplicitTLS bool   `yaml:"implicit_tls"`
}

type RegistrationConfig struct {
	Mode         string `yaml:"mode"`
	DefaultGroup string `yaml:"default_group"`
}

type AuditConfig struct {
	Retention  string `yaml:"retention"`
	ArchiveDir string `yaml:"archive_dir"`
	Interval   string `yaml:"interval"`
}

type RateLimitConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Requests      int    `yaml:"requests"`
	Window        string `yaml:"window"`
	RouteRequests int    `yaml:"route_requests"`
	RouteWindow   string `yaml:"route_window"`
}

type SearchConfig struct {
	Backend string `yaml:"backend"`
	URL     string `yaml:"url"`
	APIKey  string `yaml:"api_key"`
	Index   string `yaml:"index"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
	Token   string `yaml:"token"`
}

func Load(path string) (*Config, error) {
	envKeys, err := loadDotEnv(path)
	if err != nil {
		return nil, err
	}
	defer restoreEnv(envKeys)
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

	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	cfg.setDefault()
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
	envString("SITE_TITLE", &c.Site.Title)
	envString("SITE_DESCRIPTION", &c.Site.Description)
	envString("SITE_BASE_URL", &c.Site.BaseURL)
	envString("SITE_LANGUAGE", &c.Site.Language)
	if err := envBool("SITE_COMMUNITY_UI", &c.Site.CommunityUI); err != nil {
		return err
	}
	envString("CONTENT_SOURCE", &c.Content.Source)
	envString("CONTENT_DIR", &c.Content.Dir)
	envString("CONTENT_RELOAD_INTERVAL", &c.Content.ReloadInterval)
	if err := envInt64("CONTENT_MAX_FILE_SIZE", &c.Content.MaxFileSize); err != nil {
		return err
	}
	if err := envBool("CONTENT_GIT_PULL", &c.Content.GitPull); err != nil {
		return err
	}
	envString("CONTENT_GIT_URL", &c.Content.GitURL)
	envString("CONTENT_GIT_BRANCH", &c.Content.GitBranch)
	if err := envInt("CONTENT_GIT_DEPTH", &c.Content.GitDepth); err != nil {
		return err
	}
	envString("THEME_DIR", &c.Theme.Dir)
	envString("THEME_ROOT", &c.Theme.Root)
	envString("THEME_DEFAULT", &c.Theme.Default)
	if err := envBool("THEME_DISABLE_SWITCH", &c.Theme.DisableSwitch); err != nil {
		return err
	}
	envString("SERVER_ADDR", &c.Server.Addr)
	if err := envBool("SERVER_ACCESS_LOG", &c.Server.AccessLog); err != nil {
		return err
	}
	envString("CACHE_DRIVER", &c.Cache.Driver)
	envString("CACHE_TTL", &c.Cache.TTL)
	if err := envInt("CACHE_MAX_ENTRIES", &c.Cache.MaxEntries); err != nil {
		return err
	}
	if err := envInt64("CACHE_MAX_BYTES", &c.Cache.MaxBytes); err != nil {
		return err
	}
	envString("REDIS_PREFIX", &c.Cache.Redis.Prefix)
	envString("DATABASE_URL", &c.Database.URL)
	if value := strings.TrimSpace(os.Getenv("DATABASE_SAME")); value != "" {
		same, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("DATABASE_SAME: %w", err)
		}
		c.Database.Same = &same
	}
	envString("DATABASE_MAINTENANCE_DATABASE", &c.Database.MaintenanceDatabase)
	envString("DATABASE_PRIMARY_ADDRESS", &c.Database.Primary.Address)
	envString("DATABASE_PRIMARY_USERNAME", &c.Database.Primary.Username)
	envString("DATABASE_PRIMARY_PASSWORD", &c.Database.Primary.Password)
	envString("DATABASE_PRIMARY_DATABASE", &c.Database.Primary.Database)
	envString("DATABASE_PRIMARY_PARAMS", &c.Database.Primary.Params)
	envString("DATABASE_REPLICA_ADDRESS", &c.Database.Replica.Address)
	envString("DATABASE_REPLICA_USERNAME", &c.Database.Replica.Username)
	envString("DATABASE_REPLICA_PASSWORD", &c.Database.Replica.Password)
	envString("DATABASE_REPLICA_DATABASE", &c.Database.Replica.Database)
	envString("DATABASE_REPLICA_PARAMS", &c.Database.Replica.Params)
	if err := envInt32("DATABASE_MAX_CONNS", &c.Database.MaxConns); err != nil {
		return err
	}
	if err := envBool("DATABASE_AUTO_MIGRATE", &c.Database.AutoMigrate); err != nil {
		return err
	}
	envString("MEDIA_DIR", &c.Media.Dir)
	if err := envInt64("MEDIA_MAX_SIZE", &c.Media.MaxSize); err != nil {
		return err
	}
	envString("MEDIA_STORAGE", &c.Media.Storage)
	envString("OSS_ENDPOINT", &c.Media.OSS.Endpoint)
	envString("OSS_BUCKET", &c.Media.OSS.Bucket)
	envString("OSS_ACCESS_KEY_ID", &c.Media.OSS.AccessKeyID)
	envString("OSS_ACCESS_KEY_SECRET", &c.Media.OSS.AccessKeySecret)
	envString("OSS_PUBLIC_BASE_URL", &c.Media.OSS.PublicBaseURL)
	envString("OSS_PATH_PREFIX", &c.Media.OSS.PathPrefix)
	envString("OSS_ACL", &c.Media.OSS.ACL)
	envString("OSS_SIGNED_URL_TTL", &c.Media.OSS.SignedURLTTL)
	if err := envBool("AUTH_SECURE_COOKIES", &c.Auth.SecureCookies); err != nil {
		return err
	}
	envString("AUTH_SESSION_TTL", &c.Auth.SessionTTL)
	if value, ok := os.LookupEnv("AUTH_TRUSTED_PROXIES"); ok {
		c.Auth.TrustedProxies = splitEnvList(value)
	}
	if err := envInt("COMMENT_RATE_LIMIT", &c.Comment.RateLimit); err != nil {
		return err
	}
	envString("COMMENT_RATE_WINDOW", &c.Comment.RateWindow)
	if value := strings.TrimSpace(os.Getenv("REDIS_ADDR")); value != "" {
		c.Cache.Redis.Addr = value
	}
	if value, ok := os.LookupEnv("REDIS_PASSWORD"); ok {
		c.Cache.Redis.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("REDIS_DB")); value != "" {
		db, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("REDIS_DB: %w", err)
		}
		c.Cache.Redis.DB = db
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID")); value != "" {
		c.Auth.GitHub.ClientID = value
	}
	if value, ok := os.LookupEnv("GITHUB_CLIENT_SECRET"); ok {
		c.Auth.GitHub.ClientSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_REDIRECT_URL")); value != "" {
		c.Auth.GitHub.RedirectURL = value
	}
	if value, ok := os.LookupEnv("GITHUB_WEBHOOK_SECRET"); ok {
		c.GitHub.WebhookSecret = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_REPO")); value != "" {
		c.GitHub.Repo = value
	}
	if value, ok := os.LookupEnv("GITHUB_TOKEN"); ok {
		c.GitHub.Token = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_APP_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("GITHUB_APP_ID: %w", err)
		}
		c.GitHub.AppID = id
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_INSTALLATION_ID")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("GITHUB_INSTALLATION_ID: %w", err)
		}
		c.GitHub.InstallationID = id
	}
	if value, ok := os.LookupEnv("GITHUB_PRIVATE_KEY"); ok {
		c.GitHub.PrivateKeyPEM = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_PRIVATE_KEY_FILE")); value != "" {
		c.GitHub.PrivateKeyFile = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_BASE_BRANCH")); value != "" {
		c.GitHub.BaseBranch = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_CONTENT_PATH")); value != "" {
		c.GitHub.ContentPath = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_PR_IMPORT")); value != "" {
		c.GitHub.PRImport = value
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_API_BASE_URL")); value != "" {
		c.GitHub.APIBaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("MAIL_HOST")); value != "" {
		c.Mail.Host = value
	}
	if value := strings.TrimSpace(os.Getenv("MAIL_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("MAIL_PORT: %w", err)
		}
		c.Mail.Port = port
	}
	if value := strings.TrimSpace(os.Getenv("MAIL_USERNAME")); value != "" {
		c.Mail.Username = value
	}
	if value, ok := os.LookupEnv("MAIL_PASSWORD"); ok {
		c.Mail.Password = value
	}
	if value := strings.TrimSpace(os.Getenv("MAIL_FROM")); value != "" {
		c.Mail.From = value
	}
	if value := strings.TrimSpace(os.Getenv("MAIL_IMPLICIT_TLS")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("MAIL_IMPLICIT_TLS: %w", err)
		}
		c.Mail.ImplicitTLS = enabled
	}
	if value := strings.TrimSpace(os.Getenv("REGISTRATION_MODE")); value != "" {
		c.Registration.Mode = value
	}
	if value := strings.TrimSpace(os.Getenv("REGISTRATION_DEFAULT_GROUP")); value != "" {
		c.Registration.DefaultGroup = value
	}
	if value := strings.TrimSpace(os.Getenv("AUDIT_RETENTION")); value != "" {
		c.Audit.Retention = value
	}
	if value := strings.TrimSpace(os.Getenv("AUDIT_ARCHIVE_DIR")); value != "" {
		c.Audit.ArchiveDir = value
	}
	if value := strings.TrimSpace(os.Getenv("AUDIT_INTERVAL")); value != "" {
		c.Audit.Interval = value
	}
	if value := strings.TrimSpace(os.Getenv("RATE_LIMIT_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("RATE_LIMIT_ENABLED: %w", err)
		}
		c.RateLimit.Enabled = enabled
	}
	if value := strings.TrimSpace(os.Getenv("RATE_LIMIT_REQUESTS")); value != "" {
		requests, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("RATE_LIMIT_REQUESTS: %w", err)
		}
		c.RateLimit.Requests = requests
	}
	if value := strings.TrimSpace(os.Getenv("RATE_LIMIT_WINDOW")); value != "" {
		c.RateLimit.Window = value
	}
	if value := strings.TrimSpace(os.Getenv("RATE_LIMIT_ROUTE_REQUESTS")); value != "" {
		requests, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("RATE_LIMIT_ROUTE_REQUESTS: %w", err)
		}
		c.RateLimit.RouteRequests = requests
	}
	if value := strings.TrimSpace(os.Getenv("RATE_LIMIT_ROUTE_WINDOW")); value != "" {
		c.RateLimit.RouteWindow = value
	}
	if value := strings.TrimSpace(os.Getenv("SEARCH_BACKEND")); value != "" {
		c.Search.Backend = value
	}
	if value := strings.TrimSpace(os.Getenv("SEARCH_URL")); value != "" {
		c.Search.URL = value
	}
	if value, ok := os.LookupEnv("SEARCH_API_KEY"); ok {
		c.Search.APIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("SEARCH_INDEX")); value != "" {
		c.Search.Index = value
	}
	if value := strings.TrimSpace(os.Getenv("METRICS_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("METRICS_ENABLED: %w", err)
		}
		c.Metrics.Enabled = enabled
	}
	if value := strings.TrimSpace(os.Getenv("METRICS_PATH")); value != "" {
		c.Metrics.Path = value
	}
	if value, ok := os.LookupEnv("METRICS_TOKEN"); ok {
		c.Metrics.Token = value
	}
	return nil
}

func envString(key string, target *string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = value
	}
}

func envBool(key string, target *bool) error {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = parsed
	return nil
}

func envInt(key string, target *int) error {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = parsed
	return nil
}

func envInt32(key string, target *int32) error {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = int32(parsed)
	return nil
}

func envInt64(key string, target *int64) error {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*target = parsed
	return nil
}

func splitEnvList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
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
	c.Content.GitURL = strings.TrimSpace(c.Content.GitURL)
	c.Content.GitBranch = strings.TrimSpace(c.Content.GitBranch)
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
		c.Cache.Redis.Prefix = "wiki_x"
	}
	if c.Database.MaxConns == 0 {
		c.Database.MaxConns = 10
	}
	c.Database.URL = strings.TrimSpace(c.Database.URL)
	c.Database.MaintenanceDatabase = strings.TrimSpace(c.Database.MaintenanceDatabase)
	if c.Database.MaintenanceDatabase == "" {
		c.Database.MaintenanceDatabase = "postgres"
	}
	c.Database.Primary.Address = strings.TrimSpace(c.Database.Primary.Address)
	c.Database.Primary.Username = strings.TrimSpace(c.Database.Primary.Username)
	c.Database.Primary.Password = strings.TrimSpace(c.Database.Primary.Password)
	c.Database.Primary.Database = strings.TrimSpace(c.Database.Primary.Database)
	c.Database.Primary.Params = strings.TrimSpace(c.Database.Primary.Params)
	c.Database.Replica.Address = strings.TrimSpace(c.Database.Replica.Address)
	c.Database.Replica.Username = strings.TrimSpace(c.Database.Replica.Username)
	c.Database.Replica.Password = strings.TrimSpace(c.Database.Replica.Password)
	c.Database.Replica.Database = strings.TrimSpace(c.Database.Replica.Database)
	c.Database.Replica.Params = strings.TrimSpace(c.Database.Replica.Params)
	if c.Auth.SessionTTL == "" {
		c.Auth.SessionTTL = "336h"
	}
	if c.Media.Dir == "" {
		c.Media.Dir = "data/media"
	}
	if c.Media.MaxSize == 0 {
		c.Media.MaxSize = 8 << 20
	}
	if c.Media.Storage == "" {
		c.Media.Storage = "local"
	}
	if c.Media.OSS.PathPrefix == "" {
		c.Media.OSS.PathPrefix = "media"
	}
	if c.Media.OSS.ACL == "" {
		c.Media.OSS.ACL = "private"
	}
	if c.Media.OSS.SignedURLTTL == "" {
		c.Media.OSS.SignedURLTTL = "10m"
	}
	c.Media.Storage = strings.ToLower(strings.TrimSpace(c.Media.Storage))
	c.Media.OSS.Endpoint = strings.TrimSpace(c.Media.OSS.Endpoint)
	c.Media.OSS.Bucket = strings.TrimSpace(c.Media.OSS.Bucket)
	c.Media.OSS.AccessKeyID = strings.TrimSpace(c.Media.OSS.AccessKeyID)
	c.Media.OSS.AccessKeySecret = strings.TrimSpace(c.Media.OSS.AccessKeySecret)
	c.Media.OSS.PublicBaseURL = strings.TrimRight(strings.TrimSpace(c.Media.OSS.PublicBaseURL), "/")
	c.Media.OSS.PathPrefix = strings.Trim(strings.TrimSpace(c.Media.OSS.PathPrefix), "/")
	c.Media.OSS.ACL = strings.ToLower(strings.TrimSpace(c.Media.OSS.ACL))
	c.Media.OSS.SignedURLTTL = strings.TrimSpace(c.Media.OSS.SignedURLTTL)
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
	if c.Mail.Host != "" && c.Mail.Port == 0 {
		c.Mail.Port = 587
	}
	c.Mail.Host = strings.TrimSpace(c.Mail.Host)
	c.Mail.From = strings.TrimSpace(c.Mail.From)
	c.Registration.Mode = strings.ToLower(strings.TrimSpace(c.Registration.Mode))
	if c.Registration.Mode == "" {
		c.Registration.Mode = "off"
	}
	if strings.TrimSpace(c.Registration.DefaultGroup) == "" {
		c.Registration.DefaultGroup = "user"
	}
	if c.Audit.Retention == "" {
		c.Audit.Retention = "0s"
	}
	if c.Audit.ArchiveDir == "" {
		c.Audit.ArchiveDir = "data/audit"
	}
	if c.Audit.Interval == "" {
		c.Audit.Interval = "24h"
	}
	if c.RateLimit.Requests == 0 {
		c.RateLimit.Requests = 300
	}
	if c.RateLimit.Window == "" {
		c.RateLimit.Window = "1m"
	}
	if c.RateLimit.RouteWindow == "" {
		c.RateLimit.RouteWindow = "1m"
	}
	c.Search.Backend = strings.ToLower(strings.TrimSpace(c.Search.Backend))
	if c.Search.Backend == "" {
		c.Search.Backend = "auto"
	}
	c.Search.URL = strings.TrimRight(strings.TrimSpace(c.Search.URL), "/")
	if strings.TrimSpace(c.Search.Index) == "" {
		c.Search.Index = "wiki_x"
	}
	c.Search.Index = strings.TrimSpace(c.Search.Index)
	c.Metrics.Path = strings.TrimSpace(c.Metrics.Path)
	if c.Metrics.Path == "" {
		c.Metrics.Path = "/-/metrics"
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
	if !filepath.IsAbs(c.Audit.ArchiveDir) {
		c.Audit.ArchiveDir = filepath.Join(baseDir, c.Audit.ArchiveDir)
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
	if c.Content.GitURL != "" {
		if !validGitURL(c.Content.GitURL) {
			return fmt.Errorf("content.git_url must be a http(s), ssh or git url")
		}
	}
	if !validGitBranch(c.Content.GitBranch) {
		return fmt.Errorf("content.git_branch is invalid")
	}
	if c.Content.GitDepth < 0 || c.Content.GitDepth > 1000 {
		return fmt.Errorf("content.git_depth must be between 0 and 1000")
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
	primarySet := c.Database.Primary.Address != "" || c.Database.Primary.Database != ""
	replicaSet := c.Database.Replica.Address != "" || c.Database.Replica.Database != ""
	if primarySet && (c.Database.Primary.Address == "" || c.Database.Primary.Database == "") {
		return fmt.Errorf("database.primary.address and database.primary.database are required")
	}
	if c.Content.Source == "postgres" && c.Database.URL == "" && !primarySet {
		return fmt.Errorf("database.primary or database.url is required when content.source is postgres")
	}
	if !c.Database.SameDatabase() {
		if !primarySet {
			return fmt.Errorf("database.primary is required when database.same is false")
		}
		if c.Database.Replica.Address == "" || c.Database.Replica.Database == "" {
			return fmt.Errorf("database.replica.address and database.replica.database are required when database.same is false")
		}
	}
	if replicaSet && !c.Database.SameDatabase() && (c.Database.Replica.Address == "" || c.Database.Replica.Database == "") {
		return fmt.Errorf("database.replica.address and database.replica.database are required")
	}
	if primarySet {
		if err := validDatabaseParams(c.Database.Primary.Params); err != nil {
			return fmt.Errorf("database.primary.params: %w", err)
		}
	}
	if replicaSet {
		if err := validDatabaseParams(c.Database.Replica.Params); err != nil {
			return fmt.Errorf("database.replica.params: %w", err)
		}
	}
	if strings.ContainsAny(c.Database.MaintenanceDatabase, "/\\ \t\r\n") || c.Database.MaintenanceDatabase == "" {
		return fmt.Errorf("database.maintenance_database is invalid")
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
			return fmt.Errorf("github.private_key_file or GITHUB_PRIVATE_KEY is required")
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
	switch c.Media.Storage {
	case "local":
	case "oss":
		if c.Media.OSS.Endpoint == "" || c.Media.OSS.Bucket == "" || c.Media.OSS.AccessKeyID == "" || c.Media.OSS.AccessKeySecret == "" {
			return fmt.Errorf("media.oss endpoint, bucket, access_key_id and access_key_secret are required")
		}
		endpoint, err := url.Parse(c.Media.OSS.Endpoint)
		if err != nil || endpoint.Host == "" || endpoint.Scheme != "https" {
			return fmt.Errorf("media.oss.endpoint must be a https url")
		}
		if !validOSSBucket(c.Media.OSS.Bucket) {
			return fmt.Errorf("media.oss.bucket is invalid")
		}
		switch c.Media.OSS.ACL {
		case "private", "public-read", "public-read-write":
		default:
			return fmt.Errorf("media.oss.acl must be private, public-read or public-read-write")
		}
		if c.Media.OSS.PublicBaseURL != "" {
			publicURL, err := url.Parse(c.Media.OSS.PublicBaseURL)
			if err != nil || publicURL.Host == "" || (publicURL.Scheme != "http" && publicURL.Scheme != "https") {
				return fmt.Errorf("media.oss.public_base_url must be a http(s) url")
			}
		}
		if strings.Contains(c.Media.OSS.PathPrefix, "..") || strings.Contains(c.Media.OSS.PathPrefix, "\\") {
			return fmt.Errorf("media.oss.path_prefix is invalid")
		}
		ttl, err := time.ParseDuration(c.Media.OSS.SignedURLTTL)
		if err != nil || ttl <= 0 || ttl > 7*24*time.Hour {
			return fmt.Errorf("media.oss.signed_url_ttl must be between 0 and 168h")
		}
	default:
		return fmt.Errorf("media.storage must be local or oss")
	}
	if (c.Mail.Host == "") != (c.Mail.From == "") {
		return fmt.Errorf("mail.host and mail.from must be set together")
	}
	if c.Mail.Host != "" {
		if c.Mail.Port < 1 || c.Mail.Port > 65535 {
			return fmt.Errorf("mail.port must be between 1 and 65535")
		}
		if _, err := mail.ParseAddress(c.Mail.From); err != nil {
			return fmt.Errorf("mail.from must be a valid address")
		}
		if c.Site.BaseURL == "" {
			return fmt.Errorf("site.base_url is required when mail is configured")
		}
	}
	switch c.Registration.Mode {
	case "off", "invite", "open":
	default:
		return fmt.Errorf("registration.mode must be off, invite or open")
	}
	if c.Registration.Mode == "open" && !c.Mail.Enabled() {
		return fmt.Errorf("registration.mode open requires mail configuration")
	}
	if strings.ContainsAny(c.Registration.DefaultGroup, "/\\ \t\r\n") || c.Registration.DefaultGroup == "" {
		return fmt.Errorf("registration.default_group is invalid")
	}
	retention, err := time.ParseDuration(c.Audit.Retention)
	if err != nil || retention < 0 {
		return fmt.Errorf("audit.retention must be a non-negative duration")
	}
	if retention > 0 && retention < 180*24*time.Hour {
		return fmt.Errorf("audit.retention must be at least 180 days when enabled")
	}
	interval, err := time.ParseDuration(c.Audit.Interval)
	if err != nil || interval <= 0 {
		return fmt.Errorf("audit.interval must be a positive duration")
	}
	if strings.TrimSpace(c.Audit.ArchiveDir) == "" {
		return fmt.Errorf("audit.archive_dir is required")
	}
	if c.RateLimit.Requests < 1 {
		return fmt.Errorf("ratelimit.requests must be positive")
	}
	rateWindow, err := time.ParseDuration(c.RateLimit.Window)
	if err != nil || rateWindow <= 0 {
		return fmt.Errorf("ratelimit.window must be a positive duration")
	}
	if c.RateLimit.RouteRequests < 0 {
		return fmt.Errorf("ratelimit.route_requests cannot be negative")
	}
	routeWindow, err := time.ParseDuration(c.RateLimit.RouteWindow)
	if err != nil || routeWindow <= 0 {
		return fmt.Errorf("ratelimit.route_window must be a positive duration")
	}
	switch c.Search.Backend {
	case "auto", "memory", "postgres", "meilisearch":
	default:
		return fmt.Errorf("search.backend must be auto, memory, postgres or meilisearch")
	}
	if (c.Search.Backend == "postgres" || c.Search.Backend == "meilisearch") && c.Content.Source != "postgres" {
		return fmt.Errorf("search.backend %s requires content.source postgres", c.Search.Backend)
	}
	if c.Search.Backend == "meilisearch" {
		if !strings.HasPrefix(c.Search.URL, "http://") && !strings.HasPrefix(c.Search.URL, "https://") {
			return fmt.Errorf("search.url must be a http(s) url")
		}
		if !validSearchIndex(c.Search.Index) {
			return fmt.Errorf("search.index is invalid")
		}
	}
	if c.Metrics.Enabled {
		if !strings.HasPrefix(c.Metrics.Path, "/") || strings.Contains(c.Metrics.Path, "..") {
			return fmt.Errorf("metrics.path must be a site path")
		}
		switch c.Metrics.Path {
		case "/", "/assets/", "/-/assets/", "/-/healthz", "/media/":
			return fmt.Errorf("metrics.path is reserved")
		}
		for _, r := range c.Metrics.Path {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '-' || r == '_' {
				continue
			}
			return fmt.Errorf("metrics.path contains an invalid character")
		}
	}
	return nil
}

func (c AuditConfig) RetentionDuration() time.Duration {
	value, _ := time.ParseDuration(c.Retention)
	return value
}

func validDatabaseParams(value string) error {
	value = strings.TrimPrefix(strings.TrimSpace(value), "?")
	if value == "" {
		return nil
	}
	_, err := url.ParseQuery(value)
	return err
}

func validOSSBucket(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || (r == '-' && i > 0 && i < len(value)-1) {
			continue
		}
		return false
	}
	return true
}

func validGitURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	if strings.ContainsAny(value, "\r\n\t ") {
		return false
	}
	if strings.HasPrefix(value, "git@") {
		index := strings.IndexByte(value, ':')
		if index <= 4 || index >= len(value)-1 {
			return false
		}
		host := value[4:index]
		return host != "" && !strings.HasPrefix(host, "-")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || strings.HasPrefix(parsed.Hostname(), "-") {
		return false
	}
	if parsed.User != nil && strings.HasPrefix(parsed.User.Username(), "-") {
		return false
	}
	switch parsed.Scheme {
	case "http", "https", "ssh", "git":
		return true
	default:
		return false
	}
}

func validGitBranch(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if len(value) > 200 || strings.HasPrefix(value, "-") {
		return false
	}
	if strings.Contains(value, "..") || strings.ContainsAny(value, " ~^:?*[\\\r\n\t") {
		return false
	}
	return true
}

func validSearchIndex(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (c RateLimitConfig) WindowDuration() time.Duration {
	value, _ := time.ParseDuration(c.Window)
	if value <= 0 {
		return time.Minute
	}
	return value
}

func (c RateLimitConfig) RouteWindowDuration() time.Duration {
	value, _ := time.ParseDuration(c.RouteWindow)
	if value <= 0 {
		return time.Minute
	}
	return value
}

func (c AuditConfig) IntervalDuration() time.Duration {
	value, _ := time.ParseDuration(c.Interval)
	if value <= 0 {
		return 24 * time.Hour
	}
	return value
}

func (c MailConfig) Enabled() bool {
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != ""
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
