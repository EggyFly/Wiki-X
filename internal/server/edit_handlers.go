package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/content"
	gitsource "github.com/EggyFly/Wiki-X/internal/content/gitsource"
	"github.com/EggyFly/Wiki-X/internal/ghclient"
	"github.com/EggyFly/Wiki-X/internal/i18n"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleEditPage(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightEdit)
	if !ok {
		return
	}
	route := strings.TrimSpace(r.URL.Query().Get("route"))
	prMode := a.cfg.Content.Source == "git"
	base := a.baseData(r, "编辑")
	if route != "" {
		base = a.baseDataForRoute(r, "编辑", route)
	}
	data := editData{
		baseData:  base,
		Route:     route,
		PRMode:    prMode,
		CanUpload: !prMode && session.User.HasRight(auth.RightUpload),
		CanReview: !prMode && session.User.HasRight(auth.RightReview),
	}
	data.Notice = a.noticeTextFor(a.site.LocaleCodeForRoute(route), r.URL.Query().Get("notice"))
	if route == "" {
		data.IsNew = true
		a.renderEdit(w, r, http.StatusOK, data)
		return
	}
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "页面路由必须以 / 开头和结尾, 并且不能使用系统保留路径.")
		return
	}

	if prMode {
		page, ok := a.site.FindPage(route)
		if !ok {
			data.IsNew = true
			prefillTranslationKey(r, &data)
			a.renderEdit(w, r, http.StatusOK, data)
			return
		}
		data.Route = page.Route
		data.PageTitle = page.Title
		data.Description = page.Description
		data.Tags = strings.Join(page.Tags, ", ")
		data.Body = page.Body
		data.BaseVersion = page.Version
		data.TranslationKey = page.TranslationKey
		data.EntityKey = page.EntityKey
		if a.github != nil {
			data.HistoryURL = a.github.HistoryURL(a.githubRepoPath(page.SourcePath))
		}
		a.renderEdit(w, r, http.StatusOK, data)
		return
	}

	page, err := a.store.GetPage(r.Context(), route)
	if errors.Is(err, store.ErrNotFound) {
		data.IsNew = true
		prefillTranslationKey(r, &data)
		a.renderEdit(w, r, http.StatusOK, data)
		return
	}
	if err != nil {
		slog.Error("load page for edit failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取页面.")
		return
	}
	if page.DeletedAt != nil {
		a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "页面不存在或已删除.")
		return
	}
	if page.Draft {
		a.renderMessage(w, r, http.StatusConflict, "页面待审核", "这个页面还有未发布的版本, 请先在审核队列处理.")
		return
	}
	data.Route = page.Route
	data.PageTitle = page.Title
	data.Description = page.Description
	data.Tags = strings.Join(page.Tags, ", ")
	data.Body = page.Body
	data.BaseVersion = page.Version
	data.HistoryURL = "/history?route=" + url.QueryEscape(page.Route)
	data.Protected = page.Protected
	data.ProtectionLevel = pageProtectionLevel(page)
	data.CanProtect = session.User.HasRight(auth.RightReview)
	if data.ProtectionLevel == store.ProtectionFull && !data.CanProtect {
		a.renderMessage(w, r, http.StatusForbidden, "页面已保护", "这个页面已被完全保护, 只有审核员可以编辑.")
		return
	}
	a.renderEdit(w, r, http.StatusOK, data)
}

