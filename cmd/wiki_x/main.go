package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/audit"
	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/build"
	"github.com/EggyFly/Wiki-X/internal/config"
	gitsource "github.com/EggyFly/Wiki-X/internal/content/gitsource"
	"github.com/EggyFly/Wiki-X/internal/search"
	"github.com/EggyFly/Wiki-X/internal/server"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

const version = "0.3.0"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "build":
		err = runBuild(ctx, os.Args[2:])
	case "serve":
		err = runServe(ctx, os.Args[2:])
	case "migrate":
		err = runMigrate(ctx, os.Args[2:])
	case "import":
		err = runImport(ctx, os.Args[2:])
	case "setup":
		err = runSetup(ctx, os.Args[2:])
	case "warm":
		err = runWarm(ctx, os.Args[2:])
	case "user":
		err = runUser(ctx, os.Args[2:])
	case "audit":
		err = runAudit(ctx, os.Args[2:])
	case "search":
		err = runSearch(ctx, os.Args[2:])
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}

	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		slog.Error("command failed", "err", err)
		os.Exit(1)
	}
}

func runBuild(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	outDir := flags.String("out", "dist", "output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	cfg.Cache.Driver = "memory"
	cfg.Theme.DisableSwitch = true
	cfg.Site.Static = true

	var data *store.Store
	var loaded *site.Site
	if cfg.Content.Source == "postgres" {
		data, err = openStore(ctx, cfg)
		if err != nil {
			return err
		}
		defer data.Close()
		if cfg.Database.AutoMigrate {
			if err := data.Migrate(ctx); err != nil {
				return err
			}
		}
		loaded, err = site.LoadWithSource(ctx, cfg, data)
	} else {
		if err := ensureGitContent(ctx, cfg, cfg.Content.Dir); err != nil {
			return err
		}
		loaded, err = site.Load(ctx, cfg)
	}
	if err != nil {
		return err
	}
	defer loaded.Close()

	if err := build.Run(ctx, loaded, *outDir); err != nil {
		return err
	}
	slog.Info("build done", "pages", len(loaded.Routes()), "out", *outDir)
	return nil
}

func runServe(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	addr := flags.String("addr", "", "listen address override")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	var data *store.Store
	var authManager *auth.Manager
	var loaded *site.Site
	var syncContent func(context.Context) error

	switch cfg.Content.Source {
	case "postgres":
		data, err = openStore(ctx, cfg)
		if err != nil {
			return err
		}
		defer data.Close()
		if cfg.Database.AutoMigrate {
			if err := data.Migrate(ctx); err != nil {
				return err
			}
		}
		loaded, err = site.LoadWithSource(ctx, cfg, data)
		if err != nil {
			return err
		}
		authManager = auth.New(cfg.Auth, data)
		go authManager.CleanupSessions(ctx, time.Hour)
	case "git":
		if cfg.Database.Configured() {
			data, err = openStore(ctx, cfg)
			if err != nil {
				return err
			}
			defer data.Close()
			if cfg.Database.AutoMigrate {
				if err := data.Migrate(ctx); err != nil {
					return err
				}
			}
			authManager = auth.New(cfg.Auth, data)
			go authManager.CleanupSessions(ctx, time.Hour)
		} else if cfg.GitHub.Token != "" || cfg.GitHub.AppID > 0 {
			slog.Warn("github PR editing is disabled without database configuration")
		}
		if err := ensureGitContent(ctx, cfg, cfg.Content.Dir); err != nil {
			return err
		}
		loaded, err = site.Load(ctx, cfg)
		if err != nil {
			return err
		}
		if cfg.Content.GitPull {
			syncContent = gitPullFunc(cfg.Content.Dir)
		}
	default:
		return fmt.Errorf("unknown content source %q", cfg.Content.Source)
	}
	defer loaded.Close()

	app, err := server.NewApp(cfg, loaded, data, authManager, syncContent)
	if err != nil {
		return err
	}
	slog.Info("site loaded",
		"pages", len(loaded.Routes()),
		"cache", loaded.CacheDriver(),
		"source", cfg.Content.Source,
		"auth", authManager != nil,
		"github_pr", cfg.GitHub.Repo != "" && (cfg.GitHub.Token != "" || cfg.GitHub.AppID > 0),
	)
	if every := cfg.Content.ReloadEvery(); every > 0 {
		slog.Info("auto reload enabled", "interval", every)
		go loaded.AutoReload(ctx, every)
	}
	if data != nil {
		go audit.RunWorker(ctx, data, cfg.Audit)
	}
	return server.RunApp(ctx, app, cfg.Server.Addr)
}

func runMigrate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		return err
	}
	slog.Info("database migrated")
	return nil
}

func runImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	contentDir := flags.String("content-dir", "", "git content directory override")
	authorName := flags.String("author", "", "author user name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		return err
	}
	return importContent(ctx, cfg, data, *contentDir, *authorName)
}

func importContent(ctx context.Context, cfg *config.Config, data *store.Store, contentDir, authorName string) error {
	dir := contentDir
	if dir == "" {
		dir = cfg.Content.Dir
	}
	if err := ensureGitContent(ctx, cfg, dir); err != nil {
		return err
	}
	source := gitsource.New(dir)
	source.SetMaxFileSize(cfg.Content.MaxFileSize)
	pages, err := source.Load(ctx)
	if err != nil {
		return err
	}
	for _, page := range pages {
		if page.Draft {
			continue
		}
		if err := site.ValidateRoute(page.Route); err != nil {
			return fmt.Errorf("page %s: %w", page.Key, err)
		}
	}
	var authorID *int64
	if authorName != "" {
		user, err := data.UserByName(ctx, authorName)
		if err != nil {
			return fmt.Errorf("author %s: %w", authorName, err)
		}
		authorID = &user.ID
	}
	created, updated, err := data.ImportPages(ctx, pages, authorID)
	if err != nil {
		return err
	}
	slog.Info("import done", "created", created, "updated", updated, "source", dir)
	return nil
}

func runSetup(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	importContentFlag := flags.Bool("import", false, "import Git content after migration")
	contentDir := flags.String("content-dir", "", "git content directory override")
	authorName := flags.String("author", "", "author user name for import")
	adminName := flags.String("admin", "", "create or promote an admin user")
	adminEmail := flags.String("email", "", "admin email")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if !cfg.Database.Configured() {
		return errors.New("database.primary or DATABASE_PRIMARY_ADDRESS is required")
	}
	if err := store.CheckReplica(ctx, cfg.Database); err != nil {
		return err
	}
	if err := store.EnsureDatabase(ctx, cfg.Database); err != nil {
		return err
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		return err
	}
	if name := strings.TrimSpace(*adminName); name != "" {
		if _, err := data.UserByName(ctx, name); err == nil {
			if err := data.GrantGroup(ctx, name, "admin"); err != nil {
				return err
			}
			slog.Info("admin already exists", "name", name)
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		} else {
			err := createUserWithData(ctx, data, name, *adminEmail, true)
			if errors.Is(err, store.ErrUserExists) {
				if err := data.GrantGroup(ctx, name, "admin"); err != nil {
					return err
				}
				slog.Info("admin already exists", "name", name)
			} else if err != nil {
				return err
			}
		}
	}
	if *importContentFlag {
		if err := importContent(ctx, cfg, data, *contentDir, *authorName); err != nil {
			return err
		}
	}
	slog.Info("setup done")
	return nil
}

