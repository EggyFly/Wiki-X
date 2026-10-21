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

	"wikix/internal/auth"
	"wikix/internal/build"
	"wikix/internal/config"
	gitsource "wikix/internal/content/gitsource"
	"wikix/internal/server"
	"wikix/internal/site"
	"wikix/internal/store"
)

const version = "0.2.0-dev"

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
	case "user":
		err = runUser(ctx, os.Args[2:])
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
	configPath := flags.String("config", "wikix.yaml", "path to config file")
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
	configPath := flags.String("config", "wikix.yaml", "path to config file")
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
		if strings.TrimSpace(cfg.Database.URL) != "" {
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
			slog.Warn("github PR editing is disabled without database.url")
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
	return server.RunApp(ctx, app, cfg.Server.Addr)
}

func runMigrate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := flags.String("config", "wikix.yaml", "path to config file")
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
	configPath := flags.String("config", "wikix.yaml", "path to config file")
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

	dir := *contentDir
	if dir == "" {
		dir = cfg.Content.Dir
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
	if *authorName != "" {
		user, err := data.UserByName(ctx, *authorName)
		if err != nil {
			return fmt.Errorf("author %s: %w", *authorName, err)
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

func runUser(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: wikix user <create|grant> [flags]")
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
	configPath := flags.String("config", "wikix.yaml", "path to config file")
	name := flags.String("name", "", "user name")
	email := flags.String("email", "", "email")
	admin := flags.Bool("admin", false, "grant admin group")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("user name is required")
	}
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
	user, err := data.CreateUser(ctx, *name, *email, hash, *admin)
	if err != nil {
		return err
	}
	slog.Info("user created", "name", user.Name, "admin", *admin)
	return nil
}

func runUserGrant(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("user grant", flag.ContinueOnError)
	configPath := flags.String("config", "wikix.yaml", "path to config file")
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

func openStore(ctx context.Context, cfg *config.Config) (*store.Store, error) {
	if strings.TrimSpace(cfg.Database.URL) == "" {
		return nil, errors.New("database.url is required, set it in config or WIKIX_DATABASE_URL")
	}
	return store.Open(ctx, cfg.Database)
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
	fmt.Fprintln(os.Stderr, "usage: wikix <build|serve|migrate|import|user|version> [flags]")
}