func (a *App) handleEditSave(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightEdit)
	if !ok {
		return
	}
	limit := a.cfg.Content.MaxFileSize + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusRequestEntityTooLarge, "保存失败", "内容过大或表单格式不正确.")
		return
	}
	if !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "保存失败", "请求校验失败.")
		return
	}

	route := strings.TrimSpace(r.FormValue("route"))
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	body := r.FormValue("body")
	comment := strings.TrimSpace(r.FormValue("comment"))
	baseVersion := strings.TrimSpace(r.FormValue("base_version"))
	translationKey := strings.TrimSpace(r.FormValue("translation_key"))
	entityKey := strings.TrimSpace(r.FormValue("entity"))
	tags := parseTags(r.FormValue("tags"))

	prMode := a.cfg.Content.Source == "git"
	data := editData{
		baseData:       a.baseDataForRoute(r, "编辑", route),
		Route:          route,
		PageTitle:      title,
		Description:    description,
		Tags:           strings.Join(tags, ", "),
		Body:           body,
		Comment:        comment,
		BaseVersion:    baseVersion,
		TranslationKey: translationKey,
		EntityKey:      entityKey,
		IsNew:          baseVersion == "",
		PRMode:         prMode,
		CanUpload:      !prMode && session.User.HasRight(auth.RightUpload),
		CanReview:      !prMode && session.User.HasRight(auth.RightReview),
	}
	if !prMode && baseVersion != "" {
		data.HistoryURL = "/history?route=" + url.QueryEscape(route)
	}

	if err := site.ValidateRoute(route); err != nil {
		data.Error = "路由不合法, 必须以 / 开头和结尾, 并且不能使用系统保留路径."
		a.renderEdit(w, r, http.StatusBadRequest, data)
		return
	}
	if !prMode {
		page, err := a.store.GetPage(r.Context(), route)
		if err == nil && page.DeletedAt != nil {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "页面不存在或已删除.")
			return
		}
		if err == nil {
			data.Protected = page.Protected
			data.ProtectionLevel = pageProtectionLevel(page)
			data.CanProtect = session.User.HasRight(auth.RightReview)
			if data.ProtectionLevel == store.ProtectionFull && !data.CanProtect {
				a.renderMessage(w, r, http.StatusForbidden, "页面已保护", "这个页面已被完全保护, 只有审核员可以编辑.")
				return
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load page before save failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法读取页面.")
			return
		}
	}
	switch {
	case title == "":
		data.Error = "标题不能为空."
	case utf8.RuneCountInString(title) > 200:
		data.Error = "标题不能超过 200 个字符."
	case utf8.RuneCountInString(description) > 500:
		data.Error = "摘要不能超过 500 个字符."
	case utf8.RuneCountInString(comment) > 500:
		data.Error = "编辑摘要不能超过 500 个字符."
	case strings.TrimSpace(body) == "":
		data.Error = "正文不能为空."
	case int64(len(body)) > a.cfg.Content.MaxFileSize:
		data.Error = "正文超过站点允许的大小."
	case len(tags) > 20:
		data.Error = "标签不能超过 20 个."
	case utf8.RuneCountInString(translationKey) > 200:
		data.Error = "翻译标识不能超过 200 个字符."
	case !validTranslationKey(translationKey):
		data.Error = "翻译标识不合法."
	}
	if data.Error != "" {
		a.renderEdit(w, r, http.StatusBadRequest, data)
		return
	}
	var entityID *int64
	if !prMode && entityKey != "" {
		entity, err := a.store.GetEntityByName(r.Context(), entityKey)
		if errors.Is(err, store.ErrNotFound) {
			data.Error = "关联实体不存在."
			a.renderEdit(w, r, http.StatusBadRequest, data)
			return
		}
		if err != nil {
			slog.Error("load entity for edit failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法读取关联实体.")
			return
		}
		entityID = &entity.ID
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > 50 {
			data.Error = "单个标签不能超过 50 个字符."
			a.renderEdit(w, r, http.StatusBadRequest, data)
			return
		}
	}

	if prMode {
		a.saveGitEdit(w, r, session, data, route, title, description, body, tags, translationKey, comment, baseVersion)
		return
	}

	publish := session.User.HasRight(auth.RightReview)
	revision, err := a.store.CreateRevision(r.Context(), store.RevisionInput{
		Route:                  route,
		Title:                  title,
		Description:            description,
		Body:                   body,
		Tags:                   tags,
		AuthorID:               session.User.ID,
		Comment:                comment,
		BaseVersion:            baseVersion,
		TranslationKey:         translationKey,
		TranslationBaseVersion: a.translationBaseVersion(r.Context(), route, translationKey),
		EntityID:               entityID,
		Publish:                publish,
		AllowProtected:         publish,
	})
	if errors.Is(err, store.ErrProtected) {
		a.renderMessage(w, r, http.StatusForbidden, "页面已保护", "这个页面已被保护, 只有审核员可以编辑.")
		return
	}
	if errors.Is(err, store.ErrRouteExists) {
		data.Error = "这个路由已经被其他页面占用, 大小写不同的同名路由也算冲突."
		a.renderEdit(w, r, http.StatusConflict, data)
		return
	}
	if errors.Is(err, store.ErrEditConflict) {
		data.Error = "页面已经被其他人修改, 请刷新历史后再编辑."
		a.renderEdit(w, r, http.StatusConflict, data)
		return
	}
	if err != nil {
		slog.Error("create revision failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法保存修订.")
		return
	}

	notice := "pending"
	if revision.Status == store.StatusPublished {
		notice = "published"
		a.reloadPage(r.Context(), route)
		a.invalidateRoute(r.Context(), route)
		a.syncSearchDocument(r.Context(), route)
	}
	a.redirect(w, r, "/history?route="+url.QueryEscape(route)+"&notice="+notice, http.StatusSeeOther)
}

