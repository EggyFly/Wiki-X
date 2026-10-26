package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAdminPages(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightReview); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "all", "active", "deleted":
	default:
		status = "all"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	page, size := adminPage(r, 50)
	pages, total, err := a.store.ListAdminPages(r.Context(), status, query, size, (page-1)*size)
	if err != nil {
		slog.Error("list admin pages failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取页面列表.")
		return
	}
	data := adminPagesData{
		baseData: a.baseData(r, "页面管理"),
		Pages:    pages,
		Status:   status,
		Query:    query,
		Pager:    newPager(page, totalPages(total, size), size),
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_pages", data)
}

func (a *App) handleAdminPageDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "路由不合法.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	switch {
	case reason == "":
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能为空.")
		return
	case utf8.RuneCountInString(reason) > 500:
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能超过 500 个字符.")
		return
	}
	if err := a.store.DeletePage(r.Context(), route, reason, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面, 或者它已经被删除.")
			return
		}
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		slog.Error("delete page failed", "err", err, "route", route)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法删除页面.")
		return
	}
	a.reloadSite(r.Context())
	a.invalidateRoute(r.Context(), route)
	a.syncSearchDocument(r.Context(), route)
	a.redirect(w, r, "/admin/pages?status=all&notice=page_deleted", http.StatusSeeOther)
}

func (a *App) handleAdminPageRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "路由不合法.")
		return
	}
	if err := a.store.RestorePage(r.Context(), route, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面, 或者它没有被删除.")
			return
		}
		if errors.Is(err, store.ErrEditConflict) {
			a.renderMessage(w, r, http.StatusConflict, "恢复失败", "这个路由已经被另一个活动页面占用.")
			return
		}
		slog.Error("restore page failed", "err", err, "route", route)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法恢复页面.")
		return
	}
	a.reloadSite(r.Context())
	a.invalidateRoute(r.Context(), route)
	a.syncSearchDocument(r.Context(), route)
	a.redirect(w, r, "/admin/pages?status=all&notice=page_restored", http.StatusSeeOther)
}
