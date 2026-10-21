package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleProtect(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "页面路由必须以 / 开头和结尾, 并且不能使用系统保留路径.")
		return
	}
	level := strings.TrimSpace(r.FormValue("level"))
	if level == "" {
		level = store.ProtectionNone
		if r.FormValue("protected") == "1" {
			level = store.ProtectionSemi
		}
	}
	if err := a.store.SetPageProtection(r.Context(), route, level, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面.")
			return
		}
		if errors.Is(err, store.ErrBadProtection) {
			a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "保护级别不合法.")
			return
		}
		slog.Error("set page protection failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法修改页面保护状态.")
		return
	}
	a.reloadSite(r.Context())
	a.invalidateRoute(r.Context(), route)
	notice := "unprotected"
	switch level {
	case store.ProtectionSemi:
		notice = "protected"
	case store.ProtectionFull:
		notice = "protected_full"
	}
	a.redirect(w, r, "/edit?route="+url.QueryEscape(route)+"&notice="+notice, http.StatusSeeOther)
}

func (a *App) handleWatch(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "页面路由必须以 / 开头和结尾, 并且不能使用系统保留路径.")
		return
	}
	action := r.FormValue("action")
	var err error
	if action == "unwatch" {
		err = a.store.Unwatch(r.Context(), session.User.ID, route)
	} else {
		if _, exists := a.site.FindPage(route); !exists {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面.")
			return
		}
		err = a.store.Watch(r.Context(), session.User.ID, route)
	}
	if err != nil {
		slog.Error("update watchlist failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法更新监视列表.")
		return
	}
	next := strings.TrimSpace(r.FormValue("next"))
	if next == "" || len(next) > 2048 {
		next = "/watchlist"
	}
	a.redirect(w, r, safeNext(next), http.StatusSeeOther)
}

func (a *App) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	pages, err := a.store.ListWatchedRevisions(r.Context(), session.User.ID, 200)
	if err != nil {
		slog.Error("list watched pages failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取监视列表.")
		return
	}
	for _, page := range pages {
		info, exists := a.site.FindPage(page.Route)
		if !exists {
			continue
		}
		if a.cfg.Content.Source == "git" {
			page.Title = info.Title
			page.AuthorName = ""
			page.Comment = ""
			page.Source = "git"
			page.ChangedAt = nil
			if changed, err := time.Parse("2006-01-02", info.Updated); err == nil {
				page.ChangedAt = &changed
			}
			continue
		}
		if page.Title == page.Route {
			page.Title = info.Title
		}
	}
	data := watchlistData{
		baseData: a.baseData(r, "监视列表"),
		Pages:    pages,
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "watchlist", data)
}

func (a *App) handleDiscussion(w http.ResponseWriter, r *http.Request) {
	route := strings.TrimSpace(r.URL.Query().Get("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "无法读取这个页面的讨论.")
		return
	}
	page, exists := a.site.FindPage(route)
	if !exists {
		a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面.")
		return
	}
	session := auth.SessionFromContext(r.Context())
	canReview := session != nil && session.User.HasRight(auth.RightReview)
	comments, err := a.store.ListComments(r.Context(), route, canReview)
	if err != nil {
		slog.Error("list comments failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取讨论.")
		return
	}
	data := discussionData{
		baseData:  a.baseDataForRoute(r, "讨论", route),
		Route:     route,
		PageTitle: page.Title,
		Comments:  comments,
		LoggedIn:  session != nil,
		CanReview: canReview,
		History:   a.cfg.Content.Source == "postgres",
		Next:      r.URL.RequestURI(),
	}
	if session != nil {
		watched, err := a.store.IsWatched(r.Context(), session.User.ID, route)
		if err != nil {
			slog.Error("check discussion watch state failed", "err", err)
		}
		data.Watching = watched
	}
	data.Notice = a.noticeTextFor(a.site.LocaleCodeForRoute(route), r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "discussion", data)
}

func (a *App) handleDiscussionAdd(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusRequestEntityTooLarge, "发送失败", "评论过大或表单格式不正确.")
		return
	}
	if !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "发送失败", "请求校验失败.")
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "路由不合法", "页面路由必须以 / 开头和结尾, 并且不能使用系统保留路径.")
		return
	}
	if _, exists := a.site.FindPage(route); !exists {
		a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面.")
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	switch {
	case body == "":
		a.renderMessage(w, r, http.StatusBadRequest, "发送失败", "评论不能为空.")
		return
	case utf8.RuneCountInString(body) > 4000:
		a.renderMessage(w, r, http.StatusBadRequest, "发送失败", "评论不能超过 4000 个字符.")
		return
	case containsBlockedWord(a.blockedWords, body):
		a.renderMessage(w, r, http.StatusBadRequest, "发送失败", "评论包含不允许的内容.")
		return
	}
	if !a.allowAction(r.Context(), "comment:"+strconv.FormatInt(session.User.ID, 10), 1) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "发送失败", "评论太频繁, 请稍后再试.")
		return
	}
	if _, err := a.store.AddComment(r.Context(), route, session.User.ID, body); err != nil {
		slog.Error("add comment failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "发送失败", "暂时无法保存评论.")
		return
	}
	a.redirect(w, r, "/discussion?route="+url.QueryEscape(route)+"&notice=comment_added", http.StatusSeeOther)
}