func prefillTranslationKey(r *http.Request, data *editData) {
	key := strings.TrimSpace(r.URL.Query().Get("translation_key"))
	if key != "" && validTranslationKey(key) {
		data.TranslationKey = key
	}
}

func (a *App) translationBaseVersion(ctx context.Context, route, translationKey string) string {
	if translationKey == "" {
		return ""
	}
	baseRoute := a.site.BaseRouteFor(route)
	if baseRoute == route {
		return ""
	}
	base, err := a.store.GetPage(ctx, baseRoute)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load translation base page failed", "route", baseRoute, "err", err)
		}
		return ""
	}
	if base.Draft || base.DeletedAt != nil {
		return ""
	}
	return base.Version
}

func (a *App) saveGitEdit(w http.ResponseWriter, r *http.Request, session *store.SessionData, data editData, route, title, description, body string, tags []string, translationKey, comment, baseVersion string) {
	var sourcePath string
	historyURL := ""
	page, exists := a.site.FindPage(route)
	if exists {
		if baseVersion == "" || page.Version != baseVersion {
			data.Error = "页面内容已经变化, 请刷新后重新编辑."
			a.renderEdit(w, r, http.StatusConflict, data)
			return
		}
		sourcePath = page.SourcePath
		historyURL = a.github.HistoryURL(a.githubRepoPath(sourcePath))
	} else {
		if baseVersion != "" {
			data.Error = "页面内容已经变化, 请刷新后重新编辑."
			a.renderEdit(w, r, http.StatusConflict, data)
			return
		}
		sourcePath = gitsource.FileForRoute(route)
	}
	data.HistoryURL = historyURL
	repoPath := a.githubRepoPath(sourcePath)
	remoteData, remoteErr := a.github.FileContent(r.Context(), repoPath, a.github.BaseBranch())
	if exists {
		if remoteErr != nil {
			data.Error = "无法确认 GitHub 上的最新内容, 请稍后重试."
			a.renderEdit(w, r, http.StatusBadGateway, data)
			return
		}
		remotePage, err := gitsource.ParseDocument(sourcePath, remoteData, time.Now())
		if err != nil || !sameContentPage(page, remotePage) {
			data.Error = "页面在 GitHub 上已经变化, 请先同步后再编辑."
			a.renderEdit(w, r, http.StatusConflict, data)
			return
		}
	} else if remoteErr == nil {
		data.Error = "GitHub 上已经存在这个路径, 请先同步后再创建."
		a.renderEdit(w, r, http.StatusConflict, data)
		return
	} else if !isGitHubNotFound(remoteErr) {
		data.Error = "无法确认 GitHub 上的最新内容, 请稍后重试."
		a.renderEdit(w, r, http.StatusBadGateway, data)
		return
	}

	pageContent := &content.Page{
		Route:          route,
		SourcePath:     sourcePath,
		Title:          title,
		Description:    description,
		Body:           body,
		Tags:           tags,
		Updated:        time.Now().Format("2006-01-02"),
		TranslationKey: translationKey,
	}
	fileContent, err := gitsource.FormatDocument(pageContent)
	if err != nil {
		slog.Error("format page for github failed", "err", err)
		data.Error = "生成 Markdown 失败."
		a.renderEdit(w, r, http.StatusInternalServerError, data)
		return
	}

	action := "编辑"
	if !exists {
		action = "新建"
	}
	created, err := a.github.CreatePullRequest(r.Context(), ghclient.PullRequestInput{
		Path:    repoPath,
		Content: fileContent,
		Title:   fmt.Sprintf("%s页面: %s", action, title),
		Body:    fmt.Sprintf("由 Wiki-X 用户 %s 提交.\n\n路由: %s\n\n%s", session.User.Name, route, comment),
	})
	if err != nil {
		slog.Error("create github pull request failed", "err", err)
		data.Error = "创建 GitHub PR 失败, 请稍后重试."
		a.renderEdit(w, r, http.StatusBadGateway, data)
		return
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "github.pr.create", route, created.URL)
	a.renderUI(w, r, http.StatusOK, "pr", prData{
		baseData: a.baseDataForRoute(r, "PR 已创建", route),
		URL:      created.URL,
		Number:   created.Number,
		Route:    route,
	})
}

