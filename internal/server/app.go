package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/cache"
	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/ghclient"
	"github.com/EggyFly/Wiki-X/internal/i18n"
	"github.com/EggyFly/Wiki-X/internal/mail"
	"github.com/EggyFly/Wiki-X/internal/render"
	"github.com/EggyFly/Wiki-X/internal/search"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type App struct {
	cfg           *config.Config
	site          *site.Site
	store         *store.Store
	auth          *auth.Manager
	github        *ghclient.Client
	ui            *appUI
	reloadCh      chan struct{}
	syncContent   func(context.Context) error
	blockedWords  []string
	limitFallback *cache.Memory
	markdown      *render.Markdown
	mailer        mail.Sender
	metrics       *metricsRegistry
	searchClient  *search.Client
}

func NewApp(cfg *config.Config, s *site.Site, data *store.Store, authManager *auth.Manager, syncContent func(context.Context) error) (*App, error) {
	ui, err := loadUI()
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Content.Source == "postgres" {
		if err := os.MkdirAll(cfg.Media.Dir, 0o755); err != nil {
			return nil, fmt.Errorf("create media dir: %w", err)
		}
	}
	app := &App{
		cfg:           cfg,
		site:          s,
		store:         data,
		auth:          authManager,
		ui:            ui,
		reloadCh:      make(chan struct{}, 1),
		syncContent:   syncContent,
		limitFallback: cache.NewMemory(1, 1, time.Minute),
		markdown:      render.NewMarkdown(),
		metrics:       newMetrics(),
	}
	if cfg != nil && cfg.Search.Backend == "meilisearch" {
		app.searchClient = search.NewClient(cfg.Search.URL, cfg.Search.APIKey, cfg.Search.Index)
	}
	if s != nil && s.Markdown() != nil {
		app.markdown = s.Markdown()
	}
	if cfg != nil {
		app.blockedWords = normalizeBlockedWords(cfg.Comment.BlockedWords)
		if cfg.Mail.Enabled() {
			app.mailer = mail.NewSMTP(mail.Config{
				Host:        cfg.Mail.Host,
				Port:        cfg.Mail.Port,
				Username:    cfg.Mail.Username,
				Password:    cfg.Mail.Password,
				From:        cfg.Mail.From,
				ImplicitTLS: cfg.Mail.ImplicitTLS,
			})
		}
	}
	if cfg != nil && cfg.GitHub.Repo != "" && (cfg.GitHub.Token != "" || cfg.GitHub.AppID > 0 || cfg.GitHub.InstallationID > 0) {
		privateKey := []byte(cfg.GitHub.PrivateKeyPEM)
		if len(privateKey) == 0 && cfg.GitHub.PrivateKeyFile != "" {
			data, err := os.ReadFile(cfg.GitHub.PrivateKeyFile)
			if err != nil {
				return nil, fmt.Errorf("read github private key: %w", err)
			}
			privateKey = data
		}
		client, err := ghclient.New(ghclient.Config{
			Repo:           cfg.GitHub.Repo,
			Token:          cfg.GitHub.Token,
			AppID:          cfg.GitHub.AppID,
			InstallationID: cfg.GitHub.InstallationID,
			PrivateKey:     privateKey,
			BaseURL:        cfg.GitHub.APIBaseURL,
			BaseBranch:     cfg.GitHub.BaseBranch,
		})
		if err != nil {
			return nil, err
		}
		app.github = client
	}
	return app, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/", a.handleThemeAsset)
	if a.site.Switchable() {
		mux.HandleFunc("GET /-/skin", a.handleSkin)
	}
	if a.site.Media() != nil || (a.cfg != nil && a.cfg.Content.Source == "postgres") {
		mux.HandleFunc("GET /media/", a.handleMedia)
	}

	if a.ui != nil {
		appAssets := onlyFilesFS{FS: a.ui.assets}
		mux.Handle("GET /-/assets/", cacheAssets(http.StripPrefix("/-/assets/", http.FileServerFS(appAssets))))
	}
	mux.HandleFunc("GET /-/healthz", healthz)
	if a.cfg != nil && a.cfg.Metrics.Enabled {
		if metricsPathFree(mux, a.cfg.Metrics.Path) {
			mux.HandleFunc("GET "+a.cfg.Metrics.Path, a.handleMetrics)
		} else {
			slog.Error("metrics path conflicts with an existing route", "path", a.cfg.Metrics.Path)
		}
	}
	if a.cfg != nil && a.cfg.GitHub.WebhookSecret != "" {
		mux.HandleFunc("POST /-/webhooks/github", a.handleGitHubWebhook)
	}

	if a.ui != nil {
		mux.HandleFunc("GET /search", a.handleSearch)
	}
	if a.auth != nil && a.store != nil && a.ui != nil {
		mux.HandleFunc("GET /login", a.handleLoginPage)
		mux.HandleFunc("POST /login", a.handleLogin)
		mux.HandleFunc("POST /logout", a.handleLogout)
		mux.HandleFunc("GET /verify", a.handleVerifyPage)
		mux.HandleFunc("POST /verify", a.handleVerify)
		mux.HandleFunc("GET /account", a.handleAccount)
		mux.HandleFunc("POST /account/password", a.handleAccountPassword)
		mux.HandleFunc("POST /account/delete", a.handleAccountDelete)
		mux.HandleFunc("GET /account/export", a.handleAccountExport)
		mux.HandleFunc("GET /account/totp/qr", a.handleTOTPQR)
		mux.HandleFunc("POST /account/recovery/generate", a.handleRecoveryGenerate)
		if a.registrationMode() != "off" {
			mux.HandleFunc("GET /register", a.handleRegisterPage)
			mux.HandleFunc("POST /register", a.handleRegister)
			mux.HandleFunc("GET /register/verify", a.handleRegisterVerifyPage)
			mux.HandleFunc("POST /register/verify", a.handleRegisterVerify)
		}
		if a.mailEnabled() {
			mux.HandleFunc("GET /forgot", a.handleForgotPage)
			mux.HandleFunc("POST /forgot", a.handleForgot)
			mux.HandleFunc("GET /reset", a.handleResetPage)
			mux.HandleFunc("POST /reset", a.handleReset)
			mux.HandleFunc("GET /verify-email", a.handleVerifyEmailPage)
			mux.HandleFunc("POST /verify-email", a.handleVerifyEmail)
			mux.HandleFunc("POST /account/email", a.handleAccountEmail)
			mux.HandleFunc("POST /account/email/resend", a.handleAccountEmailResend)
		}
		mux.HandleFunc("POST /account/totp/setup", a.handleTOTPSetup)
		mux.HandleFunc("POST /account/totp/enable", a.handleTOTPEnable)
		mux.HandleFunc("POST /account/totp/disable", a.handleTOTPDisable)
		if a.auth.GitHubEnabled() {
			mux.HandleFunc("GET /auth/github", a.handleGitHubLogin)
			mux.HandleFunc("GET /auth/github/callback", a.handleGitHubCallback)
			mux.HandleFunc("GET /account/link/github", a.handleGitHubLink)
		}
	}
	if a.auth != nil && a.store != nil && a.ui != nil && a.cfg != nil {
		mux.HandleFunc("GET /watchlist", a.handleWatchlist)
		mux.HandleFunc("POST /watch", a.handleWatch)
		mux.HandleFunc("GET /discussion", a.handleDiscussion)
		mux.HandleFunc("POST /discussion/add", a.handleDiscussionAdd)
		mux.HandleFunc("POST /discussion/hide", a.handleDiscussionHide)
		mux.HandleFunc("POST /discussion/report", a.handleDiscussionReport)
		mux.HandleFunc("POST /discussion/resolve", a.handleDiscussionResolve)
		mux.HandleFunc("POST /edit/preview", a.handleEditPreview)
		mux.HandleFunc("GET /admin/users", a.handleAdminUsers)
		mux.HandleFunc("POST /admin/users/create", a.handleAdminUserCreate)
		mux.HandleFunc("POST /admin/users/groups", a.handleAdminUserGroups)
		mux.HandleFunc("POST /admin/users/password", a.handleAdminUserPassword)
		mux.HandleFunc("POST /admin/users/totp/disable", a.handleAdminUserTOTPDisable)
		mux.HandleFunc("POST /admin/users/status", a.handleAdminUserStatus)
		mux.HandleFunc("POST /admin/users/delete", a.handleAdminUserDelete)
		mux.HandleFunc("GET /admin/reports", a.handleAdminReports)
		mux.HandleFunc("GET /admin/audit", a.handleAdminAudit)
		mux.HandleFunc("GET /admin/audit/export", a.handleAdminAuditExport)
		mux.HandleFunc("GET /admin/groups", a.handleAdminGroups)
		mux.HandleFunc("POST /admin/groups/create", a.handleAdminGroupCreate)
		mux.HandleFunc("POST /admin/groups/rights", a.handleAdminGroupRights)
		mux.HandleFunc("POST /admin/groups/delete", a.handleAdminGroupDelete)
		mux.HandleFunc("GET /admin/pages", a.handleAdminPages)
		mux.HandleFunc("POST /admin/pages/delete", a.handleAdminPageDelete)
		mux.HandleFunc("POST /admin/pages/restore", a.handleAdminPageRestore)
		mux.HandleFunc("GET /admin/entities", a.handleAdminEntities)
		mux.HandleFunc("POST /admin/entities/types", a.handleAdminEntityTypeCreate)
		mux.HandleFunc("POST /admin/entities/types/delete", a.handleAdminEntityTypeDelete)
		mux.HandleFunc("POST /admin/entities/fields", a.handleAdminEntityFieldCreate)
		mux.HandleFunc("POST /admin/entities/fields/delete", a.handleAdminEntityFieldDelete)
		mux.HandleFunc("POST /admin/entities/save", a.handleAdminEntitySave)
		mux.HandleFunc("POST /admin/entities/delete", a.handleAdminEntityDelete)
		mux.HandleFunc("GET /admin/invites", a.handleAdminInvites)
		mux.HandleFunc("POST /admin/invites/create", a.handleAdminInviteCreate)
		mux.HandleFunc("POST /admin/invites/delete", a.handleAdminInviteDelete)
		switch a.cfg.Content.Source {
		case "postgres":
			mux.HandleFunc("GET /edit", a.handleEditPage)
			mux.HandleFunc("POST /edit/save", a.handleEditSave)
			mux.HandleFunc("GET /history", a.handleHistory)
			mux.HandleFunc("GET /revision", a.handleRevision)
			mux.HandleFunc("GET /translations", a.handleTranslations)
			mux.HandleFunc("GET /entities", a.handleEntities)
			mux.HandleFunc("GET /review", a.handleReview)
			mux.HandleFunc("POST /review/approve", a.handleReviewApprove)
			mux.HandleFunc("POST /review/reject", a.handleReviewReject)
			mux.HandleFunc("POST /protect", a.handleProtect)
			mux.HandleFunc("POST /upload", a.handleUpload)
		case "git":
			if a.github != nil {
				mux.HandleFunc("GET /edit", a.handleEditPage)
				mux.HandleFunc("POST /edit/save", a.handleEditSave)
			}
		}
	}

	mux.HandleFunc("GET /", a.servePage)

	var handler http.Handler = mux
	if a.cfg != nil && a.cfg.RateLimit.Enabled {
		handler = a.withRateLimit(handler)
	}
	if a.auth != nil {
		handler = a.withSession(handler)
	}
	if a.cfg != nil && a.cfg.Metrics.Enabled {
		handler = a.withMetrics(handler)
	}
	return securityHeaders(writeLog(handler, a.site.AccessLog()))
}