func (a *App) handleDiscussionHide(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "评论编号不正确.")
		return
	}
	hidden := r.FormValue("hidden") == "1"
	route, err := a.store.HideComment(r.Context(), id, session.User.ID, hidden)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "评论不存在", "没有找到这条评论.")
			return
		}
		slog.Error("hide comment failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法隐藏评论.")
		return
	}
	notice := "comment_shown"
	if hidden {
		notice = "comment_hidden"
	}
	a.redirectDiscussionResult(w, r, route, notice)
}

func (a *App) handleDiscussionReport(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "举报失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "举报失败", "评论编号不正确.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if utf8.RuneCountInString(reason) > 200 {
		a.renderMessage(w, r, http.StatusBadRequest, "举报失败", "举报原因不能超过 200 个字符.")
		return
	}
	if !a.allowAction(r.Context(), "comment-report:"+strconv.FormatInt(session.User.ID, 10), 2) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "举报失败", "举报太频繁, 请稍后再试.")
		return
	}
	route, err := a.store.ReportComment(r.Context(), id, session.User.ID, reason)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "评论不存在", "没有找到这条评论, 或者不能举报自己的评论.")
			return
		}
		slog.Error("report comment failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "举报失败", "暂时无法提交举报.")
		return
	}
	a.redirect(w, r, "/discussion?route="+url.QueryEscape(route)+"&notice=report_submitted", http.StatusSeeOther)
}

func (a *App) handleDiscussionResolve(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "评论编号不正确.")
		return
	}
	route, err := a.store.ResolveCommentReports(r.Context(), id, session.User.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "评论不存在", "没有找到这条评论.")
			return
		}
		slog.Error("resolve comment reports failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法处理举报.")
		return
	}
	a.redirectDiscussionResult(w, r, route, "reports_resolved")
}

func (a *App) allowAction(ctx context.Context, key string, factor int) bool {
	limit, window := 5, time.Minute
	if a.cfg != nil {
		limit = a.cfg.Comment.RateLimitOrDefault()
		window = a.cfg.Comment.RateWindowOrDefault()
	}
	if factor > 1 {
		limit *= factor
	}
	allowed, err := a.site.Allow(ctx, key, limit, window)
	if err != nil {
		slog.Warn("rate limit check failed, using local fallback", "key", key, "err", err)
		if a.limitFallback == nil {
			return true
		}
		allowed, err = a.limitFallback.Allow(ctx, key, limit, window)
		if err != nil {
			slog.Warn("local rate limit failed", "key", key, "err", err)
			return true
		}
	}
	return allowed
}

func normalizeBlockedWords(words []string) []string {
	seen := make(map[string]struct{}, len(words))
	result := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.ToLower(strings.TrimSpace(word))
		if word == "" {
			continue
		}
		if _, exists := seen[word]; exists {
			continue
		}
		seen[word] = struct{}{}
		result = append(result, word)
	}
	return result
}

func containsBlockedWord(words []string, body string) bool {
	if len(words) == 0 {
		return false
	}
	body = strings.ToLower(body)
	for _, word := range words {
		if strings.Contains(body, word) {
			return true
		}
	}
	return false
}