func (a *App) githubRepoPath(sourcePath string) string {
	sourcePath = strings.TrimPrefix(sourcePath, "/")
	if prefix := strings.Trim(a.cfg.GitHub.ContentPath, "/"); prefix != "" {
		return prefix + "/" + sourcePath
	}
	return sourcePath
}

func sameContentPage(left, right *content.Page) bool {
	if left.Title != right.Title || left.Description != right.Description || left.Body != right.Body || left.TranslationKey != right.TranslationKey {
		return false
	}
	if len(left.Tags) != len(right.Tags) {
		return false
	}
	for i := range left.Tags {
		if left.Tags[i] != right.Tags[i] {
			return false
		}
	}
	return true
}

func isGitHubNotFound(err error) bool {
	var apiErr *ghclient.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	route := strings.TrimSpace(r.URL.Query().Get("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "无法读取这个页面的历史.")
		return
	}
	session := auth.SessionFromContext(r.Context())
	viewerID := int64(0)
	canReview := false
	canEdit := false
	if session != nil {
		viewerID = session.User.ID
		canReview = session.User.HasRight(auth.RightReview)
		canEdit = session.User.HasRight(auth.RightEdit)
	}
	revisions, err := a.store.ListRevisions(r.Context(), route, viewerID, canReview)
	if err != nil {
		slog.Error("list revisions failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取历史版本.")
		return
	}
	data := historyData{
		baseData:  a.baseDataForRoute(r, "历史版本", route),
		Route:     route,
		Revisions: revisions,
		CanEdit:   canEdit,
		CanReview: canReview,
		LoggedIn:  session != nil,
		Next:      r.URL.RequestURI(),
	}
	if session != nil {
		watched, err := a.store.IsWatched(r.Context(), session.User.ID, route)
		if err != nil {
			slog.Error("check watch state failed", "err", err)
		}
		data.Watching = watched
	}
	data.Notice = a.noticeTextFor(a.site.LocaleCodeForRoute(route), r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "history", data)
}

func (a *App) handleRevision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "版本不存在", "版本编号不正确.")
		return
	}
	revision, err := a.store.GetRevision(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.renderMessage(w, r, http.StatusNotFound, "版本不存在", "没有找到这个版本.")
		return
	}
	if err != nil {
		slog.Error("get revision failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取版本.")
		return
	}
	session := auth.SessionFromContext(r.Context())
	canReview := session != nil && session.User.HasRight(auth.RightReview)
	if revision.Status != store.StatusPublished {
		author := false
		if session != nil && revision.AuthorID != nil && *revision.AuthorID == session.User.ID {
			author = true
		}
		if !canReview && !author {
			a.renderMessage(w, r, http.StatusNotFound, "版本不存在", "没有找到这个版本.")
			return
		}
	}

	oldTitle, oldBody := "", ""
	if revision.BaseRevisionID != nil {
		base, err := a.store.GetRevision(r.Context(), *revision.BaseRevisionID)
		if err == nil {
			oldTitle = base.Title
			oldBody = base.Body
		}
	}
	data := revisionData{
		baseData:  a.baseDataForRoute(r, "版本详情", revision.Route),
		Revision:  revision,
		Diff:      unifiedDiff(a.cfg.Site.Language, oldTitle, oldBody, revision.Title, revision.Body),
		CanReview: canReview && revision.Status == store.StatusPending,
	}
	a.renderUI(w, r, http.StatusOK, "revision", data)
}

func (a *App) handleReview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightReview); !ok {
		return
	}
	revisions, err := a.store.ListPendingRevisions(r.Context(), 100)
	if err != nil {
		slog.Error("list pending revisions failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取待审版本.")
		return
	}
	data := reviewData{
		baseData:  a.baseData(r, "待审核"),
		Revisions: revisions,
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "review", data)
}

func (a *App) handleReviewApprove(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "版本编号不正确.")
		return
	}
	route, err := a.store.PublishRevision(r.Context(), id, session.User.ID)
	if err != nil {
		if errors.Is(err, store.ErrEditConflict) {
			a.renderMessage(w, r, http.StatusConflict, "操作失败", "这个版本已经被处理.")
			return
		}
		slog.Error("approve revision failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法发布这个版本.")
		return
	}
	a.reloadPage(r.Context(), route)
	a.invalidateRoute(r.Context(), route)
	a.syncSearchDocument(r.Context(), route)
	a.redirect(w, r, "/review?notice=approved", http.StatusSeeOther)
}