func metricsPathFree(mux *http.ServeMux, path string) bool {
	req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}, Host: "localhost"}
	_, pattern := mux.Handler(req)
	return pattern == "" || pattern == "/" || pattern == "GET /"
}

func (a *App) withRateLimit(next http.Handler) http.Handler {
	cfg := a.cfg.RateLimit
	window := cfg.WindowDuration()
	routeWindow := cfg.RouteWindowDuration()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rateLimitExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ip := a.requestIP(r)
		if !a.allowRate(r.Context(), "rl:global:"+ip, cfg.Requests, window) {
			a.metrics.observeRateLimited()
			w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		if cfg.RouteRequests > 0 {
			key := "rl:route:" + ip + ":" + auth.HashEmailToken(r.URL.Path)[:16]
			if !a.allowRate(r.Context(), key, cfg.RouteRequests, routeWindow) {
				a.metrics.observeRateLimited()
				w.Header().Set("Retry-After", strconv.Itoa(int(routeWindow.Seconds())))
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitExempt(path string) bool {
	return strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/-/assets/") ||
		path == "/-/healthz" || strings.HasPrefix(path, "/-/webhooks/") || strings.HasPrefix(path, "/media/")
}

func (a *App) requestIP(r *http.Request) string {
	if a.auth != nil {
		return a.auth.ClientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/-/assets/") ||
			path == "/-/healthz" || strings.HasPrefix(path, "/-/webhooks/") || strings.HasPrefix(path, "/media/") {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := r.Cookie(auth.SessionCookieName); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		session, _, err := a.auth.CurrentSession(r.Context(), r)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				slog.Warn("load session failed", "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithSession(r.Context(), session)))
	})
}

func (a *App) baseData(r *http.Request, title string) baseData {
	info := a.site.Info()
	data := baseData{
		Site:             info,
		Title:            title,
		Path:             r.URL.Path,
		LoginEnabled:     a.auth != nil,
		GitHubEnabled:    a.auth != nil && a.auth.GitHubEnabled(),
		Lang:             "zh-CN",
		RegistrationMode: a.registrationMode(),
	}
	if a.cfg != nil && a.cfg.Site.Language != "" {
		data.Lang = a.cfg.Site.Language
	}
	data.T = i18n.For(data.Lang)
	if session := auth.SessionFromContext(r.Context()); session != nil {
		data.User = session.User
		data.CSRF = session.CSRFToken
	}
	data.UIVersion = a.uiVersion()
	if a.ui != nil {
		data.UIAssets = uiAssets{CSS: a.ui.cssName, JS: a.ui.jsName}
	}
	return data
}

func (a *App) uiVersion() string {
	if a.ui == nil {
		return ""
	}
	return a.ui.version
}

func (a *App) requireRight(w http.ResponseWriter, r *http.Request, right string) (*store.SessionData, bool) {
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		next := "/"
		if r.Method == http.MethodGet {
			next = r.URL.RequestURI()
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(safeNext(next)), http.StatusSeeOther)
		return nil, false
	}
	if right != "" && !session.User.HasRight(right) {
		a.renderMessage(w, r, http.StatusForbidden, "没有权限", "当前账号没有执行这个操作的权限.")
		return nil, false
	}
	return session, true
}

func (a *App) uiLang() string {
	if a.cfg != nil && a.cfg.Site.Language != "" {
		return a.cfg.Site.Language
	}
	return "zh-CN"
}

func (a *App) totpIssuer() string {
	if a.cfg != nil && strings.TrimSpace(a.cfg.Site.Title) != "" {
		return a.cfg.Site.Title
	}
	return "Wiki-X"
}

func (a *App) renderEdit(w http.ResponseWriter, r *http.Request, status int, data editData) {
	data.Error = i18n.Message(data.Lang, data.Error)
	a.renderUI(w, r, status, "edit", data)
}

func (a *App) renderUI(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	if a.ui == nil {
		http.Error(w, "ui is not available", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := a.ui.render(&buf, name, data); err != nil {
		slog.Error("render ui failed", "page", name, "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) renderMessage(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	lang := a.uiLang()
	heading = i18n.Message(lang, heading)
	message = i18n.Message(lang, message)
	a.renderUI(w, r, status, "message", messageData{
		baseData: a.baseData(r, heading),
		Heading:  heading,
		Message:  message,
	})
}

func (a *App) redirect(w http.ResponseWriter, r *http.Request, target string, status int) {
	if status == 0 {
		status = http.StatusSeeOther
	}
	http.Redirect(w, r, target, status)
}

func safeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return "/"
	}
	for _, r := range next {
		if unicode.IsControl(r) {
			return "/"
		}
	}
	return next
}

func (a *App) baseDataForRoute(r *http.Request, title, route string) baseData {
	data := a.baseData(r, title)
	code := a.site.LocaleCodeForRoute(route)
	data.Lang = code
	data.T = i18n.For(code)
	return data
}

func (a *App) noticeText(code string) string {
	lang := "zh-CN"
	if a.cfg != nil {
		lang = a.cfg.Site.Language
	}
	return a.noticeTextFor(lang, code)
}

func (a *App) noticeTextFor(lang, code string) string {
	switch code {
	case "published":
		return i18n.Text(lang, "notice_published")
	case "pending":
		return i18n.Text(lang, "notice_pending")
	case "approved":
		return i18n.Text(lang, "notice_approved")
	case "rejected":
		return i18n.Text(lang, "notice_rejected")
	case "linked":
		return i18n.Text(lang, "notice_linked")
	case "protected":
		return i18n.Text(lang, "notice_protected")
	case "unprotected":
		return i18n.Text(lang, "notice_unprotected")
	case "protected_full":
		return i18n.Text(lang, "notice_protected_full")
	case "comment_added":
		return i18n.Text(lang, "notice_comment_added")
	case "comment_hidden":
		return i18n.Text(lang, "notice_comment_hidden")
	case "comment_shown":
		return i18n.Text(lang, "notice_comment_shown")
	case "report_submitted":
		return i18n.Text(lang, "notice_report_submitted")
	case "reports_resolved":
		return i18n.Text(lang, "notice_reports_resolved")
	case "groups_saved":
		return i18n.Text(lang, "notice_groups_saved")
	case "user_disabled":
		return i18n.Text(lang, "notice_user_disabled")
	case "user_enabled":
		return i18n.Text(lang, "notice_user_enabled")
	case "user_created":
		return i18n.Text(lang, "notice_user_created")
	case "password_changed":
		return i18n.Text(lang, "notice_password_changed")
	case "password_reset":
		return i18n.Text(lang, "notice_password_reset")
	case "group_created":
		return i18n.Text(lang, "notice_group_created")
	case "group_rights_saved":
		return i18n.Text(lang, "notice_group_rights_saved")
	case "group_deleted":
		return i18n.Text(lang, "notice_group_deleted")
	case "totp_setup":
		return i18n.Text(lang, "notice_totp_setup")
	case "totp_enabled":
		return i18n.Text(lang, "notice_totp_enabled")
	case "totp_disabled":
		return i18n.Text(lang, "notice_totp_disabled")
	case "user_totp_disabled":
		return i18n.Text(lang, "notice_user_totp_disabled")
	case "recovery_codes_generated":
		return i18n.Text(lang, "notice_recovery_codes_generated")
	case "entity_type_created":
		return i18n.Text(lang, "notice_entity_type_created")
	case "entity_type_deleted":
		return i18n.Text(lang, "notice_entity_type_deleted")
	case "entity_field_created":
		return i18n.Text(lang, "notice_entity_field_created")
	case "entity_field_deleted":
		return i18n.Text(lang, "notice_entity_field_deleted")
	case "entity_saved":
		return i18n.Text(lang, "notice_entity_saved")
	case "entity_deleted":
		return i18n.Text(lang, "notice_entity_deleted")
	case "page_deleted":
		return i18n.Text(lang, "notice_page_deleted")
	case "page_restored":
		return i18n.Text(lang, "notice_page_restored")
	case "account_deleted":
		return i18n.Text(lang, "notice_account_deleted")
	case "user_deleted":
		return i18n.Text(lang, "notice_user_deleted")
	case "email_verified":
		return i18n.Text(lang, "notice_email_verified")
	case "email_saved":
		return i18n.Text(lang, "notice_email_saved")
	case "email_verify_sent":
		return i18n.Text(lang, "notice_email_verify_sent")
	case "email_send_failed":
		return i18n.Text(lang, "notice_email_send_failed")
	case "registered":
		return i18n.Text(lang, "notice_registered")
	case "invite_created":
		return i18n.Text(lang, "notice_invite_created")
	case "invite_deleted":
		return i18n.Text(lang, "notice_invite_deleted")
	default:
		return ""
	}
}

func (a *App) requestReload() {
	if a.reloadCh == nil {
		return
	}
	select {
	case a.reloadCh <- struct{}{}:
	default:
	}
}

func (a *App) reloadWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.reloadCh:
			if a.syncContent != nil {
				syncCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				err := a.syncContent(syncCtx)
				cancel()
				if err != nil {
					slog.Error("content sync failed", "err", err)
					continue
				}
			}
			reloadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := a.site.Reload(reloadCtx)
			cancel()
			if err != nil {
				slog.Error("content reload failed", "err", err)
				continue
			}
			slog.Info("content reloaded", "pages", len(a.site.Routes()))
		}
	}
}

func RunApp(ctx context.Context, app *App, addr string) error {
	go app.reloadWorker(ctx)
	return runServer(ctx, app.Handler(), addr)
}

func Run(ctx context.Context, s *site.Site, addr string) error {
	app := &App{site: s, reloadCh: make(chan struct{}, 1)}
	return RunApp(ctx, app, addr)
}

func newHandler(s *site.Site) http.Handler {
	app := &App{site: s, reloadCh: make(chan struct{}, 1)}
	return app.Handler()
}

func runServer(ctx context.Context, handler http.Handler, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	slog.Info("wikix server listening", "addr", listener.Addr().String())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server: %w", err)
	}
}