func runWarm(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("warm", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	skin := flags.String("skin", "", "skin to warm, empty uses theme.default")
	concurrency := flags.Int("concurrency", 4, "parallel render workers")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	var data *store.Store
	var loaded *site.Site
	if cfg.Content.Source == "postgres" {
		data, err = openStore(ctx, cfg)
		if err != nil {
			return err
		}
		defer data.Close()
		if cfg.Database.AutoMigrate {
			if err := data.Migrate(ctx); err != nil {
				return err
			}
		}
		loaded, err = site.LoadWithSource(ctx, cfg, data)
	} else {
		if err := ensureGitContent(ctx, cfg, cfg.Content.Dir); err != nil {
			return err
		}
		loaded, err = site.Load(ctx, cfg)
	}
	if err != nil {
		return err
	}
	defer loaded.Close()
	target := strings.TrimSpace(*skin)
	if target == "" {
		target = strings.TrimSpace(cfg.Theme.Default)
	}
	if target == "" {
		target = "default"
	}
	started := time.Now()
	count, err := loaded.Warm(ctx, target, *concurrency)
	if err != nil {
		return err
	}
	slog.Info("warm done", "pages", count, "skin", target, "dur", time.Since(started).String())
	return nil
}

func runSearch(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "reindex" {
		return errors.New("usage: wiki_x search reindex [flags]")
	}
	flags := flag.NewFlagSet("search reindex", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.Search.Backend != "meilisearch" {
		return errors.New("search.backend is not meilisearch")
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	pages, err := data.Load(ctx)
	if err != nil {
		return err
	}
	client := search.NewClient(cfg.Search.URL, cfg.Search.APIKey, cfg.Search.Index)
	if err := client.Clear(ctx); err != nil {
		return err
	}
	for start := 0; start < len(pages); start += 50 {
		end := start + 50
		if end > len(pages) {
			end = len(pages)
		}
		documents := make([]search.Document, 0, end-start)
		for _, page := range pages[start:end] {
			documents = append(documents, search.Document{
				ID:          search.DocumentID(page.Route),
				Route:       page.Route,
				Title:       page.Title,
				Description: page.Description,
				Body:        page.Body,
			})
		}
		if err := client.Index(ctx, documents); err != nil {
			return err
		}
	}
	slog.Info("search reindex submitted", "pages", len(pages), "index", cfg.Search.Index)
	return nil
}

func runUser(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: wiki_x user <create|grant> [flags]")
	}
	switch args[0] {
	case "create":
		return runUserCreate(ctx, args[1:])
	case "grant":
		return runUserGrant(ctx, args[1:])
	default:
		return fmt.Errorf("unknown user command %q", args[0])
	}
}

func runUserCreate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("user create", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	name := flags.String("name", "", "user name")
	email := flags.String("email", "", "email")
	admin := flags.Bool("admin", false, "grant admin group")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("user name is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		return err
	}
	return createUserWithData(ctx, data, *name, *email, *admin)
}

func createUserWithData(ctx context.Context, data *store.Store, name, email string, admin bool) error {
	fmt.Fprintln(os.Stderr, "reading password from stdin")
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		return err
	}
	password := strings.TrimRight(string(raw), "\r\n")
	if count := utf8.RuneCountInString(password); count < 8 || count > 128 {
		return errors.New("password must be 8 to 128 characters")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	user, err := data.CreateUser(ctx, name, email, hash, admin)
	if err != nil {
		return err
	}
	slog.Info("user created", "name", user.Name, "admin", admin)
	return nil
}

func runUserGrant(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("user grant", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	name := flags.String("name", "", "user name")
	group := flags.String("group", "", "group: admin, editor, user")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch *group {
	case "admin", "editor", "user":
	default:
		return errors.New("group must be admin, editor or user")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		return err
	}
	if err := data.GrantGroup(ctx, *name, *group); err != nil {
		return err
	}
	slog.Info("group granted", "name", *name, "group", *group)
	return nil
}

func runAudit(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "archive" {
		return errors.New("usage: wiki_x audit archive [flags]")
	}
	flags := flag.NewFlagSet("audit archive", flag.ContinueOnError)
	configPath := flags.String("config", "wiki_x.yaml", "path to config file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.Audit.RetentionDuration() <= 0 {
		return errors.New("audit.retention is disabled")
	}
	data, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()
	cutoff := time.Now().Add(-cfg.Audit.RetentionDuration())
	result, err := audit.Archive(ctx, data, cfg.Audit.ArchiveDir, cutoff)
	if err != nil {
		return err
	}
	if result.Count == 0 {
		slog.Info("audit archive: nothing to archive")
		return nil
	}
	slog.Info("audit archive done", "file", result.File, "archived", result.Count, "deleted", result.Deleted)
	return nil
}

func openStore(ctx context.Context, cfg *config.Config) (*store.Store, error) {
	if !cfg.Database.Configured() {
		return nil, errors.New("database.primary or DATABASE_PRIMARY_ADDRESS is required")
	}
	return store.Open(ctx, cfg.Database)
}

func ensureGitContent(ctx context.Context, cfg *config.Config, dir string) error {
	if strings.TrimSpace(cfg.Content.GitURL) == "" {
		return nil
	}
	return gitsource.EnsureRepo(ctx, dir, cfg.Content.GitURL, cfg.Content.GitBranch, cfg.Content.GitDepth, cfg.Content.GitPull)
}

func gitPullFunc(dir string) func(context.Context) error {
	return func(ctx context.Context) error {
		command := exec.CommandContext(ctx, "git", "-C", dir, "pull", "--ff-only")
		if err := command.Run(); err != nil {
			return fmt.Errorf("git pull in %s failed: %w", dir, err)
		}
		slog.Info("git pull complete", "dir", dir)
		return nil
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wiki_x <build|serve|migrate|import|setup|warm|user|audit|search|version> [flags]")
}