func (a *App) handleReviewReject(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "版本编号不正确.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if utf8.RuneCountInString(reason) > 500 {
		reason = string([]rune(reason)[:500])
	}
	if _, err := a.store.RejectRevision(r.Context(), id, session.User.ID, reason); err != nil {
		if errors.Is(err, store.ErrEditConflict) {
			a.renderMessage(w, r, http.StatusConflict, "操作失败", "这个版本已经被处理.")
			return
		}
		slog.Error("reject revision failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法拒绝这个版本.")
		return
	}
	a.redirect(w, r, "/review?notice=rejected", http.StatusSeeOther)
}

func pageProtectionLevel(page *store.PageRecord) string {
	if page.ProtectionLevel != "" {
		return page.ProtectionLevel
	}
	if page.Protected {
		return store.ProtectionSemi
	}
	return store.ProtectionNone
}

func (a *App) invalidateRoute(ctx context.Context, route string) {
	if a.site == nil || route == "" {
		return
	}
	deleted, err := a.site.InvalidateRoute(context.WithoutCancel(ctx), route)
	if err != nil {
		slog.Warn("invalidate page cache failed", "route", route, "err", err)
		return
	}
	if deleted > 0 {
		slog.Debug("page cache invalidated", "route", route, "keys", deleted)
	}
}

func (a *App) reloadSite(ctx context.Context) {
	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := a.site.Reload(reloadCtx); err != nil {
		slog.Error("reload after publish failed", "err", err)
	}
}

func (a *App) reloadPage(ctx context.Context, route string) {
	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := a.site.ReloadPage(reloadCtx, route); err != nil {
		slog.Error("reload page failed, falling back to full reload", "route", route, "err", err)
		if err := a.site.Reload(reloadCtx); err != nil {
			slog.Error("full reload after page reload failed", "err", err)
		}
	}
}

func (a *App) removePage(ctx context.Context, route string) {
	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := a.site.RemoveRoute(reloadCtx, route); err != nil {
		slog.Error("remove page from site failed, falling back to full reload", "route", route, "err", err)
		if err := a.site.Reload(reloadCtx); err != nil {
			slog.Error("full reload after page removal failed", "err", err)
		}
	}
}

func (a *App) reloadEntities(ctx context.Context) {
	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if err := a.site.ReloadEntities(reloadCtx); err != nil {
		slog.Error("reload entities failed, falling back to full reload", "err", err)
		if err := a.site.Reload(reloadCtx); err != nil {
			slog.Error("full reload after entity reload failed", "err", err)
		}
	}
}

func (a *App) handleEditPreview(w http.ResponseWriter, r *http.Request) {
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "请先登录."})
		return
	}
	if !session.User.HasRight(auth.RightEdit) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "没有编辑权限."})
		return
	}
	limit := a.cfg.Content.MaxFileSize + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "内容过大或表单格式不正确."})
		return
	}
	if !a.auth.CheckCSRF(r, session) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "请求校验失败."})
		return
	}
	body := r.FormValue("body")
	if int64(len(body)) > a.cfg.Content.MaxFileSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "正文超过站点允许的大小."})
		return
	}
	if a.markdown == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "预览暂不可用."})
		return
	}
	html, err := a.markdown.ToHTML([]byte(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法渲染预览."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": string(html)})
}

func validTranslationKey(key string) bool {
	if key == "" {
		return true
	}
	if strings.Contains(key, "..") {
		return false
	}
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func parseTags(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，'
	})
	seen := make(map[string]struct{}, len(parts))
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		tags = append(tags, part)
	}
	return tags
}

func unifiedDiff(lang, oldTitle, oldBody, newTitle, newBody string) string {
	texts := i18n.For(lang)
	var builder strings.Builder
	if oldTitle != newTitle {
		fmt.Fprintf(&builder, "%s: %s -> %s\n\n", texts["title_changed"], oldTitle, newTitle)
	}
	if oldBody == newBody {
		if builder.Len() == 0 {
			return texts["no_changes"]
		}
		return builder.String()
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldBody),
		B:        difflib.SplitLines(newBody),
		FromFile: texts["old_version"],
		ToFile:   texts["new_version"],
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return err.Error()
	}
	builder.WriteString(text)
	return builder.String()
}
